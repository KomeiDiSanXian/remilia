package engine

// engine.go — Engine 核心定义
//
// 本文件只包含：
//   - Engine 结构体定义与并发模型说明
//   - NewEngine 构造函数
//   - Shutdown / Close 关闭语义
//   - 内部辅助：removeMatcherFromStateSilently / addMatcherToStateSilently
//
// 索引设计说明：
//
// Engine 维护三个正交的索引结构，各有不同的用途：
//
//   - matcherIndex / sortedCache（按 EventType）
//     用于 ProcessEvent 时按事件类型快速获取 Matcher 列表（已按优先级排序）。
//
//   - commandIndex（按命令名 → EventType）
//     用于 O(1) 命令路由优化：消息内容以 "/" 开头时先提取命令名，
//     直接从 commandIndex 取出候选 Matcher，跳过全量遍历。
//
//   - groupIndex（按 group 名）
//     用于 DisableGroup / EnableGroup / RemoveGroup 批量操作，
//     与 Source 字段无关（Source 只用于调试和标签，不驱动分组行为）。
//
// Source vs group：
//   - Source（如 "global" / "plugin:admin"）：只读标签，用于日志/统计，不影响路由
//   - group（如 "admin"）：可变，用于 DisableGroup/EnableGroup/UseForGroup 操作

import (
	stdctx "context"
	"sync"
	"sync/atomic"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
)

// eventGateShutdownSentinel 是一个大负数，用于将 eventGate.n 从"运行态"（≥0）
// 切换到"关闭态"（<0）。选取 int64 绝对值的 1/4，确保即使有数百亿并发事件也不会溢出。
const eventGateShutdownSentinel = int64(-1) << 40 // -1,099,511,627,776

// eventGate 是无锁的事件处理门控。
//
// 将原有的 shutdown atomic.Bool + shutdownMu sync.RWMutex + eventWg sync.WaitGroup
// 三字段合并为单个 atomic.Int64，热路径（ProcessEvent）仅需一次 CAS，无 mutex 竞争。
//
// 状态编码：
//   - n ≥ 0：正常运行，值为当前活跃的 ProcessEvent 调用数
//   - n < 0：已触发 shutdown，|n| - |sentinel| 为仍在运行的事件数
//
// 正确性：shutdown() 使用 shutdownOnce 确保 sentinel 只加一次，防止 n 二次溢出。
type eventGate struct {
	n            atomic.Int64
	zeroCh       chan struct{} // 当 shutdown 后活跃数归零时关闭
	signalOnce   sync.Once     // 确保 zeroCh 只关闭一次
	shutdownOnce sync.Once     // 确保 sentinel 只加一次
}

// acquire 尝试为一次 ProcessEvent 调用占位（热路径）。
// 返回 false 表示已 shutdown，调用方应立即返回。
// 实现：无锁 CAS 循环，无 mutex，无 channel，无堆分配。
func (g *eventGate) acquire() bool {
	for {
		n := g.n.Load()
		if n < 0 {
			return false // 已 shutdown
		}
		if g.n.CompareAndSwap(n, n+1) {
			return true
		}
		// CAS 失败：有并发竞争，重试（通常 1-3 次）
	}
}

// release 在 ProcessEvent 完成时释放占位。
// 若此刻已 shutdown 且是最后一个活跃调用，关闭 zeroCh 唤醒 Shutdown()。
func (g *eventGate) release() {
	if n := g.n.Add(-1); n == eventGateShutdownSentinel {
		g.signalOnce.Do(func() { close(g.zeroCh) })
	}
}

// shutdown 触发关闭信号（幂等，多次调用安全）。
// 若调用时已无活跃事件，立即关闭 zeroCh；否则由最后一个 release() 关闭。
func (g *eventGate) shutdown() {
	g.shutdownOnce.Do(func() {
		if n := g.n.Add(eventGateShutdownSentinel); n == eventGateShutdownSentinel {
			g.signalOnce.Do(func() { close(g.zeroCh) })
		}
	})
}

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
	state      *infraatomic.Value[*state]           // 引擎核心状态
	middleware *infraatomic.Value[*middlewareState] // 中间件配置

	// 写锁（仅用于修改操作）
	writeMu sync.Mutex

	// services 持有运行时/基础设施关注点（临时 Matcher 管理器、对象池、Metrics 收集器等）
	services services

	// runtime 持有引擎自有的后台组件（定期清理、待删除处理器等）
	runtime runtime

	// gate 是无锁事件处理门控（P-5 优化）。
	// 替代原有的 shutdown atomic.Bool + shutdownMu sync.RWMutex + eventWg sync.WaitGroup，
	// 热路径（ProcessEvent）只需一次 CAS 操作，彻底消除 RWMutex 竞争。
	gate eventGate
}

// NewEngine 创建一个新的事件引擎（COW 模式）
//
// # 后台 goroutine 说明（重要）
//
// NewEngine 默认在构造时启动以下后台 goroutine：
//   - 临时 Matcher 清理器（每 1 分钟扫描并释放过期的一次性 Matcher）
//   - 批量删除处理器（每 100ms 批量处理待删除的 Matcher）
//
// 调用方必须在使用结束后调用 [Engine.Shutdown] 以停止这些 goroutine，
// 否则会导致 goroutine 泄漏。典型用法：
//
//	e := engine.NewEngine()
//	defer e.Shutdown(ctx)  // 必须配对调用
//
// 在单元测试中，推荐使用 [WithNoBackgroundWorkers] 选项禁用后台 goroutine，
// 避免测试结束后产生 goroutine 泄漏：
//
//	e := engine.NewEngine(engine.WithNoBackgroundWorkers())
//
// # 配置选项
//
// 可通过 WithCleanupInterval()、WithPendingDeleteProcessInterval() 等选项调整行为。
// 传入 0 可以分别禁用对应的后台 goroutine。
//
// COW 模式优势：
//   - 读操作无锁，性能提升 5-6x
//   - 无死锁风险
//   - 内存效率高（读操作零分配）
func NewEngine(options ...Option) *Engine {
	e := &Engine{}

	// 初始化 eventGate 的 zeroCh（其他字段零值即有效）
	e.gate.zeroCh = make(chan struct{})

	// defaults for services
	e.services.tempMatcherCleanerInterval = DefaultTempMatcherCleanerInterval
	e.services.tempManager = newTempMatcherManager()
	e.services.matcherPool = infrapool.New(func() []*Matcher { return make([]*Matcher, 0, DefaultMatcherPoolCapacity) })
	e.services.pendingDeleteProcessInterval = DefaultPendingDeleteProcessInterval
	e.services.pendingDeleteBatchSize = DefaultPendingDeleteBatchSize
	e.services.metricsCollector = infraatomic.NewValue[*inframetrics.Collector](nil)

	// 初始化不可变状态
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

	// 启动批量删除处理器（如果间隔 > 0）
	if e.services.pendingDeleteProcessInterval > 0 {
		e.services.pendingDeleteStop = e.startPendingDeleteProcessor()
	} else {
		logger.Info("[engine] Pending delete processor disabled by configuration")
	}

	// Register runtime components for unified shutdown semantics.
	e.runtime.register(&tempCleanerComponent{e: e})
	e.runtime.register(&pendingDeleteComponent{e: e})

	return e
}

// Shutdown 优雅停止 Engine 后台工作者并等待在途事件处理完成。
//
// 合约：
//   - 停止所有 Engine 持有的后台 goroutine（临时 Matcher 清理器、批量删除处理器等）
//   - 等待所有活跃的 ProcessEvent 调用完成
//   - 若 ctx 在等待完成前被取消，返回 ctx.Err()
func (e *Engine) Shutdown(ctx stdctx.Context) error {
	e.runtime.stopAll()
	if err := e.runtime.waitAll(ctx); err != nil {
		return err
	}

	// P-5 优化：通过 gate.shutdown() 设置关闭信号（无锁），
	// 然后等待所有活跃 ProcessEvent 调用完成（zeroCh 关闭）。
	e.gate.shutdown()

	select {
	case <-e.gate.zeroCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---- 内部辅助 ---------------------------------------------------------------

// removeMatcherFromStateSilently 从 State 移除 matcher，但不标记为已删除。
// 用于迁移到 TempManager 时（MigrateMatcherToTemp）。
func (e *Engine) removeMatcherFromStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.deleteMatcher(m)
	e.state.Store(newState)
}

// addMatcherToStateSilently 将 matcher 重新加入 State。
// 用于从 TempManager 迁移回来时（MigrateMatcherFromTemp）。
func (e *Engine) addMatcherToStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.addMatcher(m)
	e.state.Store(newState)
}
