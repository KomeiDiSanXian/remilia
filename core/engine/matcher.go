package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
)

type matcherRuntime struct {
	// deleted 和 disabled 使用原子操作，使得 Match() 和 invokeHandler 的提前返回检查
	// 无需持有任何锁即可读取。
	// 写入时仍然需要持有 mu，以保证 useCount + deleted 的复合更新具有原子性。
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
	// priority 和 isBlock 是热路径字段，分别在 mergeSortedMatchersSix 和
	// processEventContext 中每事件读取约 1000 次。使用 atomic 可以避免
	// getPriority/isBlocking 中原有的 RWMutex.RLock/RUnlock 对——
	// 这是大规模测试剖析中两个函数的主要 CPU 开销（分别约 180ms 和 40ms）。
	priority atomic.Uint64
	isBlock  atomic.Bool
	// channelBlocked 按 channel 维度覆盖全局 isBlock 行为。
	// 若某 channel 在此 map 中存在且值为 true，仅该 channel 被阻塞。
	// 用于替代 Per-Channel Engine 的集群隔离——无需 fork Engine 实例。
	channelBlocked sync.Map // map[ChannelKey]bool
	// commandIndexed 在 OnCommand/RegisterCommandDef 创建的 matcher 发布之前设置一次。
	// 为 true 时，Match() 会跳过 Rules[0]（OnCommand 前缀检查），
	// 因为命令已通过 commandIndex 的 O(1) 查找隐式匹配——
	// 在大规模场景中可节省约 200ms / 10% CPU。
	// 使用 atomic.Bool 以防止 rebuildIndex 写操作与
	// 共享 *Matcher 对象（COW 共享指针）的并发 Match() 读操作之间出现数据竞争。
	commandIndexed atomic.Bool
	hasHandler     atomic.Bool
	EventType      EventType
	Rules          []context.Rule
	Handler        context.Handler
	coordinator    MatcherCoordinator
	Source         string
	group          string
	middlewares    []context.Middleware

	combinedChain    infraatomic.Value[[]context.Middleware] // 原始中间件切片
	compiledHandlers infraatomic.Value[*compiledChain]       // 预构建的迭代器处理链切片
	// compiledVersion 是单调递增计数器，在组合中间件链或 Handler 变更时自增。
	// getOrBuildIterChain 通过比较此计数器代替在每次热路径调用时
	// 使用 reflect 计算 FNV 指纹来判断是否需要重建。
	compiledVersion atomic.Uint64

	cachedGen struct {
		global uint64
		group  uint64
	}
	cacheMu sync.RWMutex

	definition *command.Definition // 命令定义（可选，包含所有元数据）

	// triggerPrefix 是命令的触发前缀（如 "/"、"!"、"$#"），
	// 在 OnCommand/RegisterCommandDef 时从 cmdPattern 的开头非字母数字序列提取。
	// 用于 GetCommand()、BindCommand 和别名注册等需要还原命令字符串的场景。
	triggerPrefix string

	// execProfile 跟踪此 matcher 的执行耗时，用于自适应决定走同步还是 ExecPool。
	// nil 表示尚未初始化（首次执行时延迟创建）。
	execProfile *ExecProfile

	// aliasRegistrar 由框架在 RegisterCommand 后注入，在 Handle() 第一次被调用时触发，
	// 为 definition.Aliases 中的每个别名自动注册路由 Matcher。
	// 触发后置 nil 以防止重复注册。
	// 插件无需感知此字段——框架负责设置它。
	aliasRegistrar func(*command.Definition, context.Handler)
}

// compiledChain 缓存 getOrBuildIterChain 生成的最外层组合 handler。
//
// 构建 N 个中间件 + 1 个 handler 后的结构：
//
//	head = chain[0](chain[1](...chain[N-1](handler)...))
//
// 每个 chain[i] 返回一个捕获了 chain[i+1] 结果作为 "next" 的闭包。
// 调用 head() 通过这些闭包捕获执行完整调用链——
// 无需切片迭代，此处只存储 head。
//
// version 反映构建时 m.compiledVersion 的快照；版本不匹配
// 会在下次慢路径调用时触发重建。
type compiledChain struct {
	head    context.Handler // 入口：包裹实际 handler 的最外层中间件
	version uint64          // 编译时 m.compiledVersion 的快照
}

func (m *Matcher) copy() *Matcher {
	newRules := make([]context.Rule, len(m.Rules))
	copy(newRules, m.Rules)

	newMiddlewares := make([]context.Middleware, len(m.middlewares))
	copy(newMiddlewares, m.middlewares)

	newM := &Matcher{
		rt: matcherRuntime{
			isTemp:      atomic.LoadInt32(&m.rt.isTemp),
			useCount:    m.rt.useCount,
			maxUseCount: m.rt.maxUseCount,
			createdAt:   m.rt.createdAt,
			expiresAt:   m.rt.expiresAt,
		},
		EventType:     m.EventType,
		Rules:         newRules,
		Handler:       m.Handler,
		Source:        m.Source,
		group:         m.group,
		middlewares:   newMiddlewares,
		definition:    m.definition,
		triggerPrefix: m.triggerPrefix,
		execProfile:   m.execProfile,
	}
	newM.priority.Store(m.priority.Load())
	newM.isBlock.Store(m.isBlock.Load())
	newM.commandIndexed.Store(m.commandIndexed.Load())
	newM.hasHandler.Store(m.hasHandler.Load())
	return newM
}

// GetCommand 获取匹配器的触发命令（含前缀，如 "/help" 或 "!help"）
func (m *Matcher) GetCommand() string {
	if m == nil {
		return ""
	}
	m.rt.mu.RLock()
	def := m.definition
	prefix := m.triggerPrefix
	m.rt.mu.RUnlock()

	if def != nil && def.Name != "" {
		if prefix == "" {
			prefix = "/"
		}
		return prefix + def.Name
	}
	return ""
}

// GetPrefix 获取匹配器的触发前缀（如 "/" 或 "!"）。
// 若未显式设置，返回 "/"（保持向后兼容）。
func (m *Matcher) GetPrefix() string {
	if m == nil {
		return "/"
	}
	m.rt.mu.RLock()
	prefix := m.triggerPrefix
	m.rt.mu.RUnlock()
	if prefix == "" {
		return "/"
	}
	return prefix
}

// GetSource 获取匹配器的来源标识（实现 context.Matcher）
func (m *Matcher) GetSource() string {
	if m == nil {
		return ""
	}
	m.rt.mu.RLock()
	s := m.Source
	m.rt.mu.RUnlock()
	return s
}

// SetSource 设置匹配器的来源标识
func (m *Matcher) SetSource(source string) *Matcher {
	m.rt.mu.Lock()
	m.Source = source
	coord := m.coordinator
	m.rt.mu.Unlock()
	// Source 变更会影响 CommandInfo 的 Source/Plugin 字段；
	// 触发 commandInfoCache 更新，使 GetAllCommands/FindCommand 反映新来源。
	if coord != nil {
		coord.UpdateCommandCache(m)
	}
	return m
}

// IsTemp 若该 matcher 是由 TempManager 管理的临时 matcher，则返回 true。
func (m *Matcher) IsTemp() bool {
	return atomic.LoadInt32(&m.rt.isTemp) == 1
}

// getCombinedChain 获取组合的中间件链（无锁读取）
func (m *Matcher) getCombinedChain() []context.Middleware {
	return m.combinedChain.Load()
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
	return m == nil || m.Source == "noop"
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
	// 对于通过命令索引匹配的 matcher，跳过 Rules[0]（OnCommand 前缀检查）：
	// commandIndex 查找已确认命令匹配，无需重复检查。
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
	hadHandler := m.Handler != nil
	m.Handler = handler
	m.hasHandler.Store(true)
	// 自增版本号，使 getOrBuildIterChain 在下次使用时重建已编译的处理链。
	m.compiledVersion.Add(1)
	coord := m.coordinator
	m.rt.mu.Unlock()

	if coord != nil {
		coord.RebuildMatcherChain(m)
		// 首次绑定 Handler：触发运行时索引重建，将此前被过滤的匹配器纳入
		if !hadHandler {
			coord.InvalidateSortedCache(m.EventType)
			if def := m.definition; def != nil && def.Name != "" {
				coord.UpdateMatcherIndex(m)
			}
		}
	}

	// 别名自动注册：由框架注入的回调，在 Handle() 首次调用时触发。
	// 取出后立即置 nil，保证只触发一次，即使 Handle() 被多次调用也不会重入。
	m.rt.mu.Lock()
	registrar := m.aliasRegistrar
	def := m.definition
	m.aliasRegistrar = nil
	m.rt.mu.Unlock()
	if registrar != nil && def != nil && len(def.Aliases) > 0 {
		registrar(def, handler)
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
	def := m.definition // 持锁读取 definition
	m.rt.mu.Unlock()

	isCommandMatcher := def != nil && def.Name != ""

	if changed && coord != nil {
		if isTempManager {
			coord.UpdateTempMatcherPriority(m)
		} else if isCommandMatcher {
			coord.UpdateMatcherIndex(m) // 重排命令匹配器的 commandIndex
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

// tempExpirationSetter 由 *Engine 实现：在 TempManager 的 shard 锁内写入
// createdAt/expiresAt，并在 matcher 已由管理器持有时补登过期堆。
//
// 使用带未导出方法的接口做类型断言（仅本包类型可实现），
// 避免扩大 MatcherCoordinator 公共接口、破坏外部 mock。
type tempExpirationSetter interface {
	setTempMatcherExpiration(m *Matcher, createdAt, expiresAt time.Time)
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

	atomic.StoreInt32(&m.rt.isTemp, 1)
	m.rt.mu.Unlock()

	now := time.Now()
	expiresAt := now.Add(timeout)

	if setter, ok := coord.(tempExpirationSetter); ok {
		// 在 TempManager shard 锁内写入过期时间：
		//   - 与清理器/过期堆对这两个字段的读取由同一把锁序列化（消除数据竞争）；
		//   - matcher 已在管理器中时（如 OnTemp 之后调用）同时补登过期堆，
		//     修复"已是 temp 再设超时 → 永不入堆、永不过期"的会话泄漏。
		setter.setTempMatcherExpiration(m, now, expiresAt)
	} else {
		// 无协调器（未注册）或外部 mock：退化为直接写字段
		m.rt.mu.Lock()
		m.rt.createdAt = now
		m.rt.expiresAt = expiresAt
		m.rt.mu.Unlock()
	}

	if needsMigration && coord != nil {
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
	m.combinedChain.Store(nil)
	m.compiledHandlers.Store((*compiledChain)(nil))
	// 自增版本计数器，使下次 getOrBuildIterChain 调用重建
	// 已编译的处理链，无需进行 reflect 指纹计算。
	m.compiledVersion.Add(1)
}

// ensureChain 确保组合中间件链已缓存且有效。
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
	// 自增版本：组合链已变更，因此已编译的处理链
	// 也需要在下次 getOrBuildIterChain 调用时重建。
	m.compiledVersion.Add(1)
}

// getPriority 以线程安全方式返回优先级（无锁原子读取）。
//
// 返回 uint64：避免 32 位平台上 uint 截断 atomic.Uint64 导致排序错乱。
func (m *Matcher) getPriority() uint64 {
	if m == nil {
		return 0
	}
	return m.priority.Load()
}

// isBlocking 以线程安全方式返回 matcher 是否应阻塞后续处理器。
// 优先检查 per-channel 阻塞状态，再回退到全局 isBlock。
func (m *Matcher) isBlocking(key ChannelKey) bool {
	if m == nil {
		return false
	}
	if key != "" {
		if blocked, ok := m.channelBlocked.Load(key); ok && blocked.(bool) {
			return true
		}
	}
	return m.isBlock.Load()
}

// BlockForChannel 设置指定 channel 的阻塞状态，用于替代 Per-Channel Engine 的集群隔离。
// 若想全局阻塞所有 channel，请使用 SetBlock。
func (m *Matcher) BlockForChannel(key ChannelKey, block bool) *Matcher {
	if m.isNoop() {
		return m
	}
	if block {
		m.channelBlocked.Store(key, true)
	} else {
		m.channelBlocked.Delete(key)
	}
	return m
}

// BindCommand 手动绑定触发命令
//
// 此方法会自动创建或更新 Definition.Name。
// cmd 应包含前缀（如 "/help"、"!!admin"），或仅命令名（如 "help"）。
// 开头的连续非字母数字字符会被识别为前缀并自动剥离。
// 如需自定义前缀，请使用 RegisterCommandWithPrefix。
func (m *Matcher) BindCommand(cmd string) *Matcher {
	if m.isNoop() {
		return m
	}

	m.rt.mu.Lock()
	trimmed := strings.TrimSpace(cmd)
	if trimmed != "" {
		prefix, cmdName := context.SplitCommandPattern(trimmed)
		m.triggerPrefix = prefix
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
		// 同步命令元数据缓存，使 GetAllCommands/FindCommand 立即可见新绑定的命令
		coord.UpdateCommandCache(m)
	}
	return m
}

// SetGroup 设置 matcher 的分组名称。
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

// GetGroup 返回 matcher 的分组名称。
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

// SetAliasRegistrar 设置别名自动注册回调。
// 由框架（liveRegistryWriter）在 RegisterCommand 后注入，插件代码无需调用此方法。
// 回调在 Handle() 第一次被调用且 definition.Aliases 非空时触发，触发后置 nil 防止重入。
func (m *Matcher) SetAliasRegistrar(fn func(*command.Definition, context.Handler)) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	m.aliasRegistrar = fn
	m.rt.mu.Unlock()
	return m
}

// GetDefinition 获取命令定义
func (m *Matcher) GetDefinition() *command.Definition {
	m.rt.mu.RLock()
	defer m.rt.mu.RUnlock()
	return m.definition
}

// mutateDefinition 在锁内应用 definition 变更，随后同步命令元数据缓存。
//
// commandInfoCache 中的 CommandInfo 是注册瞬间对 definition 的字段拷贝；
// 若注册后再修改 definition（OnCommand(...).SetDescription(...) 是文档推荐链式写法），
// 必须触发 UpdateCommandCache，否则 GetAllCommands/FindCommand//help
// 将一直返回注册时的陈旧元数据（直到某次无关的全量索引重建）。
func (m *Matcher) mutateDefinition(mutate func(def *command.Definition)) *Matcher {
	if m.isNoop() {
		return m
	}
	m.rt.mu.Lock()
	if m.definition == nil {
		m.definition = &command.Definition{}
	}
	mutate(m.definition)
	coord := m.coordinator
	m.rt.mu.Unlock()

	// UpdateCommandCache 内部对 GetCommand()=="" 的 matcher 直接返回，
	// 因此对未绑定命令名的 matcher 调用是安全的空操作。
	if coord != nil {
		coord.UpdateCommandCache(m)
	}
	return m
}

// SetDescription 设置命令描述（便捷方法）
func (m *Matcher) SetDescription(desc string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Description = desc
	})
}

// SetUsage 设置命令用法（便捷方法）
func (m *Matcher) SetUsage(usage string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Usage = usage
	})
}

// SetCategory 设置命令分类（便捷方法）
func (m *Matcher) SetCategory(category string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Category = category
	})
}

// SetAliases 设置命令别名（便捷方法）
func (m *Matcher) SetAliases(aliases ...string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Aliases = aliases
	})
}

// SetExamples 设置命令示例（便捷方法）
func (m *Matcher) SetExamples(examples ...string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Examples = examples
	})
}

// SetHidden 设置是否在帮助中隐藏（便捷方法）
func (m *Matcher) SetHidden(hidden bool) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Hidden = hidden
	})
}

// SetPermissions 设置所需权限（便捷方法）
func (m *Matcher) SetPermissions(permissions ...string) *Matcher {
	return m.mutateDefinition(func(def *command.Definition) {
		def.Permissions = permissions
	})
}
