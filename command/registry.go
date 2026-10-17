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
	"unicode"
	"unicode/utf8"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/trie"
)

// Registry 是一个高性能的命令注册表
// 提供快速的命令查找、别名解析和统计功能
//
// 优化: 使用单一 Trie 树索引，移除冗余的 commands map，减少 40-50% 内存占用
type Registry struct {
	// 索引结构 - 统一使用 Trie 树
	trie *trie.Trie[*Meta] // Trie 树用于精确查找和前缀搜索

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
		trie:    trie.New[*Meta](),
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

// validateAliasLocked 校验别名是否可用（需持有 cr.mu）。
//
// owner 是本次注册/更新的命令名。冲突包括：
//   - 空别名；
//   - 别名等于命令自身名称（Lookup 先查命令名，该别名永远不会生效）；
//   - 同一定义内重复的别名；
//   - 别名已被其它命令占用；
//   - 别名与已注册的命令名相同（Lookup 会命中那个命令，此处注册的别名成为
//     永不生效的死别名——历史实现会静默写入 aliases 表）。
func (cr *Registry) validateAliasLocked(owner string, aliases []string) error {
	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		switch alias {
		case "":
			return fmt.Errorf("command %s: empty alias", owner)
		case owner:
			return fmt.Errorf("command %s: alias %s equals the command name", owner, alias)
		}
		if _, dup := seen[alias]; dup {
			return fmt.Errorf("command %s: duplicate alias %s", owner, alias)
		}
		seen[alias] = struct{}{}

		if existing, exists := cr.aliases[alias]; exists && existing != owner {
			return fmt.Errorf("command %s: alias %s already used by command %s", owner, alias, existing)
		}
		if _, ok := cr.trie.ExactMatch(alias); ok {
			return fmt.Errorf("command %s: alias %s conflicts with an existing command name", owner, alias)
		}
	}
	return nil
}

// RegisterWithOptions 使用选项注册命令
func (cr *Registry) RegisterWithOptions(def *Definition, opts RegisterOptions) error {
	if def.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	// 检查命令名冲突（使用 Trie）
	if _, ok := cr.trie.ExactMatch(def.Name); ok {
		return fmt.Errorf("command %s already registered", def.Name)
	}

	// 检查别名冲突（其它命令占用、遮蔽命令名、自身重复等）
	if err := cr.validateAliasLocked(def.Name, def.Aliases); err != nil {
		return err
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
//
// 若命令已存在，则以其为新版元数据（保留调用统计），并刷新编译快照；
// 若命令尚未注册，则直接插入。
// 此方法可避免"already registered"错误——当 RegisterCommandDef 先通过 OnCommand
// 注册了一个裸定义，随后又尝试注册带完整元数据的定义时，即会触发该错误。
//
// 更新路径必须重建编译快照：compiledRegistry 中保存的是 *Meta 指针，
// 只改字段不会让 Lookup/List 看到变化。写入也统一使用 Trie.Replace，
// 确保同一命令只保留一份元数据。
//
// # 返回值
//
// 别名冲突（含遮蔽命令名、与自身同名、重复）视为注册失败：不写入任何数据，
// 返回错误由调用方告警或修正定义。非法正则模式不阻断注册——模式是可选字段，
// 此时命令照常写入、模式被忽略，并返回该错误（此前直接对非法模式 panic）。
//
// 调用方必须检查返回值：忽略它意味着"命令注册了但输入不完整"。
func (cr *Registry) Upsert(def *Definition, opts RegisterOptions) error {
	if def == nil || def.Name == "" {
		return fmt.Errorf("command definition name cannot be empty")
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	if err := cr.validateAliasLocked(def.Name, def.Aliases); err != nil {
		return err
	}

	var patternErr error

	if existing, ok := cr.trie.ExactMatch(def.Name); ok {
		// 别名变更：同步更新 cr.aliases（影响 compiledRegistry.aliasMap）
		if !slices.Equal(existing.Aliases, def.Aliases) {
			for _, alias := range existing.Aliases {
				delete(cr.aliases, alias)
			}
			for _, alias := range def.Aliases {
				cr.aliases[alias] = def.Name
			}
		}

		priority := existing.Priority
		if opts.Priority != 0 {
			priority = opts.Priority
		}

		pattern := existing.pattern
		if opts.Pattern != "" {
			if compiled, err := regexp.Compile(opts.Pattern); err == nil {
				pattern = compiled
			} else {
				patternErr = fmt.Errorf("invalid pattern for %s: %w", def.Name, err)
				logger.Warnf("[command] upsert %s: invalid pattern %q ignored: %v", def.Name, opts.Pattern, err)
			}
		}

		// COW: 手动创建新副本（avoid copying atomic.Int64 / infraatomic.Value 'noCopy' fields）
		newMeta := &Meta{
			Name:        def.Name,
			Aliases:     def.Aliases,
			Description: def.Description,
			Usage:       def.Usage,
			Category:    existing.Category,
			Source:      existing.Source,
			Definition:  def,
			Priority:    priority,
			pattern:     pattern,
		}
		if opts.Category != "" {
			newMeta.Category = opts.Category
		}
		if opts.Source != "" {
			newMeta.Source = opts.Source
		}
		// Preserve atomic stats from the original meta
		newMeta.callCount.Store(existing.callCount.Load())
		newMeta.lastCall.Store(existing.lastCall.Load())

		// Replace 而不是 Insert：Insert 会向同一 key 追加新值，
		// 使 Lookup 继续命中旧元数据、List/Complete 出现重复项，
		// 且 Unregister 只能删掉其中一个。
		cr.trie.Replace(def.Name, newMeta)
		cr.recompile()
		return patternErr
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
			// 与 RegisterWithOptions 返回 error、RegisterBatch 跳过的口径一致：
			// 非法模式忽略（命令本身照常注册）并把错误返回给调用方。
			patternErr = fmt.Errorf("invalid pattern for %s: %w", def.Name, err)
			logger.Warnf("[command] upsert %s: invalid pattern %q ignored: %v", def.Name, opts.Pattern, err)
		}
	}

	cr.trie.Insert(def.Name, meta)
	for _, alias := range def.Aliases {
		cr.aliases[alias] = def.Name
	}
	cr.recompile()
	return patternErr
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

		if _, ok := cr.trie.ExactMatch(def.Name); ok {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[def.Name] = fmt.Errorf("command %s already registered", def.Name)
			continue
		}

		aliasConflict := false
		// 与 Register/Upsert 同一套校验：其它命令占用、遮蔽命令名、自身重复
		if err := cr.validateAliasLocked(def.Name, def.Aliases); err != nil {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[def.Name] = err
			aliasConflict = true
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
//
// 会清理该名称下的全部元数据：更新路径历史上会向同一 key 追加新值，
// 只删第一个会让命令在注销后依然存在。
func (cr *Registry) Unregister(name string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if _, ok := cr.trie.ExactMatch(name); !ok {
		return fmt.Errorf("command %s not registered", name)
	}

	for {
		meta, ok := cr.trie.ExactMatch(name)
		if !ok {
			break
		}

		// 从 Trie 删除命令
		cr.trie.Remove(name, meta)

		// 删除别名（仅当别名仍指向该命令时）
		for _, alias := range meta.Aliases {
			if cr.aliases[alias] == name {
				delete(cr.aliases, alias)
			}
		}
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
// 返回按「优先级降序、名称升序」排序的确定顺序副本，并过滤 Hidden=true 的
// 隐藏命令（自动生成的别名 Matcher 即为隐藏项，不应出现在补全候选里）。
//
// 优化: 使用 Trie 前缀搜索定位候选，随后排序保证输出稳定。
// 对于命令补全这种非高频操作，性能损失可以忽略（通常 < 1ms）
func (cr *Registry) Complete(prefix string) []*Meta {
	return visibleSorted(cr.trie.SearchPrefix(prefix))
}

// List 列出所有对用户可见的命令（不含 Hidden）。
//
// 返回编译快照的副本，调用方修改返回值不会影响注册表；顺序为
// 「优先级降序、名称升序」，与重新编译无关（此前依赖 map/trie 迭代顺序，
// 同优先级命令的顺序会在重建之间抖动）。
func (cr *Registry) List() []*Meta {
	return visibleSorted(cr.compiled.Load().commandList)
}

// ListAll 列出全部命令（含 Hidden）。
//
// 供需要完整集合的内部工具使用（诊断、审计）；面向用户的列表请用 [List]。
func (cr *Registry) ListAll() []*Meta {
	return sortedCopy(cr.compiled.Load().commandList)
}

// visibleSorted 过滤隐藏项后排序并复制（无结果时返回 nil，与 Trie 查询口径一致）。
func visibleSorted(src []*Meta) []*Meta {
	out := make([]*Meta, 0, len(src))
	for _, meta := range src {
		if meta == nil || isHiddenMeta(meta) {
			continue
		}
		out = append(out, meta)
	}
	if len(out) == 0 {
		return nil
	}
	sortCommandsDeterministic(out)
	return out
}

// sortedCopy 排序并复制（不过滤隐藏项；无结果时返回 nil）。
func sortedCopy(src []*Meta) []*Meta {
	out := make([]*Meta, 0, len(src))
	for _, meta := range src {
		if meta == nil {
			continue
		}
		out = append(out, meta)
	}
	if len(out) == 0 {
		return nil
	}
	sortCommandsDeterministic(out)
	return out
}

// isHiddenMeta 报告命令是否标记为隐藏。
func isHiddenMeta(meta *Meta) bool {
	return meta.Definition != nil && meta.Definition.Hidden
}

// sortCommandsDeterministic 按「优先级降序、名称升序」排序。
//
// 同优先级时按名称升序：编译快照来自 trie 遍历（顺序不稳定），
// 只按优先级排序会让同优先级命令的输出顺序在每次 recompile 后抖动。
func sortCommandsDeterministic(commands []*Meta) {
	slices.SortStableFunc(commands, func(a, b *Meta) int {
		if a.Priority != b.Priority {
			return b.Priority - a.Priority
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// ListByCategory 按分类列出命令（返回副本，顺序确定）。
func (cr *Registry) ListByCategory(category string) []*Meta {
	compiled := cr.compiled.Load()

	result := make([]*Meta, 0)
	for _, meta := range compiled.commandList {
		if meta.Category == category {
			result = append(result, meta)
		}
	}
	sortCommandsDeterministic(result)

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
	allCommands := cr.trie.GetAll()

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

	// 按「优先级降序、名称升序」排序命令列表（顺序确定，不受 trie 遍历顺序影响）
	sortCommandsDeterministic(newCompiled.commandList)

	// 原子更新
	cr.compiled.Store(newCompiled)
}

// ExtractCommandFast 使用自定义前缀快速提取命令名称
//
// 示例：
//
//	ExtractCommandFast("!help arg1", "!")   // returns "!help"
//	ExtractCommandFast("/help arg1", "/")    // returns "/help"
//
// 空白判定使用 unicode.IsSpace，与解析器及引擎保持一致（全角空格等同样分隔）。
func ExtractCommandFast(content string, prefix string) string {
	content = strings.TrimSpace(content)
	if content == "" || prefix == "" {
		return ""
	}

	if !strings.HasPrefix(content, prefix) {
		return ""
	}

	// 查找第一个空白字符
	spaceIdx := strings.IndexFunc(content, unicode.IsSpace)
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
//
// 空白判定使用 unicode.IsSpace，与解析器及引擎保持一致（全角空格等同样分隔）。
func ExtractCommandAndArgs(content string, prefix string) (command string, args string) {
	content = strings.TrimSpace(content)
	if content == "" || prefix == "" {
		return "", ""
	}

	if !strings.HasPrefix(content, prefix) {
		return "", content
	}

	// 查找第一个空白字符。注意 IndexFunc 返回的是字节下标，
	// 全角空格等分隔符本身可能是多字节字符，切分参数时必须按 rune 宽度跳过整个分隔符，
	// 否则会撕裂字符、把残余字节带进参数。
	spaceIdx := strings.IndexFunc(content, unicode.IsSpace)
	if spaceIdx == -1 {
		return content, ""
	}

	_, sepSize := utf8.DecodeRuneInString(content[spaceIdx:])
	command = content[:spaceIdx]
	args = strings.TrimSpace(content[spaceIdx+sepSize:])
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
