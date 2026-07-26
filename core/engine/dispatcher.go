package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// ── 错误定义 ──────────────────────────────────────────────────────────────

var (
	// ErrDispatcherClosed 表示 Dispatcher 已关闭，拒绝新任务。
	ErrDispatcherClosed = errors.New("dispatcher: closed")
	// ErrQueueFull 表示目标 Chat 的队列已满。
	ErrQueueFull = errors.New("dispatcher: queue full")
)

// ── 配置 ──────────────────────────────────────────────────────────────────

// DispatcherConfig OutboundDispatcher 的配置参数。
type DispatcherConfig struct {
	// MaxInflight 最大并发 Worker 数（默认 512）。
	MaxInflight int
	// QueueSize 单个 Chat 队列最大长度（默认 64）。
	QueueSize int
	// SendTimeout 单个发送任务的超时时间（默认 30s）。
	SendTimeout time.Duration
	// Recover 是 Worker panic 的回调函数。nil 表示仅记录日志。
	Recover func(any)
	// Hooks 提供了调度生命周期的观察点。
	Hooks DispatcherHooks
}

// DispatcherHooks 提供调度生命周期的观察点。
// 所有字段可选（nil 安全）。
type DispatcherHooks struct {
	// OnQueued 在任务入队时调用，参数为 ChatID 和当前队列长度。
	OnQueued func(chatID string, queueLen int)
	// OnStart 在 Worker 开始执行任务时调用。
	OnStart func(chatID string)
	// OnDone 在 Worker 执行完成后调用。
	OnDone func(info DispatchInfo)
	// OnRejected 在任务被拒绝时调用（队列满或 Dispatcher 已关闭）。
	OnRejected func(chatID string, err error)
}

// DispatchInfo 包含一次发送调用的完整信息。
type DispatchInfo struct {
	ChatID       string
	QueueLen     int
	QueueLatency time.Duration
	RunLatency   time.Duration
	Err          error
}

// ── 环形缓冲区 ────────────────────────────────────────────────────────────

type queuedTask struct {
	enqueueAt time.Time
	run       func(context.Context) error
}

// ringBuffer 固定容量环形缓冲区，用于 Chat 级别的 FIFO 队列。
// 非并发安全，需要外部加锁。
type ringBuffer struct {
	buf  []queuedTask
	head int // 下一个可读位置
	tail int // 下一个可写位置
	len  int
	cap  int
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{buf: make([]queuedTask, cap), cap: cap}
}

func (r *ringBuffer) push(t queuedTask) bool {
	if r.len >= r.cap {
		return false
	}
	r.buf[r.tail] = t
	r.tail = (r.tail + 1) % r.cap
	r.len++
	return true
}

func (r *ringBuffer) pop() (queuedTask, bool) {
	if r.len == 0 {
		return queuedTask{}, false
	}
	t := r.buf[r.head]
	r.head = (r.head + 1) % r.cap
	r.len--
	return t, true
}

// ── Chat 队列 ─────────────────────────────────────────────────────────────

// chatQueue 维护单个 Chat 的出站任务队列和 Worker 状态。
type chatQueue struct {
	mu      sync.Mutex
	q       ringBuffer
	running bool
	chatID  string
}

// ── Dispatcher ────────────────────────────────────────────────────────────

// OutboundDispatcher 是出站任务调度器。
//
// 调度单位是 Message Stream（Chat），同一 Chat 内的任务严格 FIFO，
// 不同 Chat 之间受 MaxInflight 限制并发执行。
//
// 使用方式：
//
//	dispatcher := engine.NewOutboundDispatcher(ctx, DispatcherConfig{
//	    MaxInflight: 512,
//	    QueueSize:   64,
//	})
//	err := dispatcher.Submit("chat_123", func(ctx context.Context) error {
//	    return sender.Send(ctx, req)
//	})
type OutboundDispatcher struct {
	baseCtx context.Context
	cancel  context.CancelFunc
	config  DispatcherConfig
	sem     chan struct{} // 全局并发令牌
	queues  sync.Map      // map[string]*chatQueue
	stopped atomic.Bool
	wg      sync.WaitGroup
}

// NewOutboundDispatcher 创建 OutboundDispatcher。
//
// ctx 的生命周期由调用方管理，通过 ForceClose 或 Shutdown 释放。
// 配置参数会自动补全默认值（参数 <= 0 时）。
func NewOutboundDispatcher(ctx context.Context, config DispatcherConfig) *OutboundDispatcher {
	if config.MaxInflight <= 0 {
		config.MaxInflight = 512
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 64
	}
	if config.SendTimeout <= 0 {
		config.SendTimeout = 30 * time.Second
	}
	bCtx, cancel := context.WithCancel(ctx)
	return &OutboundDispatcher{
		baseCtx: bCtx,
		cancel:  cancel,
		config:  config,
		sem:     make(chan struct{}, config.MaxInflight),
	}
}

// Submit 按 ChatID 提交一个出站任务。
//
// 同一 ChatID 的任务严格 FIFO 顺序执行，不同 ChatID 之间并发执行。
// 当队列满时返回 ErrQueueFull；当 Dispatcher 已关闭时返回 ErrDispatcherClosed。
func (d *OutboundDispatcher) Submit(chatID string, task func(context.Context) error) error {
	if d.stopped.Load() {
		d.reject(chatID, ErrDispatcherClosed)
		return ErrDispatcherClosed
	}

	actual, _ := d.queues.LoadOrStore(chatID, &chatQueue{
		chatID: chatID,
		q:      *newRingBuffer(d.config.QueueSize),
	})
	q := actual.(*chatQueue)

	q.mu.Lock()
	if !q.q.push(queuedTask{enqueueAt: time.Now(), run: task}) {
		q.mu.Unlock()
		d.reject(chatID, ErrQueueFull)
		return ErrQueueFull
	}
	if h := d.config.Hooks.OnQueued; h != nil {
		h(chatID, q.q.len)
	}
	if !q.running {
		q.running = true
		q.mu.Unlock()
		d.wg.Add(1)
		go d.worker(q)
	} else {
		q.mu.Unlock()
	}
	return nil
}

func (d *OutboundDispatcher) reject(chatID string, err error) {
	if h := d.config.Hooks.OnRejected; h != nil {
		h(chatID, err)
	}
}

// worker 是单个 Chat 的消息流处理 goroutine。
//
// Worker 自行竞争全局 semaphore（不阻塞 Submit），空队列时自删除。
func (d *OutboundDispatcher) worker(q *chatQueue) {
	defer d.wg.Done()

	// 竞争全局并发令牌
	select {
	case d.sem <- struct{}{}:
	case <-d.baseCtx.Done():
		q.mu.Lock()
		q.running = false
		q.mu.Unlock()
		return
	}
	defer func() { <-d.sem }()

	// panic 恢复：清理 queue，不处理 Future（Future 由 Submit 方的 recover 保证）
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			err := fmt.Errorf("panic in dispatcher worker [%s]: %v\n%s", q.chatID, r, stack)
			q.mu.Lock()
			q.q = *newRingBuffer(d.config.QueueSize)
			q.running = false
			if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
				d.queues.Delete(q.chatID)
			}
			q.mu.Unlock()
			if fn := d.config.Recover; fn != nil {
				fn(r)
			}
			if h := d.config.Hooks.OnDone; h != nil {
				h(DispatchInfo{ChatID: q.chatID, Err: err})
			}
		}
	}()

	for {
		q.mu.Lock()
		if q.q.len == 0 {
			q.running = false
			if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
				d.queues.Delete(q.chatID)
			}
			q.mu.Unlock()
			return
		}
		t, _ := q.q.pop()
		queueLen := q.q.len
		q.mu.Unlock()

		start := time.Now()
		if h := d.config.Hooks.OnStart; h != nil {
			h(q.chatID)
		}

		sendCtx, cancel := context.WithTimeout(d.baseCtx, d.config.SendTimeout)
		err := t.run(sendCtx)
		cancel()

		if h := d.config.Hooks.OnDone; h != nil {
			h(DispatchInfo{
				ChatID:       q.chatID,
				QueueLen:     queueLen,
				QueueLatency: start.Sub(t.enqueueAt),
				RunLatency:   time.Since(start),
				Err:          err,
			})
		}
	}
}

// ── 生命周期管理 ──────────────────────────────────────────────────────────

// Close 拒绝新任务，已有任务继续执行。
func (d *OutboundDispatcher) Close() {
	d.stopped.Store(true)
}

// Drain 等待所有 Worker 完成已有任务。
//
// 如果 ctx 在完成前被取消，返回 ctx.Err()。
func (d *OutboundDispatcher) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown 拒绝新任务并等待所有 Worker 完成。
//
// 等价于 Close() 后 Drain()。
func (d *OutboundDispatcher) Shutdown(ctx context.Context) error {
	d.Close()
	return d.Drain(ctx)
}

// ForceClose 强制关闭，取消所有未完成的发送。
//
// 调用后所有进行中的发送任务会收到 context.Canceled 错误。
// 通常仅在进程强制退出时使用，生产环境应优先使用 Shutdown。
func (d *OutboundDispatcher) ForceClose() {
	d.stopped.Store(true)
	d.cancel()
}
