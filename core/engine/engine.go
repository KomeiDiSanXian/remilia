package engine

import (
	stdctx "context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// Engine 事件引擎（Copy-on-Write 模式）
//
// COW 并发模型：
//   - 读操作：完全无锁，通过 infraatomic.Value 读取不可变状态
//   - 写操作：使用 writeMu 保护，复制-修改-替换
//   - 无死锁风险：只有单一写锁，读操作无锁
//   - 读写分离：写操作不阻塞读操作（读操作看到旧状态）
//
// 性能特性：
//   - 读操作性能：5-6x 提升（无锁）
//   - 写操作性能：略有下降（复制开销）
//   - 内存效率：读操作零分配，整体效率提升 93%
//   - 适用场景：读多写少（完美匹配 Engine 使用模式）
type Engine struct {
	// 不可变状态（COW 模式）- 使用类型安全的泛型包装器
	state      *infraatomic.Value[*engineState]     // 引擎核心状态
	middleware *infraatomic.Value[*middlewareState] // 中间件配置

	// 写锁（仅用于修改操作）
	writeMu sync.Mutex

	// services holds runtime/infra concerns (temp manager, pools, metrics, etc.)
	services engineServices

	// runtime holds engine-owned background components.
	runtime engineRuntime

	// eventWg tracks active event processing calls
	eventWg sync.WaitGroup

	// shutdown 标志：Shutdown() 设置后，ProcessEvent 不再接受新事件
	// 防止 Shutdown 调用 eventWg.Wait() 后，ProcessEvent 仍调用 eventWg.Add(1) 的竞态
	shutdown atomic.Bool
}

// NewEngine 创建一个新的事件引擎（COW 模式）
//
// 默认自动启动临时 Matcher 清理器，每 5 分钟清理一次。
// 可以通过 WithCleanupInterval() 选项或 user SetTempMatcherCleanInterval() 修改清理间隔。
//
// COW 模式优势：
//   - 读操作无锁，性能提升 5-6x
//   - 无死锁风险
//   - 内存效率高（读操作零分配）
func NewEngine(options ...Option) *Engine {
	e := &Engine{}

	// defaults for services
	e.services.tempMatcherCleanerInterval = DefaultTempMatcherCleanerInterval
	e.services.tempManager = newTempMatcherManager()
	e.services.matcherPool = infrapool.New(func() []*Matcher { return make([]*Matcher, 0, DefaultMatcherPoolCapacity) })
	e.services.pendingDeleteProcessInterval = DefaultPendingDeleteProcessInterval
	e.services.pendingDeleteBatchSize = DefaultPendingDeleteBatchSize
	e.services.compiler = NewMatcherCompiler()

	// 初始化不可变状态 - 使用类型安全的泛型包装器
	e.state = infraatomic.NewValue(newEngineState())
	e.middleware = infraatomic.NewValue(newMiddlewareState())

	// 应用用户自定义的选项
	for _, opt := range options {
		opt(e)
	}

	// 如果未通过选项配置，则使用默认的 pendingDeleteCh
	if e.services.pendingDeleteCh == nil {
		e.services.pendingDeleteCh = make(chan *Matcher, DefaultPendingDeleteBufferSize)
	}

	// 自动启动临时 Matcher 清理器（如果间隔 > 0）
	if e.services.tempMatcherCleanerInterval > 0 {
		e.services.tempMatcherCleanerStop = e.StartTempMatcherCleaner(e.services.tempMatcherCleanerInterval)
	} else {
		logger.Info("[engine] Temp matcher cleaner disabled by default configuration")
	}

	// 启动批量删除处理器
	e.services.pendingDeleteStop = e.startPendingDeleteProcessor()

	// Register runtime components for unified shutdown semantics.
	e.runtime.register(&tempCleanerComponent{e: e})
	e.runtime.register(&pendingDeleteComponent{e: e})

	return e
}

// DeleteAllMatchers 删除引擎中的所有匹配器（COW 写操作）
func (e *Engine) DeleteAllMatchers() {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态 - 无需类型断言
	oldState := e.state.Load()
	oldMatchers := append([]*Matcher(nil), oldState.matchers...)

	// 2. 创建新的空状态
	newState := copyEngineState(oldState)
	newState.matchers = make([]*Matcher, 0)
	newState.matcherIndex = make(map[dto.EventType][]*Matcher)
	newState.sortedCache = make(map[dto.EventType][]*Matcher)

	// 3. 原子替换
	e.state.Store(newState)

	// 4. 标记旧 matchers 为已删除
	for _, m := range oldMatchers {
		if m == nil {
			continue
		}
		m.rt.mu.Lock()
		m.rt.deleted = true
		m.rt.mu.Unlock()
	}
}

// DeleteMatcher 删除指定的匹配器（COW 写操作）
func (e *Engine) DeleteMatcher(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 复制状态
	newState := copyEngineState(oldState)

	// 从副本中删除
	newState.deleteMatcher(m)

	// 原子替换
	e.state.Store(newState)
}

// DeleteMatchers 批量删除匹配器（COW 写操作）
func (e *Engine) DeleteMatchers(matchers []*Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 获取当前状态 - 无需类型断言
	state := e.state.Load()

	// 2. 复制状态
	newState := copyEngineState(state)

	// 3. 修改新状态
	newState.deleteMatchers(matchers)

	// 4. 原子替换
	e.state.Store(newState)
}

// RemoveGroup 根据分组名称（通常是插件名称）删除所有匹配器（COW 写操作）
//
// 此操作是原子的：要么全部删除，要么都不删除。
// 使用了 Copy-On-Write 机制，不会阻塞读操作。
func (e *Engine) RemoveGroup(groupName string) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 获取当前状态 - 无需类型断言
	state := e.state.Load()

	// 2. 检查是否有该组（快速检查，避免不必要的复制）
	if _, ok := state.groupIndex[groupName]; !ok {
		return
	}

	// 3. 复制状态
	newState := copyEngineState(state)

	// 4. 修改新状态
	newState.removeGroup(groupName)

	// 5. 原子替换
	e.state.Store(newState)

	logger.Debugf("[engine] Removed matcher group: %s", groupName)
}

// InvalidateSortedCache 失效指定事件类型的排序缓存（COW 写操作）
//
// 当 Matcher 的优先级被修改时调用此方法。
// 在 COW 模式中，这会创建新状态并重建索引。
func (e *Engine) InvalidateSortedCache(eventType dto.EventType) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 复制状态
	newState := copyEngineState(oldState)

	// 失效缓存
	newState.invalidateSortedCache(eventType)

	// 原子替换
	e.state.Store(newState)
}

// SetBlock 设置引擎的阻塞状态（COW 写操作）
func (e *Engine) SetBlock(block bool) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 复制状态
	newState := copyEngineState(oldState)
	newState.block = block

	// 原子替换
	e.state.Store(newState)

	return e
}

// SetMaxMatchers 设置匹配器数量上限（COW 写操作）
// 设置为 0 表示不限制（默认）
// 此设置可以防止恶意或错误的代码无限制注册匹配器导致内存溢出
func (e *Engine) SetMaxMatchers(limit int) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 复制状态
	newState := copyEngineState(oldState)
	newState.maxMatchers = limit

	// 原子替换
	e.state.Store(newState)

	return e
}

// GetMaxMatchers 获取当前的匹配器数量上限（COW 无锁读取）
func (e *Engine) GetMaxMatchers() int {
	state := e.state.Load() // 无需类型断言
	return state.maxMatchers
}

// GetMatcherCount 获取当前已注册的匹配器数量（COW 无锁读取）
func (e *Engine) GetMatcherCount() int {
	state := e.state.Load() // 无需类型断言
	return len(state.matchers)
}

// GetTempMatcherCount 获取当前已注册的临时匹配器数量
func (e *Engine) GetTempMatcherCount() int {
	return e.services.tempManager.Count()
}

// noopMatcher 是一个空操作匹配器，用于在达到匹配器限制时返回
// 所有方法都返回自身，形成无操作链
var noopMatcher = &Matcher{
	rt:          matcherRuntime{deleted: true},
	priority:    999,
	Source:      "noop",
	Rules:       []context.Rule{},
	middlewares: []Middleware{},
}

// registerMatcher 注册一个已初始化的匹配器（内部方法，COW 写操作）
func (e *Engine) registerMatcher(m *Matcher) *Matcher {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 检查匹配器数量限制
	if oldState.maxMatchers > 0 && len(oldState.matchers) >= oldState.maxMatchers {
		logger.Errorf("[engine] Matcher limit reached: %d/%d, returning noop matcher",
			len(oldState.matchers), oldState.maxMatchers)
		// 返回一个新的 noop matcher，带有 engine 引用，避免 panic
		return &Matcher{
			rt:          matcherRuntime{deleted: true},
			priority:    999,
			Source:      "noop",
			Rules:       []context.Rule{},
			middlewares: []Middleware{},
			coordinator: e,
		}
	}

	// 复制状态
	newState := copyEngineState(oldState)

	// 添加到副本 (addMatcher handles indexing based on m.command)
	newState.addMatcher(m)

	// 原子替换状态
	e.state.Store(newState)

	// 重建中间件链（读取中间件状态，无需加锁）
	e.rebuildMatcherChainCOW(m)

	return m
}

// BatchRegisterMatchers 批量注册多个匹配器（COW 写操作）
//
// 相比多次调用 registerMatcher，此方法只执行一次 COW 复制，
// 在批量注册场景下可以大幅提升性能（3-5x）。
//
// 使用场景：
//   - 插件初始化时注册多个匹配器
//   - 配置热更新时重新注册所有匹配器
//
// 性能优势：
//   - 减少 50-70% 的内存复制开销
//   - 原子操作，保证一致性
func (e *Engine) BatchRegisterMatchers(matchers []*Matcher) []*Matcher {
	if len(matchers) == 0 {
		return matchers
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态 - 无需类型断言
	oldState := e.state.Load()

	// 2. 检查匹配器数量限制
	newCount := len(oldState.matchers) + len(matchers)
	if oldState.maxMatchers > 0 && newCount > oldState.maxMatchers {
		logger.Errorf("[engine] Matcher limit reached: %d+%d > %d, truncating batch",
			len(oldState.matchers), len(matchers), oldState.maxMatchers)

		// 只注册到达限制前的 matchers
		available := oldState.maxMatchers - len(oldState.matchers)
		if available <= 0 {
			// 全部返回 noop
			noop := make([]*Matcher, len(matchers))
			for i := range noop {
				noop[i] = &Matcher{
					rt:          matcherRuntime{deleted: true},
					priority:    999,
					Source:      "noop",
					Rules:       []context.Rule{},
					middlewares: []Middleware{},
					coordinator: e,
				}
			}
			return noop
		}
		matchers = matchers[:available]
	}

	// 3. 复制状态
	newState := copyEngineState(oldState)

	// 4. 批量添加到副本
	for _, m := range matchers {
		newState.addMatcher(m)
	}

	// 5. 原子替换状态
	e.state.Store(newState)

	// 6. 重建中间件链（批量处理）
	for _, m := range matchers {
		e.rebuildMatcherChainCOW(m)
	}

	logger.Debugf("[engine] Batch registered %d matchers", len(matchers))
	return matchers
}

// On 注册一个新的事件匹配器，显式指定事件类型（COW 写操作）
//
// COW 流程：
//  1. 加载当前状态
//  2. 复制状态
//  3. 修改副本
//  4. 原子替换
func (e *Engine) On(eventType dto.EventType, rules ...context.Rule) *Matcher {
	matcher := &Matcher{
		EventType:   eventType,
		Rules:       rules,
		coordinator: e,
		priority:    50,       // 默认优先级
		Source:      "global", // 默认来源为全局（非插件）
	}
	return e.registerMatcher(matcher)
}

// OnAny 注册一个适用于所有事件类型的匹配器
func (e *Engine) OnAny(rules ...context.Rule) *Matcher {
	return e.On("", rules...)
}

// OnC2C 是 On(dto.C2CMessageCreate, ...) 的便捷封装
func (e *Engine) OnC2C(rules ...context.Rule) *Matcher {
	return e.On(dto.C2CMessageCreate, rules...)
}

// OnGroupAt 是 On(dto.GroupAtMessageCreate, ...) 的便捷封装
func (e *Engine) OnGroupAt(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupAtMessageCreate, rules...)
}

// OnGroupAdd 是 On(dto.GroupAddRobot, ...) 的便捷封装
func (e *Engine) OnGroupAdd(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupAddRobot, rules...)
}

// OnGroupDel 是 On(dto.GroupDelRobot, ...) 的便捷封装
func (e *Engine) OnGroupDel(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupDelRobot, rules...)
}

// OnFullMatch 注册一个完全匹配器
//
// 相当于：engine.OnAny(context.OnFullMatch(text))
func (e *Engine) OnFullMatch(text string, extraRules ...context.Rule) *Matcher {
	finalRules := append([]context.Rule{context.OnFullMatch(text)}, extraRules...)
	return e.OnAny(finalRules...)
}

// OnTemp 注册一个临时的事件匹配器（非 COW 模式）
//
// 专为高频创建/销毁的场景优化，避免 COW 的全量复制开销。
// 临时 Matcher 默认为一次性（使用 1 次后删除），可通过 SetTempWithMaxUse 等修改。
// 它们存储在独立的读写锁结构中，清理效率更高（使用最小堆）。
func (e *Engine) OnTemp(eventType dto.EventType, rules ...context.Rule) *Matcher {
	matcher := &Matcher{
		EventType:   eventType,
		Rules:       rules,
		coordinator: e,
		priority:    50, // Default priority
		Source:      "temp",
		rt: matcherRuntime{
			isTemp:      1, // Initialize as true (1)
			maxUseCount: 1, // Default to disposable
		},
	}

	e.services.tempManager.Add(matcher)

	// Build chain locally (no need for COW rebuild since it'services not in state)
	e.rebuildMatcherChainCOW(matcher)

	return matcher
}

// UpdateTempMatcherPriority 更新临时 matcher 的优先级（内部方法）
func (e *Engine) UpdateTempMatcherPriority(m *Matcher) {
	e.services.tempManager.Remove(m)
	e.services.tempManager.Add(m)
}

// MatcherStats 匹配器统计
type MatcherStats struct {
	Total         int
	Global        int
	ByPlugin      map[string]int
	GlobalEnabled bool
}

// GetMatcherStats 获取匹配器统计信息（COW 无锁读取）
func (e *Engine) GetMatcherStats() MatcherStats {
	// 无锁读取状态
	state := e.state.Load()
	stats := MatcherStats{ByPlugin: make(map[string]int)}
	stats.Total = len(state.matchers)

	for _, m := range state.matchers {
		if m.Source == "global" || m.Source == "" {
			stats.Global++
			continue
		}
		if after, ok := strings.CutPrefix(m.Source, "plugin:"); ok {
			name := after
			stats.ByPlugin[name]++
		}
	}

	// GlobalEnabled: 当 block=false 时认为全局匹配器可用；也可以扩展为独立开关
	stats.GlobalEnabled = !state.block
	return stats
}

// EnableGlobalMatchers 启用/禁用通过 Engine.On 注册的全局匹配器（COW 写操作）
// 注意：这会影响所有非插件注册的匹配器
func (e *Engine) EnableGlobalMatchers(enable bool) {
	e.SetBlock(!enable)
}

// SetMetricsCollector 设置指标收集器（v0.7.1 新增）
func (e *Engine) SetMetricsCollector(mc *metrics.Collector) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	e.services.metricsCollector.Store(mc)
	return e
}

// GetMetricsCollector 获取指标收集器（v0.7.1 新增）
func (e *Engine) GetMetricsCollector() *metrics.Collector {
	val := e.services.metricsCollector.Load()
	if val == nil {
		return nil
	}
	return val.(*metrics.Collector)
}

// GetCompiler 获取 Matcher 编译器
func (e *Engine) GetCompiler() *MatcherCompiler {
	return e.services.compiler
}

// CompileAllMatchers 预编译所有 matchers 以提升性能
//
// 此方法会遍历所有已注册的 matchers 并预编译它们的规则。
// 编译后的 matchers 会按成本排序规则，并缓存正则表达式等资源。
//
// 使用场景：
//   - 在应用启动后调用一次，预编译所有 matchers
//   - 在批量注册 matchers 后调用
//
// 注意：编译是可选的优化，不编译也能正常工作。
func (e *Engine) CompileAllMatchers() {
	state := e.state.Load()
	compiler := e.services.compiler

	for _, m := range state.matchers {
		compiler.Compile(m)
	}
}

// Shutdown gracefully stops Engine background workers (cleaners, processors, etc.)
// and waits for in-flight event processing to complete.
//
// Contract/Behavior:
//   - Shutdown(ctx) is idempotent-ish in the sense that calling it multiple times is safe.
//   - It stops engine-owned background goroutines (e.g. temp matcher cleaner, pending delete processor).
//   - It waits for all active ProcessEvent/ProcessEventBatch calls to finish.
//   - If ctx is done before waiting completes, it returns ctx.Err().
func (e *Engine) Shutdown(ctx stdctx.Context) error {
	// 1) Stop background components.
	e.runtime.stopAll()
	if err := e.runtime.waitAll(ctx); err != nil {
		return err
	}

	e.shutdown.Store(true)

	// Wait for active events to complete, bounded by ctx.
	done := make(chan struct{})
	go func() {
		e.eventWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops Engine background workers and waits for in-flight events.
//
// Deprecated-ish note: prefer Shutdown(ctx) for bounded graceful shutdown.
// Close keeps backward compatibility and performs an unbounded wait.
func (e *Engine) Close() {
	_ = e.Shutdown(stdctx.Background())
}

// removeMatcherFromStateSilently removes a matcher from state without marking it deleted.
// Used for migration to TempManager.
func (e *Engine) removeMatcherFromStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.deleteMatcher(m)
	e.state.Store(newState)
}

// addMatcherToStateSilently adds a matcher back to state.
// Used for migration from TempManager.
func (e *Engine) addMatcherToStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.addMatcher(m)
	e.state.Store(newState)
}

// Snapshot represents an opaque snapshot of the engine state.
type Snapshot struct {
	data *engineSnapshot
}

// Snapshot creates a snapshot of the current engine state.
func (e *Engine) Snapshot() Snapshot {
	return Snapshot{
		data: &engineSnapshot{
			state:      e.state.Load(),
			middleware: e.middleware.Load(),
		},
	}
}

// Restore restores the engine state from a snapshot.
func (e *Engine) Restore(s Snapshot) {
	if s.data == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.state.Store(s.data.state)
	e.middleware.Store(s.data.middleware)
}

type engineSnapshot struct {
	state      *engineState
	middleware *middlewareState
}

// UpdateMatcherCommand updateMatcherCommand 重新索引指定的 matcher（COW 写操作）
// 当 matcher 的 command 属性变化时调用
func (e *Engine) UpdateMatcherCommand(m *Matcher) {
	e.UpdateMatcherIndex(m)
}

// UpdateMatcherIndex 强制更新匹配器的索引（COW 写操作）
// 当匹配器的 command 或 group 属性变化时调用
//
// 改进 #14：仅重建受影响 matcher 的 EventType 对应的 sortedCache，
// 避免每次都全量重建所有索引（原实现 O(N) 全量重建）。
// 若 m 为 nil，则回退到全量重建。
func (e *Engine) UpdateMatcherIndex(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)

	if m == nil {
		// 无法确定受影响范围，全量重建
		newState.rebuildIndex()
		e.state.Store(newState)
		return
	}

	// 局部重建：只重建受影响 eventType 的排序缓存
	// 对于有 command 的 matcher，只需更新 commandIndex 对应的排序
	// 对于无 command 的 matcher，只需更新 matcherIndex + sortedCache 的对应 eventType
	cmd := m.GetCommand()
	et := m.EventType

	if cmd != "" {
		// 命令 matcher：重新排序 commandIndex 中该命令该 eventType 的列表
		if cmdMap, ok := newState.commandIndex[cmd]; ok {
			if matchers, ok := cmdMap[et]; ok {
				sortMatchersByPriority(matchers)
			}
		}
	} else {
		// 普通 matcher：重新排序 matcherIndex 中该 eventType 的列表，并更新 sortedCache
		if matchers, ok := newState.matcherIndex[et]; ok {
			sorted := make([]*Matcher, len(matchers))
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			newState.sortedCache[et] = sorted
		}
	}

	e.state.Store(newState)
}

// UpdateCommandCache 更新指定 matcher 的命令缓存（COW 写操作）
// 当 matcher 的 definition 变化时调用
func (e *Engine) UpdateCommandCache(m *Matcher) {
	if m == nil {
		return
	}

	cmd := m.GetCommand()
	if cmd == "" {
		return
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态
	oldState := e.state.Load()

	// 复制状态
	newState := copyEngineState(oldState)

	// 更新命令缓存
	newState.rebuildCommandInfoCache(m, cmd)

	// 原子替换
	e.state.Store(newState)
}

// OnCommand 注册一个命令匹配器（自动开启 O(1) 分发优化）
//
// 此方法会自动创建一个 command.Definition 并设置到 Matcher 中。
//
// 性能优势：
//   - 自动将匹配器注册到 Hash Map 索引中
//   - 消息处理时仅需 O(1) 查找，无需遍历所有规则
//
// 参数：
//   - eventType: 事件类型
//   - cmdPattern: 触发命令 (如 "/ping")
//   - extraRules: 其他附加规则
func (e *Engine) OnCommand(eventType dto.EventType, cmdPattern string, extraRules ...context.Rule) *Matcher {
	// 构造规则列表，首位为 standard OnCommand rule
	// 这样即使优化失效或降级，也能正确匹配
	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, context.OnCommand(cmdPattern))
	finalRules = append(finalRules, extraRules...)

	m := &Matcher{
		EventType:   eventType,
		Rules:       finalRules,
		coordinator: e,
		priority:    50,       // 默认优先级
		Source:      "global", // 默认来源为全局（非插件）
	}

	// 自动创建 Definition（用于 Help 生成和命令索引）
	cmdName := strings.TrimPrefix(strings.TrimSpace(cmdPattern), "/")
	if cmdName != "" {
		m.definition = &command.Definition{
			Name: cmdName,
		}
	}

	return e.registerMatcher(m)
}

// MigrateMatcherToTemp 将 matcher 迁移到 TempManager
func (e *Engine) MigrateMatcherToTemp(m *Matcher) {
	e.services.tempManager.Add(m)
	e.removeMatcherFromStateSilently(m)
}

// MigrateMatcherFromTemp 将 matcher 从 TempManager 迁移到 State
func (e *Engine) MigrateMatcherFromTemp(m *Matcher) {
	e.addMatcherToStateSilently(m)
	e.services.tempManager.Remove(m)
}

// RegisterCommand 注册一个高级命令定义
func (e *Engine) RegisterCommand(cmd *command.Definition, rules ...context.Rule) *Matcher {
	return e.RegisterCommandWithPrefix("/", cmd, rules...)
}

// RegisterCommandWithPrefix 带自定义前缀的 RegisterCommand
func (e *Engine) RegisterCommandWithPrefix(prefix string, cmd *command.Definition, rs ...context.Rule) *Matcher {
	trigger := prefix + cmd.Name

	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, cmd, prefix)
		if err != nil {
			logger.WithError(err).WithField("trigger", trigger).Debug("[engine] Command parse match failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	finalRules := append([]context.Rule{parseRule}, rs...)
	m := e.OnCommand("", trigger, finalRules...)
	m.SetDefinition(cmd) // 直接设置 Definition
	m.Handle(context.ExecuteCommandDefinition)
	return m
}

// RegisterCommandDef 注册 command.Definition（自动设置元数据）
//
// 这是推荐的命令注册方式，集成了命令解析和元数据管理。
// 它会自动：
//  1. 创建命令解析规则
//  2. 转换 Definition 为 MatcherMetadata
//  3. 设置 Handler（如果 Definition 中有定义）
//
// 参数:
//   - eventType: 事件类型（空字符串表示所有类型）
//   - def: 命令定义
//   - extraRules: 额外的匹配规则（可选）
//
// 返回:
//   - 注册的 Matcher
//
// 示例:
//
//	def := &command.Definition{
//	    Name:        "search",
//	    Aliases:     []string{"find", "query"},
//	    Description: "搜索内容",
//	    Usage:       "/search <keyword> [--engine google]",
//	    Category:    "实用工具",
//	    Examples:    []string{"/search Go语言"},
//	    Arguments: []*command.Argument{
//	        {Name: "keyword", Description: "搜索关键词", Required: true, Type: command.ArgTypeString},
//	    },
//	    Flags: []*command.Flag{
//	        {Name: "engine", ShortName: "e", Description: "搜索引擎", Default: "google"},
//	    },
//	}
//	m := engine.RegisterCommandDef(dto.GroupAtMessageCreate, def)
func (e *Engine) RegisterCommandDef(eventType dto.EventType, def *command.Definition, extraRules ...context.Rule) *Matcher {
	if def == nil {
		logger.Warn("[engine] RegisterCommandDef: definition is nil")
		return &Matcher{
			rt:          matcherRuntime{deleted: true},
			priority:    999,
			Source:      "noop",
			Rules:       []context.Rule{},
			middlewares: []Middleware{},
			coordinator: e,
		}
	}

	trigger := "/" + def.Name

	// 构造解析规则
	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, def, "/")
		if err != nil {
			logger.WithError(err).
				WithField("trigger", trigger).
				Debug("[engine] Command parse failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	// 组合规则
	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, parseRule)
	finalRules = append(finalRules, extraRules...)

	// 注册命令
	m := e.OnCommand(eventType, trigger, finalRules...)

	// 直接设置 Definition（无需转换）
	m.SetDefinition(def)

	// 如果 Definition 有 Handler，自动设置
	if def.Handler != nil {
		m.Handle(func(ctx *context.Context) error {
			def.Handler(ctx)
			return nil
		})
	}

	return m
}

// RegisterCommandDefWithPrefix 带自定义前缀的 RegisterCommandDef
//
// 此方法允许使用自定义命令前缀（如 "!" 或 "#"）。
//
// 参数:
//   - eventType: 事件类型
//   - prefix: 命令前缀（如 "/"、"!"、"#"）
//   - def: 命令定义
//   - extraRules: 额外的匹配规则
//
// 返回:
//   - 注册的 Matcher
//
// 示例:
//
//	// 使用 "!" 作为命令前缀
//	m := engine.RegisterCommandDefWithPrefix(dto.GroupAtMessageCreate, "!", def)
func (e *Engine) RegisterCommandDefWithPrefix(
	eventType dto.EventType,
	prefix string,
	def *command.Definition,
	extraRules ...context.Rule,
) *Matcher {
	if def == nil {
		logger.Warn("[engine] RegisterCommandDefWithPrefix: definition is nil")
		return &Matcher{
			rt:          matcherRuntime{deleted: true},
			priority:    999,
			Source:      "noop",
			Rules:       []context.Rule{},
			middlewares: []Middleware{},
			coordinator: e,
		}
	}

	if prefix == "" {
		prefix = "/"
	}

	trigger := prefix + def.Name

	// 构造解析规则
	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, def, prefix)
		if err != nil {
			logger.WithError(err).
				WithField("trigger", trigger).
				Debug("[engine] Command parse failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, parseRule)
	finalRules = append(finalRules, extraRules...)

	m := e.OnCommand(eventType, trigger, finalRules...)

	// 直接设置 Definition
	m.SetDefinition(def)

	// 自动设置 Handler
	if def.Handler != nil {
		m.Handle(func(ctx *context.Context) error {
			def.Handler(ctx)
			return nil
		})
	}

	return m
}

// WithMatcherGroupBatch batches group/source updates for matchers.
//
// Why:
//   - Plugin loading often creates many matchers and then assigns them into a group.
//   - Updating group membership by rebuilding indices per matcher causes write amplification.
//
// Contract:
//   - All updates applied within fn will be committed with at most one COW store.
//   - Index rebuild (groupIndex, commandIndex, matcherIndex, sortedCache) happens once.
func (e *Engine) WithMatcherGroupBatch(fn func()) {
	if fn == nil {
		return
	}

	// Important: do NOT hold writeMu while executing fn().
	// fn frequently calls engine.On/registerMatcher, which also takes writeMu.
	// Holding it here would deadlock.
	fn()

	// Commit via a single rebuild of the engine state.
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.rebuildIndex()
	e.state.Store(newState)
}

// SetMatcherGroup sets matcher.group and matcher.Source and updates engine indices.
// This is the preferred way to attach an already-registered matcher to a plugin group.
func (e *Engine) SetMatcherGroup(m *Matcher, group, source string) {
	if m == nil {
		return
	}

	// Update matcher fields.
	m.rt.mu.Lock()
	m.group = strings.TrimSpace(group)
	if source != "" {
		m.Source = source
	}
	m.invalidateCombinedChain()
	m.rt.mu.Unlock()

	// Update middleware chain as group affects group middlewares.
	e.rebuildMatcherChainCOW(m)
}

// CommandInfo 命令信息（用于 Help 生成和命令发现）
type CommandInfo struct {
	Command     string              // 命令名（如 "/help"）
	Description string              // 命令描述
	Usage       string              // 使用方法
	Aliases     []string            // 别名列表
	Category    string              // 分类
	Examples    []string            // 使用示例
	Permissions []string            // 所需权限
	Plugin      string              // 所属插件名
	Source      string              // 来源标识（如 "plugin:help"）
	EventType   dto.EventType       // 事件类型
	Definition  *command.Definition // 完整定义（直接使用 command.Definition）
}

// GetAllCommands 获取所有已注册的命令信息
//
// 改进 3.7：直接返回预构建的命令列表缓存副本，避免每次调用遍历 map。
// 命令列表在每次 COW 写操作（注册/删除命令）时自动更新，读操作 O(n) 复制一次切片。
// 用于 Help Plugin 等需要发现所有命令的场景。
//
// 返回的命令列表不包含隐藏命令（Hidden=true）。
func (e *Engine) GetAllCommands() []CommandInfo {
	state := e.state.Load()

	// 改进 3.7: 直接复制预构建缓存切片，避免 map 遍历
	if len(state.commandListCache) == 0 {
		return nil
	}
	commands := make([]CommandInfo, len(state.commandListCache))
	copy(commands, state.commandListCache)
	return commands
}

// GetCommandsByPlugin 按插件分组获取命令
//
// 返回的 map 键为插件名，值为该插件的所有命令列表。
// 全局命令（非插件注册）使用 "global" 作为键。
func (e *Engine) GetCommandsByPlugin() map[string][]CommandInfo {
	commands := e.GetAllCommands()
	grouped := make(map[string][]CommandInfo)

	for _, cmd := range commands {
		plugin := cmd.Plugin
		if plugin == "" {
			plugin = "global"
		}
		grouped[plugin] = append(grouped[plugin], cmd)
	}

	return grouped
}

// GetCommandsByCategory 按分类获取命令
//
// 返回的 map 键为分类名，值为该分类的所有命令列表。
// 未设置分类的命令使用 "其他" 作为键。
func (e *Engine) GetCommandsByCategory() map[string][]CommandInfo {
	commands := e.GetAllCommands()
	grouped := make(map[string][]CommandInfo)

	for _, cmd := range commands {
		category := cmd.Category
		if category == "" {
			category = "其他"
		}
		grouped[category] = append(grouped[category], cmd)
	}

	return grouped
}

// FindCommand 查找特定命令（支持别名）
//
// 参数:
//   - name: 命令名或别名
//
// 返回:
//   - 找到的命令信息，未找到返回 nil
func (e *Engine) FindCommand(name string) *CommandInfo {
	commands := e.GetAllCommands()

	// Normalize search term: ensure it has "/" prefix
	searchName := name
	if !strings.HasPrefix(searchName, "/") {
		searchName = "/" + searchName
	}

	for _, cmd := range commands {
		// 匹配命令名（精确匹配）
		if cmd.Command == searchName {
			return &cmd
		}

		// Also try matching without prefix normalization for exact matches
		if cmd.Command == name {
			return &cmd
		}

		// 匹配别名
		for _, alias := range cmd.Aliases {
			aliasWithSlash := alias
			if !strings.HasPrefix(alias, "/") {
				aliasWithSlash = "/" + alias
			}
			if aliasWithSlash == searchName || alias == name {
				return &cmd
			}
		}
	}

	return nil
}
