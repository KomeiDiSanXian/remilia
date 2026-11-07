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
	// 清掉槽位里的引用：队列任务闭包通常捕获 Handler/上下文，
	// 不清会让环形缓冲（容量 = QueueSize）在空闲期继续持有它们。
	r.buf[r.head] = queuedTask{}
	r.head = (r.head + 1) % r.cap
	r.len--
	return t, true
}

// takeAll 取出并清空剩余任务（保持 FIFO 顺序）。
//
// 用于队列被放弃时（worker panic 恢复、停机上下文结束）让每个任务都能
// 得到明确结果，见 [OutboundDispatcher.failDroppedTasks]。
func (r *ringBuffer) takeAll() []queuedTask {
	if r.len == 0 {
		return nil
	}
	out := make([]queuedTask, 0, r.len)
	for {
		t, ok := r.pop()
		if !ok {
			break
		}
		out = append(out, t)
	}
	return out
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

	// gate 把「入队 + 计数」与「关闭」串行化：
	// Submit 在读锁内复查 stopped 并登记在途任务，Close 先置 stopped
	// 再取写锁（等于等待所有在途 Submit 走完），因此 Drain 不会在
	// 一个刚通过 stopped 检查、尚未入队的任务之前就返回。
	gate sync.RWMutex

	// wg 统计在途 worker/失败补偿 goroutine。
	// 用 drainTracker 而不是 sync.WaitGroup：Submit 与 Drain 天然并发，
	// WaitGroup 在归零后 Add 会触发 "reused before previous Wait has returned"，
	// 且 Drain 可能在上一次 Wait 返回前被再次调用。
	wg drainTracker
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
	// 读锁覆盖「复查 stopped + 入队 + 登记在途」全过程：
	// Close 会先置 stopped 再取写锁，因此 Close→Drain 之后不可能再有
	// 通过检查的任务被登记（否则 Drain 可能先返回、任务随后才执行）。
	d.gate.RLock()
	if d.stopped.Load() {
		d.gate.RUnlock()
		d.reject(chatID, ErrDispatcherClosed)
		return ErrDispatcherClosed
	}

	for {
		// 先 Load 再按需 LoadOrStore，避免热路径上每次都分配新的 chatQueue
		var q *chatQueue
		if v, ok := d.queues.Load(chatID); ok {
			q = v.(*chatQueue)
		} else {
			v, _ := d.queues.LoadOrStore(chatID, &chatQueue{
				chatID: chatID,
				q:      *newRingBuffer(d.config.QueueSize),
			})
			q = v.(*chatQueue)
		}

		q.mu.Lock()

		// 复查映射：worker 在 q.mu 下将空队列从 map 中移除；
		// 若我们拿到的 q 已被移除，向其推送会导致同一 chat 出现两个并存的
		// 队列与 worker（破坏 FIFO 保证）。此时重试整个流程。
		// worker 的移除同样发生在 q.mu 临界区内，因此本检查无竞争窗口。
		if cur, ok := d.queues.Load(chatID); !ok || cur.(*chatQueue) != q {
			q.mu.Unlock()
			continue
		}

		if !q.q.push(queuedTask{enqueueAt: time.Now(), run: task}) {
			q.mu.Unlock()
			d.gate.RUnlock()
			d.reject(chatID, ErrQueueFull)
			return ErrQueueFull
		}
		queueLen := q.q.len
		startWorker := !q.running
		if startWorker {
			q.running = true
			// 起 worker 前先登记在途计数：worker 可能在登记完成前就跑完
			// （空队列立即返回），先起 goroutine 再 Add 会让 Drain 看到瞬时 0。
			// 必须在 gate 读锁内登记，见函数开头说明。
			d.wg.Add(1)
		}
		q.mu.Unlock()
		d.gate.RUnlock()

		if startWorker {
			go d.worker(q)
		}
		// 钩子在锁外调用：慢钩子（写日志/打点）不应阻塞同一 chat 的其它
		// 入队者，也不应拖住 Close 的写锁屏障。
		if h := d.config.Hooks.OnQueued; h != nil {
			h(chatID, queueLen)
		}
		return nil
	}
}

func (d *OutboundDispatcher) reject(chatID string, err error) {
	if h := d.config.Hooks.OnRejected; h != nil {
		h(chatID, err)
	}
}

// failDroppedTasks 让被放弃的队列任务得到明确结果。
//
// 队列会在两种情况下被放弃：worker 恢复 panic 后重建队列、worker 因停机
// 上下文结束而退出。此前这些任务（以及它们各自的 Future）被静默丢弃，
// 调用方可能正卡在 Wait 上永久等待。
//
// 处理方式是用已取消的 context 运行任务：发送层立即失败并把错误写入
// Future，消息不会真的发出。在独立 goroutine 中执行，避免调用点
// （worker 的恢复/退出路径）阻塞。
func (d *OutboundDispatcher) failDroppedTasks(chatID string, tasks []queuedTask) {
	if len(tasks) == 0 {
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithCancel(d.baseCtx)
		cancel() // 立即取消：任务只应快速失败，不应真正发送
		for _, t := range tasks {
			if t.run == nil {
				continue
			}
			err := t.run(ctx)
			if h := d.config.Hooks.OnDone; h != nil {
				h(DispatchInfo{ChatID: chatID, Err: err})
			}
		}
	}()
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
		// 停机：本 worker 不再执行队列任务，但要保证队列里的任务
		// 各自得到结果（此前直接返回，任务与 Future 一起被静默丢弃）
		q.mu.Lock()
		dropped := q.q.takeAll()
		q.running = false
		if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
			d.queues.Delete(q.chatID)
		}
		q.mu.Unlock()
		d.failDroppedTasks(q.chatID, dropped)
		return
	}
	defer func() { <-d.sem }()

	// panic 恢复：清理 queue，不处理 Future（Future 由 Submit 方的 recover 保证）
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			err := fmt.Errorf("panic in dispatcher worker [%s]: %v\n%s", q.chatID, r, stack)
			q.mu.Lock()
			dropped := q.q.takeAll()
			q.q = *newRingBuffer(d.config.QueueSize)
			q.running = false
			if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
				d.queues.Delete(q.chatID)
			}
			q.mu.Unlock()
			// 队列中的其它任务必须拿到结果，而不是随队列一起消失
			d.failDroppedTasks(q.chatID, dropped)
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
//
// 置 stopped 后取写锁（= 等待所有在途 Submit 完成入队与登记），
// 使随后的 Drain 一定能看到这些任务。
func (d *OutboundDispatcher) Close() {
	d.stopped.Store(true)
	d.gate.Lock()
	//lint:ignore SA2001 intentional drain barrier — 确保在途 Submit 已全部登记
	d.gate.Unlock() //nolint:staticcheck // 见上：空临界区是刻意的关闭栅栏
}

// Drain 等待所有 Worker 完成已有任务。
//
// 如果 ctx 在完成前被取消，返回 ctx.Err()。
func (d *OutboundDispatcher) Drain(ctx context.Context) error {
	return d.wg.Wait(ctx)
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
	d.gate.Lock()
	//lint:ignore SA2001 intentional drain barrier — 确保在途 Submit 已全部登记
	d.gate.Unlock() //nolint:staticcheck // 见上：空临界区是刻意的关闭栅栏
	d.cancel()
}
