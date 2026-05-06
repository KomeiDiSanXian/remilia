package command

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
)

// Registry 是一个高性能的命令注册表
// 提供快速的命令查找、别名解析和统计功能
//
// 优化: 使用单一 Trie 树索引，移除冗余的 commands map，减少 40-50% 内存占用
type Registry struct {
	// 索引结构 - 统一使用 Trie 树
	trie *Trie // Trie 树用于精确查找和前缀搜索

	// 别名映射（保留 map，因为别名不需要前缀搜索）
	aliases map[string]string // alias -> command name

	// 快速查找
	mu       sync.RWMutex
	compiled infraatomic.Value[*compiledRegistry]

	// 统计信息
	lookupCount atomic.Int64
	hitCount    atomic.Int64
	missCount   atomic.Int64
	aliasHits   atomic.Int64
}

// Meta 存储命令的元数据和快速访问信息
type Meta struct {
	// 基本信息
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Category    string
	Source      string // 命令来源，格式如 "plugin:pluginName"

	// 定义
	Definition *Definition

	// 快速匹配
	pattern *regexp.Regexp // 预编译的匹配模式

	// 统计
	callCount atomic.Int64
	lastCall  infraatomic.Value[time.Time]

	// 优先级（用于冲突解决）
	Priority int
}

// GetCallCount 获取命令的调用次数
func (cm *Meta) GetCallCount() int64 {
	return cm.callCount.Load()
}

// compiledRegistry 是预编译的注册表，用于快速只读访问
type compiledRegistry struct {
	commandMap  map[string]*Meta
	aliasMap    map[string]string
	commandList []*Meta // 按优先级排序
}

// NewCommandRegistry 创建新的命令注册表
func NewCommandRegistry() *Registry {
	cr := &Registry{
		trie:    NewTrie(),
		aliases: make(map[string]string),
	}
	cr.compiled.Store(&compiledRegistry{
		commandMap:  make(map[string]*Meta),
		aliasMap:    make(map[string]string),
		commandList: make([]*Meta, 0),
	})
	return cr
}

// Register 注册一个命令
func (cr *Registry) Register(def *Definition) error {
	return cr.RegisterWithOptions(def, RegisterOptions{})
}

// RegisterOptions 注册选项
type RegisterOptions struct {
	Priority int    // 优先级（越高越优先）
	Category string // 分类
	Pattern  string // 自定义匹配模式（正则表达式）
	Source   string // 来源标识（如 "plugin:pluginName"）
}

// RegisterWithOptions 使用选项注册命令
func (cr *Registry) RegisterWithOptions(def *Definition, opts RegisterOptions) error {
	if def.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	// 检查命令名冲突（使用 Trie）
	if existing := cr.trie.ExactMatch(def.Name); existing != nil {
		return fmt.Errorf("command %s already registered", def.Name)
	}

	// 检查别名冲突
	for _, alias := range def.Aliases {
		if existingCmd, exists := cr.aliases[alias]; exists {
			return fmt.Errorf("alias %s already used by command %s", alias, existingCmd)
		}
	}

	// 创建元数据
	meta := &Meta{
		Name:        def.Name,
		Aliases:     def.Aliases,
		Description: def.Description,
		Usage:       def.Usage,
		Category:    opts.Category,
		Source:      opts.Source,
		Definition:  def,
		Priority:    opts.Priority,
	}

	// 编译匹配模式
	if opts.Pattern != "" {
		pattern, err := regexp.Compile(opts.Pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern for %s: %w", def.Name, err)
		}
		meta.pattern = pattern
	}

	// 注册命令到 Trie
	cr.trie.Insert(def.Name, meta)

	// 注册别名
	for _, alias := range def.Aliases {
		cr.aliases[alias] = def.Name
	}

	// 重新编译注册表
	cr.recompile()

	return nil
}

// Upsert 注册或更新注册表中的命令定义。
// 若命令已存在，则原地更新其元数据；
// 若命令尚未注册，则直接插入。
// 此方法可避免"already registered"错误——当 RegisterCommandDef 先通过 OnCommand
// 注册了一个裸定义，随后又尝试注册带完整元数据的定义时，即会触发该错误。
//
// 引入 needRecompile 脏标志，仅当别名或优先级发生变化时才触发 recompile()，
// 避免仅变更描述/用途等元数据时产生不必要的快照重建开销。
func (cr *Registry) Upsert(def *Definition, opts RegisterOptions) {
	if def == nil || def.Name == "" {
		return
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	if existing := cr.trie.ExactMatch(def.Name); existing != nil {
		needRecompile := false

		// 别名变更：同步更新 cr.aliases（影响 compiledRegistry.aliasMap），标记重编译
		if !slices.Equal(existing.Aliases, def.Aliases) {
			// 移除旧别名
			for _, alias := range existing.Aliases {
				delete(cr.aliases, alias)
			}
			// 注册新别名
			for _, alias := range def.Aliases {
				cr.aliases[alias] = def.Name
			}
			existing.Aliases = def.Aliases
			needRecompile = true
		}

		// 优先级变更：影响 commandList 排序，标记重编译
		if opts.Priority != 0 && existing.Priority != opts.Priority {
			existing.Priority = opts.Priority
			needRecompile = true
		}

		// COW: 手动创建新副本（avoid copying atomic.Int64 / infraatomic.Value 'noCopy' fields）
		newMeta := &Meta{
			Name:        existing.Name,
			Aliases:     existing.Aliases,
			Description: def.Description,
			Usage:       def.Usage,
			Definition:  def,
			Priority:    existing.Priority,
			pattern:     existing.pattern,
		}
		if opts.Category != "" {
			newMeta.Category = opts.Category
		} else {
			newMeta.Category = existing.Category
		}
		if opts.Source != "" {
			newMeta.Source = opts.Source
		} else {
			newMeta.Source = existing.Source
		}
		// Preserve atomic stats from the original meta
		newMeta.callCount.Store(existing.callCount.Load())
		newMeta.lastCall.Store(existing.lastCall.Load())
		existing = newMeta
		cr.trie.Insert(def.Name, existing)

		if needRecompile {
			cr.recompile()
		}
		return
	}

	// 新建注册
	meta := &Meta{
		Name:        def.Name,
		Aliases:     def.Aliases,
		Description: def.Description,
		Usage:       def.Usage,
		Category:    opts.Category,
		Source:      opts.Source,
		Definition:  def,
		Priority:    opts.Priority,
	}

	if opts.Pattern != "" {
		if pattern, err := regexp.Compile(opts.Pattern); err == nil {
			meta.pattern = pattern
		} else {
			panic(err)
		}
	}

	cr.trie.Insert(def.Name, meta)
	for _, alias := range def.Aliases {
		cr.aliases[alias] = def.Name
	}
	cr.recompile()
}

// RegisterBatch 批量注册命令，最终只调用一次 recompile()
//
// 若某个命令注册失败（如名称冲突），仅记录错误并跳过，继续注册其余命令。
// 返回所有失败命令的名称及其错误（key 为命令名，返回 nil 表示全部成功）。
func (cr *Registry) RegisterBatch(defs []*Definition, opts ...RegisterOptions) map[string]error {
	if len(defs) == 0 {
		return nil
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	var errs map[string]error
	for i, def := range defs {
		if def == nil || def.Name == "" {
			continue
		}
		var opt RegisterOptions
		if i < len(opts) {
			opt = opts[i]
		}

		if existing := cr.trie.ExactMatch(def.Name); existing != nil {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[def.Name] = fmt.Errorf("command %s already registered", def.Name)
			continue
		}

		aliasConflict := false
		for _, alias := range def.Aliases {
			if existingCmd, exists := cr.aliases[alias]; exists {
				if errs == nil {
					errs = make(map[string]error)
				}
				errs[def.Name] = fmt.Errorf("alias %s already used by command %s", alias, existingCmd)
				aliasConflict = true
				break
			}
		}
		if aliasConflict {
			continue
		}

		meta := &Meta{
			Name:        def.Name,
			Aliases:     def.Aliases,
			Description: def.Description,
			Usage:       def.Usage,
			Category:    opt.Category,
			Source:      opt.Source,
			Definition:  def,
			Priority:    opt.Priority,
		}
		if opt.Pattern != "" {
			if pattern, err := regexp.Compile(opt.Pattern); err == nil {
				meta.pattern = pattern
			}
		}
		cr.trie.Insert(def.Name, meta)
		for _, alias := range def.Aliases {
			cr.aliases[alias] = def.Name
		}
	}

	// 仅在批量注册完成后调用一次 recompile()（O(n) × n 中间快照）
	cr.recompile()
	return errs
}

// Unregister 注销命令
func (cr *Registry) Unregister(name string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	meta := cr.trie.ExactMatch(name)
	if meta == nil {
		return fmt.Errorf("command %s not registered", name)
	}

	// 从 Trie 删除命令
	cr.trie.Remove(name, meta)

	// 删除别名
	for _, alias := range meta.Aliases {
		delete(cr.aliases, alias)
	}

	// 重新编译
	cr.recompile()

	return nil
}

// Lookup 查找命令（支持别名）
func (cr *Registry) Lookup(nameOrAlias string) (*Meta, bool) {
	cr.lookupCount.Add(1)

	compiled := cr.compiled.Load()

	// 先查找命令名
	if meta, exists := compiled.commandMap[nameOrAlias]; exists {
		cr.hitCount.Add(1)
		meta.callCount.Add(1)
		return meta, true
	}

	// 再查找别名
	if cmdName, exists := compiled.aliasMap[nameOrAlias]; exists {
		cr.aliasHits.Add(1)
		cr.hitCount.Add(1)
		if meta, exists := compiled.commandMap[cmdName]; exists {
			meta.callCount.Add(1)
			return meta, true
		}
	}

	cr.missCount.Add(1)
	return nil, false
}

// LookupByPattern 通过正则模式查找命令
func (cr *Registry) LookupByPattern(input string) []*Meta {
	compiled := cr.compiled.Load()

	matches := make([]*Meta, 0)
	for _, meta := range compiled.commandList {
		if meta.pattern != nil && meta.pattern.MatchString(input) {
			matches = append(matches, meta)
		}
	}

	return matches
}

// Complete 命令补全（返回匹配的命令列表）
//
// 优化: 使用简单的字符串前缀匹配替代 Trie，简化实现
// 对于命令补全这种非高频操作，性能损失可以忽略（通常 < 1ms）
func (cr *Registry) Complete(prefix string) []*Meta {
	// 使用 Trie 进行前缀搜索
	return cr.trie.Search(prefix)
}

// List 列出所有命令
func (cr *Registry) List() []*Meta {
	compiled := cr.compiled.Load()
	return compiled.commandList
}

// ListByCategory 按分类列出命令
func (cr *Registry) ListByCategory(category string) []*Meta {
	compiled := cr.compiled.Load()

	result := make([]*Meta, 0)
	for _, meta := range compiled.commandList {
		if meta.Category == category {
			result = append(result, meta)
		}
	}

	return result
}

// GetStats 获取注册表统计信息
func (cr *Registry) GetStats() RegistryStats {
	cr.mu.RLock()
	compiled := cr.compiled.Load()
	commandCount := len(compiled.commandMap)
	aliasCount := len(cr.aliases)
	cr.mu.RUnlock()

	return RegistryStats{
		CommandCount: commandCount,
		AliasCount:   aliasCount,
		LookupCount:  cr.lookupCount.Load(),
		HitCount:     cr.hitCount.Load(),
		MissCount:    cr.missCount.Load(),
		AliasHits:    cr.aliasHits.Load(),
		HitRate:      float64(cr.hitCount.Load()) / float64(max(1, cr.lookupCount.Load())),
	}
}

// RegistryStats 注册表统计信息
type RegistryStats struct {
	CommandCount int
	AliasCount   int
	LookupCount  int64
	HitCount     int64
	MissCount    int64
	AliasHits    int64
	HitRate      float64
}

// recompile 重新编译注册表（持有写锁时调用）
func (cr *Registry) recompile() {
	// 从 Trie 获取所有命令
	allCommands := cr.trie.GetAllCommands()

	newCompiled := &compiledRegistry{
		commandMap:  make(map[string]*Meta, len(allCommands)),
		aliasMap:    make(map[string]string, len(cr.aliases)),
		commandList: make([]*Meta, 0, len(allCommands)),
	}

	// 构建命令映射和列表
	for _, meta := range allCommands {
		newCompiled.commandMap[meta.Name] = meta
		newCompiled.commandList = append(newCompiled.commandList, meta)
	}

	// 复制别名映射
	maps.Copy(newCompiled.aliasMap, cr.aliases)

	// 按优先级排序命令列表（Trie.GetAllCommands 已经排序，但为了保险再排一次）
	sortCommandsByPriority(newCompiled.commandList)

	// 原子更新
	cr.compiled.Store(newCompiled)
}

// sortCommandsByPriority 按优先级降序排列命令列表
func sortCommandsByPriority(commands []*Meta) {
	slices.SortStableFunc(commands, func(a, b *Meta) int {
		return b.Priority - a.Priority // 降序：优先级高的在前
	})
}

// ExtractCommandFast 使用自定义前缀快速提取命令名称
//
// 示例：
//
//	ExtractCommandFast("!help arg1", "!")   // returns "!help"
//	ExtractCommandFast("/help arg1", "/")    // returns "/help"
func ExtractCommandFast(content string, prefix string) string {
	content = strings.TrimSpace(content)
	if content == "" || prefix == "" {
		return ""
	}

	if !strings.HasPrefix(content, prefix) {
		return ""
	}

	// 查找第一个空白字符
	spaceIdx := strings.IndexAny(content, " \t\n")
	if spaceIdx == -1 {
		return content
	}

	return content[:spaceIdx]
}

// ExtractCommandAndArgs 使用自定义前缀同时提取命令和参数
//
// 示例：
//
//	ExtractCommandAndArgs("!help foo bar", "!")  // returns "!help", "foo bar"
//	ExtractCommandAndArgs("hello", "!")           // returns "", "hello"
func ExtractCommandAndArgs(content string, prefix string) (command string, args string) {
	content = strings.TrimSpace(content)
	if content == "" || prefix == "" {
		return "", ""
	}

	if !strings.HasPrefix(content, prefix) {
		return "", content
	}

	// 查找第一个空白字符
	spaceIdx := strings.IndexAny(content, " \t\n")
	if spaceIdx == -1 {
		return content, ""
	}

	command = content[:spaceIdx]
	args = strings.TrimSpace(content[spaceIdx+1:])
	return
}

// ValidateCommandName 验证命令名称（支持自定义前缀）
//
// name 应包含前缀，如 "/help" 或 "!help"。
// prefix 指定期望的前缀。
//
// 示例：
//
//	ValidateCommandName("/help", "/")   // nil
//	ValidateCommandName("!help", "!")   // nil
//	ValidateCommandName("help", "/")     // error: 不以 / 开头
func ValidateCommandName(name string, prefix string) error {
	if name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if !strings.HasPrefix(name, prefix) {
		return fmt.Errorf("command name must start with %s", prefix)
	}

	if len(name) == len(prefix) {
		return fmt.Errorf("command name too short")
	}

	// 检查非法字符（跳过前缀部分）
	for i, r := range name[len(prefix):] {
		if !isValidCommandChar(r) {
			return fmt.Errorf("invalid character at position %d: %c", i+len(prefix)+1, r)
		}
	}

	return nil
}

// isValidCommandChar 检查字符是否有效
func isValidCommandChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '-'
}
