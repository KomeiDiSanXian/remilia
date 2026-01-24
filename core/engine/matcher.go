package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

type matcherRuntime struct {
	mu          sync.RWMutex
	deleted     bool
	useCount    int32
	maxUseCount int32
	createdAt   time.Time
	expiresAt   time.Time
	isTemp      int32
}

// Matcher 事件匹配器
type Matcher struct {
	rt          matcherRuntime
	isBlock     bool
	priority    uint
	EventType   dto.EventType
	Rules       []context.Rule
	Handler     context.Handler
	coordinator MatcherCoordinator
	Source      string
	group       string
	middlewares []Middleware

	combinedChain atomic.Value
	cachedGen     struct {
		global uint64
		group  uint64
	}
	cacheMu sync.RWMutex

	definition *command.Definition // 命令定义（可选，包含所有元数据）
}

func (m *Matcher) copy() *Matcher {
	newRules := make([]context.Rule, len(m.Rules))
	copy(newRules, m.Rules)

	newMiddlewares := make([]Middleware, len(m.middlewares))
	copy(newMiddlewares, m.middlewares)

	return &Matcher{
		rt:          matcherRuntime{isTemp: atomic.LoadInt32(&m.rt.isTemp)},
		EventType:   m.EventType,
		Rules:       newRules,
		isBlock:     m.isBlock,
		priority:    m.priority,
		Handler:     m.Handler,
		Source:      m.Source,
		group:       m.group,
		middlewares: newMiddlewares,
		definition:  m.definition, // 定义为指针，浅拷贝
	}
}

// GetCommand 获取匹配器的触发命令
func (m *Matcher) GetCommand() string {
	if m == nil {
		return ""
	}
	m.rt.mu.RLock()
	def := m.definition
	m.rt.mu.RUnlock()

	if def != nil && def.Name != "" {
		return "/" + def.Name
	}
	return ""
}

// GetSource 获取匹配器的来源标识（实现 context.Matcher）
func (m *Matcher) GetSource() string {
	if m == nil {
		return ""
	}
	return m.Source
}

// SetSource 设置匹配器的来源标识
func (m *Matcher) SetSource(source string) *Matcher {
	m.rt.mu.Lock()
	m.Source = source
	m.rt.mu.Unlock()
	return m
}

// IsTemp returns true if the matcher is a temporary matcher managed by TempManager.
func (m *Matcher) IsTemp() bool {
	return atomic.LoadInt32(&m.rt.isTemp) == 1
}

// getCombinedChain 获取组合的中间件链（无锁读取）
func (m *Matcher) getCombinedChain() []Middleware {
	if v := m.combinedChain.Load(); v != nil {
		return v.([]Middleware)
	}
	return nil
}

// getChainCache 返回链缓存及代际号的快照（线程安全）
func (m *Matcher) getChainCache() ([]Middleware, uint64, uint64) {
	m.cacheMu.RLock()
	chain := m.getCombinedChain()
	globalGen := m.cachedGen.global
	groupGen := m.cachedGen.group
	m.cacheMu.RUnlock()
	return chain, globalGen, groupGen
}

// setCombinedChain 设置组合的中间件链（写操作）
func (m *Matcher) setCombinedChain(chain []Middleware, globalGen, groupGen uint64) {
	m.cacheMu.Lock()
	m.cachedGen.global = globalGen
	m.cachedGen.group = groupGen
	m.combinedChain.Store(chain)
	m.cacheMu.Unlock()
}

// Delete 从所属引擎中删除该匹配器
func (m *Matcher) Delete() {
	m.rt.mu.Lock()
	if m.rt.deleted {
		m.rt.mu.Unlock()
		return
	}

	m.rt.deleted = true
	coordinator := m.coordinator
	m.rt.mu.Unlock()

	if coordinator != nil {
		coordinator.DeleteMatcher(m)
	}
}

// IsDeleted 返回 matcher 是否已经被删除
func (m *Matcher) IsDeleted() bool {
	m.rt.mu.RLock()
	defer m.rt.mu.RUnlock()
	return m.rt.deleted
}

// isNoop 检查是否为 noop matcher
func (m *Matcher) isNoop() bool {
	return m != nil && m.Source == "noop"
}

// Match 检查事件是否匹配此 Matcher
func (m *Matcher) Match(ctx *context.Context) bool {
	m.rt.mu.RLock()
	if m.rt.deleted {
		m.rt.mu.RUnlock()
		return false
	}
	rs := m.Rules
	m.rt.mu.RUnlock()

	for _, rule := range rs {
		if !rule(ctx) {
			return false
		}
	}

	// 双重检查：在匹配过程中可能被删除
	m.rt.mu.RLock()
	deleted := m.rt.deleted
	m.rt.mu.RUnlock()

	if deleted {
		return false
	}

	return true
}

// Handle 设置 Matcher 的处理函数（无错误返回）
func (m *Matcher) Handle(handler context.Handler) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	m.Handler = handler
	coord := m.coordinator
	m.rt.mu.Unlock()
	if coord != nil {
		coord.RebuildMatcherChain(m)
	}
	return m
}

// SetPriority 设置 Matcher 的优先级
func (m *Matcher) SetPriority(priority uint) *Matcher {
	if m.isNoop() {
		return m
	}

	m.rt.mu.Lock()
	changed := m.priority != priority
	m.priority = priority
	coord := m.coordinator
	muEvent := m.EventType
	isTempManager := atomic.LoadInt32(&m.rt.isTemp) == 1
	m.rt.mu.Unlock()

	if changed && coord != nil {
		if isTempManager {
			coord.UpdateTempMatcherPriority(m)
		} else {
			coord.InvalidateSortedCache(muEvent)
		}
	}

	return m
}

// SetBlock 设置 Matcher 是否阻塞后续匹配
func (m *Matcher) SetBlock(block bool) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	m.isBlock = block
	m.rt.mu.Unlock()
	return m
}

// SetTemp 设置 Matcher 是否为临时匹配器
func (m *Matcher) SetTemp(temp bool) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.rt.deleted {
		m.rt.mu.Unlock()
		return m
	}

	currentIsTemp := atomic.LoadInt32(&m.rt.isTemp) == 1
	coord := m.coordinator

	if temp {
		atomic.StoreInt32(&m.rt.isTemp, 1)
		m.rt.maxUseCount = 1
		m.rt.useCount = 0
	} else {
		atomic.StoreInt32(&m.rt.isTemp, 0)
		m.rt.maxUseCount = 0
		m.rt.useCount = 0
	}
	m.rt.mu.Unlock()

	if temp != currentIsTemp && coord != nil {
		if temp {
			coord.MigrateMatcherToTemp(m)
		} else {
			coord.MigrateMatcherFromTemp(m)
		}
	}
	return m
}

func (m *Matcher) deletedOrLocked() bool {
	return m.rt.deleted
}

// SetTempWithMaxUse 将 matcher 标记为临时匹配器
func (m *Matcher) SetTempWithMaxUse(maxUse int) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.rt.deleted {
		m.rt.mu.Unlock()
		return m
	}

	needsMigration := atomic.LoadInt32(&m.rt.isTemp) == 0
	coord := m.coordinator
	needsMigration = needsMigration && coord != nil

	atomic.StoreInt32(&m.rt.isTemp, 1)
	if maxUse <= 0 {
		m.rt.maxUseCount = 1
	} else {
		m.rt.maxUseCount = int32(maxUse)
	}
	m.rt.useCount = 0
	m.rt.mu.Unlock()

	if needsMigration {
		coord.MigrateMatcherToTemp(m)
	}
	return m
}

func (m *Matcher) SetTempWithTimeout(timeout time.Duration) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.rt.deleted {
		m.rt.mu.Unlock()
		return m
	}

	needsMigration := atomic.LoadInt32(&m.rt.isTemp) == 0
	coord := m.coordinator
	needsMigration = needsMigration && coord != nil

	atomic.StoreInt32(&m.rt.isTemp, 1)
	m.rt.createdAt = time.Now()
	m.rt.expiresAt = m.rt.createdAt.Add(timeout)
	m.rt.mu.Unlock()

	if needsMigration {
		coord.MigrateMatcherToTemp(m)
	}
	return m
}

// Use 为当前 matcher 注册局部中间件
func (m *Matcher) Use(mw ...Middleware) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	m.middlewares = append(m.middlewares, mw...)
	m.invalidateCombinedChain()
	coord := m.coordinator
	m.rt.mu.Unlock()
	if coord != nil {
		coord.RebuildMatcherChain(m)
	}
	return m
}

// Command 添加命令匹配规则（链式调用）
func (m *Matcher) Command(cmd string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnCommand(cmd))
	return m
}

// Keyword 添加关键词匹配规则
func (m *Matcher) Keyword(keyword string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnKeyword(keyword))
	return m
}

// Prefix 添加前缀匹配规则
func (m *Matcher) Prefix(prefix string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnPrefix(prefix))
	return m
}

// Suffix 添加后缀匹配规则
func (m *Matcher) Suffix(suffix string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnSuffix(suffix))
	return m
}

// FullMatch 添加完全匹配规则
func (m *Matcher) FullMatch(text string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnFullMatch(text))
	return m
}

// Regex 添加正则表达式匹配规则
func (m *Matcher) Regex(pattern string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, context.OnRegex(pattern))
	return m
}

// Where 添加自定义规则
func (m *Matcher) Where(rule context.Rule) *Matcher {
	if m.isNoop() {
		return m
	}
	m.Rules = append(m.Rules, rule)
	return m
}

func (m *Matcher) invalidateCombinedChain() {
	if m == nil {
		return
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	m.cachedGen.global = 0
	m.cachedGen.group = 0
	defer func() { _ = recover() }()
	m.combinedChain.Store(nil)
}

// ensureChain ensures the combined chain is cached and valid.
func (m *Matcher) ensureChain(globalChain []Middleware, globalGen uint64, groupChain []Middleware, groupGen uint64) {
	if m == nil {
		return
	}

	if chain, gGen, pGen := m.getChainCache(); chain != nil {
		if gGen == globalGen && pGen == groupGen {
			return
		}
	}

	m.rt.mu.RLock()
	defer m.rt.mu.RUnlock()

	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	if m.cachedGen.global == globalGen && m.cachedGen.group == groupGen {
		if m.combinedChain.Load() != nil {
			return
		}
	}

	locals := m.middlewares
	chain := make([]Middleware, 0, len(globalChain)+len(groupChain)+len(locals))
	chain = append(chain, globalChain...)
	chain = append(chain, groupChain...)
	chain = append(chain, locals...)

	m.cachedGen.global = globalGen
	m.cachedGen.group = groupGen
	m.combinedChain.Store(chain)
}

// getPriority returns priority in a threadsafe way.
func (m *Matcher) getPriority() uint {
	if m == nil {
		return 0
	}
	m.rt.mu.RLock()
	p := m.priority
	m.rt.mu.RUnlock()
	return p
}

// isBlocking returns whether matcher should block subsequent handlers.
func (m *Matcher) isBlocking() bool {
	if m == nil {
		return false
	}
	m.rt.mu.RLock()
	b := m.isBlock
	m.rt.mu.RUnlock()
	return b
}

// BindCommand 手动绑定触发命令
//
// 此方法会自动创建或更新 Definition.Name
func (m *Matcher) BindCommand(cmd string) *Matcher {
	if m.isNoop() {
		return m
	}

	m.rt.mu.Lock()
	cmdName := strings.TrimPrefix(strings.TrimSpace(cmd), "/")
	if cmdName != "" {
		if m.definition == nil {
			m.definition = &command.Definition{Name: cmdName}
		} else {
			m.definition.Name = cmdName
		}
	}
	coord := m.coordinator
	m.rt.mu.Unlock()

	if coord != nil {
		coord.UpdateMatcherCommand(m)
	}
	return m
}

// SetGroup sets the matcher group name.
func (m *Matcher) SetGroup(group string) *Matcher {
	m.rt.mu.Lock()
	m.group = strings.TrimSpace(group)
	m.invalidateCombinedChain()
	coord := m.coordinator
	m.rt.mu.Unlock()

	if coord != nil {
		coord.RebuildMatcherChain(m)
	}

	return m
}

// GetGroup returns the matcher group name.
func (m *Matcher) GetGroup() string {
	m.rt.mu.RLock()
	g := m.group
	m.rt.mu.RUnlock()
	return g
}

// SetDefinition 设置命令定义（用于 Help 生成和命令解析）
func (m *Matcher) SetDefinition(def *command.Definition) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	m.definition = def
	m.rt.mu.Unlock()
	return m
}

// GetDefinition 获取命令定义
func (m *Matcher) GetDefinition() *command.Definition {
	m.rt.mu.RLock()
	defer m.rt.mu.RUnlock()
	return m.definition
}

// SetDescription 设置命令描述（便捷方法）
func (m *Matcher) SetDescription(desc string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Description = desc
	m.rt.mu.Unlock()
	return m
}

// SetUsage 设置命令用法（便捷方法）
func (m *Matcher) SetUsage(usage string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Usage = usage
	m.rt.mu.Unlock()
	return m
}

// SetCategory 设置命令分类（便捷方法）
func (m *Matcher) SetCategory(category string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Category = category
	m.rt.mu.Unlock()
	return m
}

// SetAliases 设置命令别名（便捷方法）
func (m *Matcher) SetAliases(aliases ...string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Aliases = aliases
	m.rt.mu.Unlock()
	return m
}

// SetExamples 设置命令示例（便捷方法）
func (m *Matcher) SetExamples(examples ...string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Examples = examples
	m.rt.mu.Unlock()
	return m
}

// SetHidden 设置是否在帮助中隐藏（便捷方法）
func (m *Matcher) SetHidden(hidden bool) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Hidden = hidden
	m.rt.mu.Unlock()
	return m
}

// SetPermissions 设置所需权限（便捷方法）
func (m *Matcher) SetPermissions(permissions ...string) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	m.definition.Permissions = permissions
	m.rt.mu.Unlock()
	return m
}
