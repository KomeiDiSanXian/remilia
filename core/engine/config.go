package engine

import (
	"time"

	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
)

const (
	// DefaultTempMatcherCleanerInterval 默认临时 Matcher 清理间隔
	DefaultTempMatcherCleanerInterval = 1 * time.Minute
	// DefaultPendingDeleteBufferSize 默认批量删除通道大小
	DefaultPendingDeleteBufferSize = 1000
	// DefaultMatcherPoolCapacity 默认 Matcher 池初始容量
	DefaultMatcherPoolCapacity = 16
	// MaxMatcherPoolRetainCapacity Matcher 池回收的最大容量，防止无限增长
	MaxMatcherPoolRetainCapacity = 1024
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

// WithMatcherPoolCapacity 设置 Matcher 池的初始容量。
//
// 默认值：DefaultMatcherPoolCapacity（16）。
// 调大此值可降低高并发场景下对象池的扩容频率，代价是初始内存占用略有增加。
func WithMatcherPoolCapacity(capacity int) Option {
	return func(e *Engine) {
		if capacity > 0 {
			e.internals.matcherPool = infrapool.New(func() []*Matcher {
				return make([]*Matcher, 0, capacity)
			})
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
