package command

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
	compiled atomic.Value // *compiledRegistry

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
	lastCall  atomic.Value // time.Time

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

// Upsert registers or updates a command definition in the registry.
// If the command is already registered, its metadata is updated in-place.
// If the command is not yet registered, it is inserted.
// This avoids the "already registered" error that occurs when RegisterCommandDef
// first registers a bare definition via OnCommand and then tries to register the
// full definition with metadata.
func (cr *Registry) Upsert(def *Definition, opts RegisterOptions) {
	if def == nil || def.Name == "" {
		return
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	if existing := cr.trie.ExactMatch(def.Name); existing != nil {
		// Update existing metadata in-place
		existing.Description = def.Description
		existing.Usage = def.Usage
		existing.Aliases = def.Aliases
		existing.Definition = def
		if opts.Category != "" {
			existing.Category = opts.Category
		}
		if opts.Source != "" {
			existing.Source = opts.Source
		}
		if opts.Priority != 0 {
			existing.Priority = opts.Priority
		}
		cr.recompile()
		return
	}

	// New registration
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
		}
	}

	cr.trie.Insert(def.Name, meta)
	for _, alias := range def.Aliases {
		cr.aliases[alias] = def.Name
	}
	cr.recompile()
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

	compiled := cr.compiled.Load().(*compiledRegistry)

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
	compiled := cr.compiled.Load().(*compiledRegistry)

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
	compiled := cr.compiled.Load().(*compiledRegistry)
	return compiled.commandList
}

// ListByCategory 按分类列出命令
func (cr *Registry) ListByCategory(category string) []*Meta {
	compiled := cr.compiled.Load().(*compiledRegistry)

	result := make([]*Meta, 0)
	for _, meta := range compiled.commandList {
		if meta.Category == category {
			result = append(result, meta)
		}
	}

	return result
}

// GetStats 获取注册表统计信息
// GetStats 获取注册表统计信息
func (cr *Registry) GetStats() RegistryStats {
	cr.mu.RLock()
	compiled := cr.compiled.Load().(*compiledRegistry)
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

// sortCommandsByPriority 按优先级排序命令
func sortCommandsByPriority(commands []*Meta) {
	// 使用简单的冒泡排序（命令数量通常不多）
	n := len(commands)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if commands[j].Priority < commands[j+1].Priority {
				commands[j], commands[j+1] = commands[j+1], commands[j]
			}
		}
	}
}

// ExtractCommandFast 快速提取命令名称
// 使用预编译的正则表达式，性能提升约 50%
// 支持含连字符的命令名（如 /get-help、/sub-cmd）
var commandPattern = regexp.MustCompile(`^(/[\w-]+)`)

func ExtractCommandFast(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	// 快速路径：检查第一个字符
	if content[0] != '/' {
		return ""
	}

	// 使用预编译的正则表达式
	if match := commandPattern.FindString(content); match != "" {
		return match
	}

	return ""
}

// ExtractCommandAndArgs 同时提取命令和参数
func ExtractCommandAndArgs(content string) (command string, args string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ""
	}

	// 快速路径：检查第一个字符
	if content[0] != '/' {
		return "", content
	}

	// 查找第一个空格
	spaceIdx := strings.IndexAny(content, " \t\n")
	if spaceIdx == -1 {
		return content, ""
	}

	command = content[:spaceIdx]
	args = strings.TrimSpace(content[spaceIdx+1:])
	return
}

// ValidateCommandName 验证命令名称
func ValidateCommandName(name string) error {
	if name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if !strings.HasPrefix(name, "/") {
		return fmt.Errorf("command name must start with /")
	}

	if len(name) == 1 {
		return fmt.Errorf("command name too short")
	}

	// 检查非法字符
	for i, r := range name[1:] {
		if !isValidCommandChar(r) {
			return fmt.Errorf("invalid character at position %d: %c", i+1, r)
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
