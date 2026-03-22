package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
)

type matcherRuntime struct {
	// deleted and disabled are atomic so Match() and the early-return check in
	// invokeHandler can read them without acquiring any lock.
	// Writers still hold mu to keep composite updates (useCount + deleted) atomic.
	deleted  atomic.Bool
	disabled atomic.Bool

	mu          sync.RWMutex
	useCount    int32
	maxUseCount int32
	createdAt   time.Time
	expiresAt   time.Time
	isTemp      int32
}

// Matcher 事件匹配器
type Matcher struct {
	rt matcherRuntime
	// priority and isBlock are hot-path fields read in mergeSortedMatchersSix and
	// processEventContext respectively (1000× per event).  Using atomics eliminates
	// the RWMutex.RLock/RUnlock pair in getPriority/isBlocking — the dominant CPU
	// cost in those two functions (180 ms + 40 ms in the large-scenario profile).
	priority atomic.Uint64
	isBlock  atomic.Bool
	// commandIndexed is set once (before the matcher is published) for matchers
	// created by OnCommand/RegisterCommandDef.  When true, Match() skips Rules[0]
	// (the OnCommand prefix check) because the command was already matched via the
	// commandIndex O(1) lookup — saving ~200 ms / 10 % CPU in the large scenario.
	// Uses atomic.Bool to prevent data races between rebuildIndex writes and
	// concurrent Match() reads on shared *Matcher objects (COW shares pointers).
	commandIndexed atomic.Bool
	EventType      EventType
	Rules          []context.Rule
	Handler        context.Handler
	coordinator    MatcherCoordinator
	Source         string
	group          string
	middlewares    []context.Middleware

	combinedChain    atomic.Value // []Middleware  — the raw middleware slice
	compiledHandlers atomic.Value // compiledChain — pre-built iterative handler slice
	// compiledVersion is a monotonically increasing counter that is bumped
	// whenever the combined middleware chain OR the Handler changes.
	// getOrBuildIterChain compares this counter instead of computing a
	// reflect-based FNV fingerprint on every hot-path invocation.
	compiledVersion atomic.Uint64

	cachedGen struct {
		global uint64
		group  uint64
	}
	cacheMu sync.RWMutex

	definition *command.Definition // 命令定义（可选，包含所有元数据）
}

// compiledChain caches the outermost composed handler produced by
// getOrBuildIterChain.
//
// Structure after building N middlewares + 1 handler:
//
//	head = chain[0](chain[1](...chain[N-1](handler)...))
//
// Each chain[i] returns a closure that captures chain[i+1]'s result as "next".
// Calling head() executes the full chain through these closure captures —
// no slice iteration needed; only head is stored here.
//
// version mirrors m.compiledVersion at build time; a version mismatch
// triggers a rebuild on the next slow-path call.
type compiledChain struct {
	head    context.Handler // entry point: outermost middleware wrapping the actual handler
	version uint64          // snapshot of m.compiledVersion when this chain was compiled
}

func (m *Matcher) copy() *Matcher {
	newRules := make([]context.Rule, len(m.Rules))
	copy(newRules, m.Rules)

	newMiddlewares := make([]context.Middleware, len(m.middlewares))
	copy(newMiddlewares, m.middlewares)

	newM := &Matcher{
		rt:          matcherRuntime{isTemp: atomic.LoadInt32(&m.rt.isTemp)},
		EventType:   m.EventType,
		Rules:       newRules,
		Handler:     m.Handler,
		Source:      m.Source,
		group:       m.group,
		middlewares: newMiddlewares,
		definition:  m.definition, // 定义为指针，浅拷贝
	}
	newM.priority.Store(m.priority.Load())
	newM.isBlock.Store(m.isBlock.Load())
	newM.commandIndexed.Store(m.commandIndexed.Load())
	return newM
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
func (m *Matcher) getCombinedChain() []context.Middleware {
	if v := m.combinedChain.Load(); v != nil {
		return v.([]context.Middleware)
	}
	return nil
}

// getChainCache 返回链缓存及代际号的快照（线程安全）
func (m *Matcher) getChainCache() ([]context.Middleware, uint64, uint64) {
	m.cacheMu.RLock()
	chain := m.getCombinedChain()
	globalGen := m.cachedGen.global
	groupGen := m.cachedGen.group
	m.cacheMu.RUnlock()
	return chain, globalGen, groupGen
}

// setCombinedChain 设置组合的中间件链（写操作）
func (m *Matcher) setCombinedChain(chain []context.Middleware, globalGen, groupGen uint64) {
	m.cacheMu.Lock()
	m.cachedGen.global = globalGen
	m.cachedGen.group = groupGen
	m.combinedChain.Store(chain)
	m.cacheMu.Unlock()
}

// Delete 从所属引擎中删除该匹配器
func (m *Matcher) Delete() {
	m.rt.mu.Lock()
	if m.rt.deleted.Load() {
		m.rt.mu.Unlock()
		return
	}

	m.rt.deleted.Store(true)
	coordinator := m.coordinator
	m.rt.mu.Unlock()

	if coordinator != nil {
		coordinator.DeleteMatcher(m)
	}
}

// IsDeleted 返回 matcher 是否已经被删除
func (m *Matcher) IsDeleted() bool {
	return m.rt.deleted.Load()
}

// IsDisabled 返回 Matcher 是否处于暂停状态
func (m *Matcher) IsDisabled() bool {
	return m.rt.disabled.Load()
}

// disable 将 Matcher 标记为暂停（不影响 deleted）
func (m *Matcher) disable() {
	m.rt.disabled.Store(true)
}

// enable 恢复 Matcher 响应
func (m *Matcher) enable() {
	m.rt.disabled.Store(false)
}

// isNoop 检查是否为 noop matcher
func (m *Matcher) isNoop() bool {
	return m != nil && m.Source == "noop"
}

// Match 检查事件是否匹配此 Matcher
//
// 性能优化：
//   - deleted/disabled 是 atomic.Bool，无需 RWMutex 即可读取。
//   - Rules 在注册后不再修改，也无需加锁读取。
//   - commandIndexed 为 true 时跳过 Rules[0]（OnCommand 前缀规则）——
//     该规则已由 commandIndex O(1) 查找隐式验证，节省约 10% CPU。
func (m *Matcher) Match(ctx *context.Context) bool {
	if m.rt.deleted.Load() || m.rt.disabled.Load() {
		return false
	}

	rules := m.Rules
	// Skip Rules[0] (the OnCommand prefix check) for command-indexed matchers:
	// the commandIndex lookup already confirmed the command matches.
	if m.commandIndexed.Load() && len(rules) > 0 {
		rules = rules[1:]
	}
	for _, rule := range rules {
		if !rule(ctx) {
			return false
		}
	}

	return !m.rt.deleted.Load()
}

// Handle 设置 Matcher 的处理函数（终结点）
//
// 此方法是 Matcher 配置链的**终结点**：调用后不再返回值，
// 从编译期杜绝 `.Handle(h1).Handle(h2)` 这类误用——第二个 Handle
// 会静默丢弃第一个 handler，造成难以察觉的 bug。
//
// 如需在 Handle 后访问 Matcher（如调用 SetTemp），
// 请提前保存 eng.On(...) 返回的 *Matcher：
//
//	m := eng.OnCommand("/ping").SetPriority(100)
//	m.Handle(func(ctx *context.Context) error {
//	    return ctx.Reply("Pong!")
//	})
//	m.SetTemp(30 * time.Second) // 仍可操作 m
//
// # 线程安全说明
//
// Handle 本身是线程安全的（内部使用 m.rt.mu 保护），注册到 Engine 后调用也不会 panic。
// 但强烈建议**在注册（RegisterMatcher/On/OnC2C 等返回前）之前**完成所有链式配置，
// 注册后修改 Handler 会触发中间件链重建并短暂影响并发执行中的请求，属于高代价操作。
//
// 推荐用法：
//
//	eng.OnCommand("/ping").
//	    SetDescription("测试连接").
//	    SetPriority(100).
//	    Use(middleware.Logging()).
//	    Handle(func(ctx *context.Context) error {  // ← 终结点，无返回值
//	        return ctx.Reply("Pong!")
//	    })
func (m *Matcher) Handle(handler context.Handler) {
	if m.isNoop() {
		return
	}
	m.rt.mu.Lock()
	m.Handler = handler
	// Bump version so getOrBuildIterChain rebuilds the compiled chain on next use.
	m.compiledVersion.Add(1)
	coord := m.coordinator
	m.rt.mu.Unlock()
	if coord != nil {
		coord.RebuildMatcherChain(m)
	}
}

// SetPriority 设置 Matcher 的优先级
func (m *Matcher) SetPriority(priority uint64) *Matcher {
	if m.isNoop() {
		return m
	}

	m.rt.mu.Lock()
	changed := m.priority.Load() != priority
	m.priority.Store(priority)
	coord := m.coordinator
	muEvent := m.EventType
	isTempManager := atomic.LoadInt32(&m.rt.isTemp) == 1
	def := m.definition // read definition while holding lock
	m.rt.mu.Unlock()

	isCommandMatcher := def != nil && def.Name != ""

	if changed && coord != nil {
		if isTempManager {
			coord.UpdateTempMatcherPriority(m)
		} else if isCommandMatcher {
			coord.UpdateMatcherIndex(m) // reorder commandIndex for command matchers
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
	m.isBlock.Store(block)
	return m
}

// SetTemp 设置 Matcher 是否为临时匹配器
func (m *Matcher) SetTemp(temp bool) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.rt.deleted.Load() {
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
	return m.rt.deleted.Load()
}

// SetTempWithMaxUse 将 matcher 标记为临时匹配器
func (m *Matcher) SetTempWithMaxUse(maxUse int) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.rt.deleted.Load() {
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
	if m.rt.deleted.Load() {
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

// Use 为当前 matcher 注册局部中间件。
//
// # 线程安全说明
//
// Use 本身是线程安全的（内部使用 m.rt.mu 保护），注册到 Engine 后调用也不会 panic。
// 但强烈建议**在注册之前**完成所有中间件配置：注册后调用 Use 会触发
// 全局中间件链重建，对并发执行中的请求有短暂影响，属于高代价操作。
func (m *Matcher) Use(mw ...context.Middleware) *Matcher {
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
	var nilChain []context.Middleware
	m.combinedChain.Store(nilChain)
	m.compiledHandlers.Store((*compiledChain)(nil))
	// Bump the version counter so the next getOrBuildIterChain call rebuilds
	// the compiled handler chain without needing reflect fingerprinting.
	m.compiledVersion.Add(1)
}

// ensureChain ensures the combined chain is cached and valid.
func (m *Matcher) ensureChain(globalChain []context.Middleware, globalGen uint64, groupChain []context.Middleware, groupGen uint64) {
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
	chain := make([]context.Middleware, 0, len(globalChain)+len(groupChain)+len(locals))
	chain = append(chain, globalChain...)
	chain = append(chain, groupChain...)
	chain = append(chain, locals...)

	m.cachedGen.global = globalGen
	m.cachedGen.group = groupGen
	m.combinedChain.Store(chain)
	// Bump version: the combined chain changed, so the compiled handler chain
	// must also be rebuilt on the next getOrBuildIterChain call.
	m.compiledVersion.Add(1)
}

// getPriority returns priority in a threadsafe way (lock-free atomic load).
func (m *Matcher) getPriority() uint {
	if m == nil {
		return 0
	}
	return uint(m.priority.Load())
}

// isBlocking returns whether matcher should block subsequent handlers (lock-free atomic load).
func (m *Matcher) isBlocking() bool {
	if m == nil {
		return false
	}
	return m.isBlock.Load()
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
	coord := m.coordinator
	m.rt.mu.Unlock()

	// 触发命令缓存更新
	if coord != nil {
		coord.UpdateCommandCache(m)
	}

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
