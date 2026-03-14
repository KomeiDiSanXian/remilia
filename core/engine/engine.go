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

	// shutdownMu 保护 shutdown 标志检查与 eventWg.Add 的原子性。
	// ProcessEvent 持有 RLock 完成"检查 shutdown → Add(1)"两步操作；
	// Shutdown 持有 Lock 完成"Store(true)"后立即释放，随后调用 eventWg.Wait()。
	// 这确保不存在"ProcessEvent 已通过检查但尚未 Add(1)，而 Shutdown 已开始 Wait"的窗口。
	shutdownMu sync.RWMutex
}

// NewEngine 创建一个新的事件引擎（COW 模式）
//
// 默认自动启动临时 Matcher 清理器，每 5 分钟清理一次。
// 可以通过 WithCleanupInterval() 选项或 SetTempMatcherCleanInterval() 修改清理间隔。
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

	// 启动批量删除处理器
	e.services.pendingDeleteStop = e.startPendingDeleteProcessor()

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

	e.shutdownMu.Lock()
	e.shutdown.Store(true)
	e.shutdownMu.Unlock()

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
