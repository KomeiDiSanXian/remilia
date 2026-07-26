package engine

import (
	"time"
)

const (
	// DefaultTempMatcherCleanerInterval 默认临时 Matcher 清理间隔
	DefaultTempMatcherCleanerInterval = 1 * time.Minute
	// DefaultPendingDeleteBufferSize 默认批量删除通道大小
	DefaultPendingDeleteBufferSize = 1000
	// DefaultPendingDeleteProcessInterval 默认批量删除处理间隔
	DefaultPendingDeleteProcessInterval = 100 * time.Millisecond
	// DefaultPendingDeleteBatchSize 默认每次批量删除数量
	DefaultPendingDeleteBatchSize = 1000

	// DefaultExecPoolMaxConcurrency 默认 ExecPool 最大并发 goroutine 数
	DefaultExecPoolMaxConcurrency = 64
	// DefaultExecPoolQueueSize 默认 ExecPool 等待队列大小
	DefaultExecPoolQueueSize = 128
)

// WithCleanupInterval 设置临时 Matcher 清理间隔。
//
// 默认值：DefaultTempMatcherCleanerInterval（1 分钟）。
// 传入 0 可以完全禁用自动清理（不推荐，会导致一次性 Matcher 内存泄漏）。
func WithCleanupInterval(interval time.Duration) Option {
	return func(e *Engine) {
		e.internals.tempMatcherCleanerInterval = interval
	}
}

// WithPendingDeleteProcessInterval 设置批量删除处理器的触发间隔。
//
// 默认值：DefaultPendingDeleteProcessInterval（100ms）。
// 传入 0 可完全禁用批量删除处理器后台 goroutine（适用于单元测试场景，
// 禁用后 Matcher 仍会被标记删除，只是不会被后台批量清理）。
func WithPendingDeleteProcessInterval(interval time.Duration) Option {
	return func(e *Engine) {
		e.internals.pendingDeleteProcessInterval = interval
	}
}

// WithNoBackgroundWorkers 禁用所有后台 goroutine。
//
// 这包括：
//   - 临时 Matcher 清理器（WithCleanupInterval(0) 效果）
//   - 批量删除处理器（WithPendingDeleteProcessInterval(0) 效果）
//
// 适用于单元测试场景，避免测试完成后出现 goroutine 泄漏：
//
//	func TestXxx(t *testing.T) {
//	    e := engine.NewEngine(engine.WithNoBackgroundWorkers())
//	    // 不需要调用 e.Shutdown()，因为没有后台 goroutine
//	    ...
//	}
//
// 注意：生产环境请勿使用此选项，会导致过期 Matcher 无法自动回收。
func WithNoBackgroundWorkers() Option {
	return func(e *Engine) {
		e.internals.tempMatcherCleanerInterval = 0
		e.internals.pendingDeleteProcessInterval = 0
	}
}

// WithExecPoolMaxConcurrency 设置 ExecPool 的最大并发 goroutine 数。
//
// 默认值：DefaultExecPoolMaxConcurrency（64）。
// 此值控制同时执行慢 handler 的最大 goroutine 数量。
// 调高可提升并发吞吐，但会增加 goroutine 调度压力和内存使用。
func WithExecPoolMaxConcurrency(maxConcurrency int) Option {
	return func(e *Engine) {
		e.internals.execPoolCfg.MaxConcurrency = maxConcurrency
	}
}

// WithExecPoolQueueSize 设置 ExecPool 的等待队列大小。
//
// 默认值：DefaultExecPoolQueueSize（128）。
// 队列满时新任务会 fallback 到同步执行，不会阻塞调用方。
func WithExecPoolQueueSize(queueSize int) Option {
	return func(e *Engine) {
		e.internals.execPoolCfg.QueueSize = queueSize
	}
}

// WithPendingDeleteBufferSize 设置批量删除通道的缓冲大小。
//
// 默认值：DefaultPendingDeleteBufferSize（1000）。
// 调高此值可减少高频删除时 DeleteMatcher 的阻塞概率，代价是更多内存占用。
func WithPendingDeleteBufferSize(size int) Option {
	return func(e *Engine) {
		e.internals.pendingDeleteCh = make(chan *Matcher, size)
	}
}

// WithMaxMatchers 设置引擎允许注册的 Matcher 数量上限。
//
// 默认值：0（不限制）。
// 设置为正整数可防止恶意或错误代码无限注册 Matcher 导致内存耗尽。
// 达到上限后，新注册的 Matcher 会返回一个 noop Matcher（链式调用安全，但不实际执行）。
func WithMaxMatchers(max int) Option {
	return func(e *Engine) {
		e.state.Store(e.state.Load().withMaxMatchers(max))
	}
}

// WithPendingDeleteBatchSize 设置每次批量删除 Matcher 的数量。
//
// 默认值：DefaultPendingDeleteBatchSize（1000）。
func WithPendingDeleteBatchSize(size int) Option {
	return func(e *Engine) {
		if size > 0 {
			e.internals.pendingDeleteBatchSize = size
		}
	}
}

// WithExecPoolDisabled 禁用 ExecPool，所有 handler 同步执行。
//
// 适用于单元测试等需要确定性同步行为的场景。
func WithExecPoolDisabled() Option {
	return func(e *Engine) {
		e.internals.execPoolCfg.MaxConcurrency = 0
	}
}

// WithSharedExecPool 设置共享的 ExecPool，适用于多个 Engine 复用线程池的场景。
//
// 共享池的生命周期归调用方所有：
//   - NewEngine 不会再为该 Engine 创建自有池；
//   - Engine.Shutdown 不会 Drain/停止共享池（避免影响其他 Engine），
//     调用方应在所有使用该池的 Engine 关闭后自行调用 pool.Stop()/Drain()。
func WithSharedExecPool(pool *ExecPool) Option {
	return func(e *Engine) {
		e.internals.execPool = pool
		e.internals.execPoolShared = true
	}
}

// WithDispatcherConfig 设置 OutboundDispatcher 的配置参数。
//
// 默认值参见 DispatcherConfig 文档。传入零值字段会被自动填充默认值。
func WithDispatcherConfig(cfg DispatcherConfig) Option {
	return func(e *Engine) {
		e.internals.dispatcherCfg = cfg
	}
}

// GetDispatcher 返回 Engine 的出站调度器，用于在 Handler 外提交发送任务。
func (e *Engine) GetDispatcher() *OutboundDispatcher {
	return e.dispatcher
}
