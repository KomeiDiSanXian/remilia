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
//
// 关闭语义：shutdown atomic.Bool + eventWg sync.WaitGroup 替代了原 eventGate
// sentinel 编码设计，性能提升约 3 倍且语义更直观。
type Engine struct {
	// 不可变状态（COW 模式）- 使用类型安全的泛型包装器
	state      *infraatomic.Value[*state]           // 引擎核心状态
	middleware *infraatomic.Value[*middlewareState] // 中间件配置

	// 写锁（仅用于修改操作）
	writeMu sync.Mutex

	// internals 集中管理非核心基础设施：（临时 Matcher 管理器、对象池、
	// Metrics 收集器、后台 goroutine 生命周期管理等）。
	internals engineInternals

	// shutdown 标志位，Shutdown() 时设置，ProcessEvent 热路径上通过 Load 检查
	shutdown atomic.Bool

	// eventWg 追踪活跃的 ProcessEvent 调用，Shutdown() 等待归零
	eventWg sync.WaitGroup
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

	// defaults for internals
	e.internals.tempMatcherCleanerInterval = DefaultTempMatcherCleanerInterval
	e.internals.tempManager = newTempMatcherManager()
	e.internals.matcherPool = infrapool.New(func() []*Matcher { return make([]*Matcher, 0, DefaultMatcherPoolCapacity) })
	e.internals.pendingDeleteProcessInterval = DefaultPendingDeleteProcessInterval
	e.internals.pendingDeleteBatchSize = DefaultPendingDeleteBatchSize
	e.internals.metricsCollector = infraatomic.NewValue[*inframetrics.Collector](nil)

	// 初始化不可变状态
	e.state = infraatomic.NewValue(newEngineState())
	e.middleware = infraatomic.NewValue(newMiddlewareState())

	// 应用用户自定义的选项
	for _, opt := range options {
		opt(e)
	}

	// 如果未通过选项配置，则使用默认的 pendingDeleteCh
	if e.internals.pendingDeleteCh == nil {
		e.internals.pendingDeleteCh = make(chan *Matcher, DefaultPendingDeleteBufferSize)
	}

	// 自动启动临时 Matcher 清理器（如果间隔 > 0）
	if e.internals.tempMatcherCleanerInterval > 0 {
		e.internals.tempMatcherCleanerStop = e.StartTempMatcherCleaner(e.internals.tempMatcherCleanerInterval)
	} else {
		logger.Info("[engine] Temp matcher cleaner disabled by default configuration")
	}

	// 启动批量删除处理器（如果间隔 > 0）
	if e.internals.pendingDeleteProcessInterval > 0 {
		e.internals.pendingDeleteStop = e.startPendingDeleteProcessor()
	} else {
		logger.Info("[engine] Pending delete processor disabled by configuration")
	}

	// Register runtime components for unified shutdown semantics.
	e.internals.register(&tempCleanerComponent{e: e})
	e.internals.register(&pendingDeleteComponent{e: e})

	return e
}

// Shutdown 优雅停止 Engine 后台工作者并等待在途事件处理完成。
//
// 合约：
//   - 停止所有 Engine 持有的后台 goroutine（临时 Matcher 清理器、批量删除处理器等）
//   - 等待所有活跃的 ProcessEvent 调用完成
//   - 若 ctx 在等待完成前被取消，返回 ctx.Err()
//
// 关闭顺序：
//  1. 设置 shutdown 标志（阻止新事件进入）
//  2. 停止后台 goroutine（清理器、删除处理器）
//  3. 等待所有活跃的 ProcessEvent 调用完成
func (e *Engine) Shutdown(ctx stdctx.Context) error {
	// 设置 shutdown 标志，阻止新事件进入 ProcessEvent
	e.shutdown.Store(true)

	// 停止所有后台 goroutine
	e.internals.stopAll()
	if err := e.internals.waitAll(ctx); err != nil {
		return err
	}

	// 等待所有活跃的 ProcessEvent 调用完成
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

// ---- 内部辅助 ---------------------------------------------------------------

// removeMatcherFromStateSilently 从 State 移除 matcher，但不标记为已删除。
// 用于迁移到 TempManager 时（MigrateMatcherToTemp）。
func (e *Engine) removeMatcherFromStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.state.Store(e.state.Load().withDeletedMatcher(m))
}

// addMatcherToStateSilently 将 matcher 重新加入 State。
// 用于从 TempManager 迁移回来时（MigrateMatcherFromTemp）。
func (e *Engine) addMatcherToStateSilently(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.state.Store(e.state.Load().withAddedMatcher(m))
}
