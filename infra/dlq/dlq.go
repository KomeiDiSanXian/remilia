package dlq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Error definitions
var (
	ErrQueueClosed       = errors.New("queue is closed")
	ErrQueueFull         = errors.New("queue is full")
	ErrInvalidDropPolicy = errors.New("invalid drop policy")
	ErrCloseTimeout      = errors.New("close operation timed out")
)

type DropPolicy int

const (
	DropPolicyOldest DropPolicy = iota
	DropPolicyNewest
	DropPolicyBlockUntilSpace
)

type DeadLetterQueueConfig struct {
	MaxSize    int
	Workers    int
	DropPolicy DropPolicy
	// BlockTimeout 控制 DropPolicyBlockUntilSpace 策略下等待空间的最大时间。
	// 默认值 0 表示使用内部默认值（5s）。改进 B9: 从硬编码改为可配置。
	BlockTimeout time.Duration
	OnDropped    func(item DeadLetterItem, reason string)
	OnProcessed  func(item DeadLetterItem, duration time.Duration)
}

type Stats struct {
	QueueSize  int
	MaxSize    int
	Processed  int64
	Dropped    int64
	Workers    int
	IsClosed   bool
	Consumers  int
	DropPolicy DropPolicy
}

type DeadLetterQueue struct {
	config       DeadLetterQueueConfig
	queue        chan DeadLetterItem
	consumers    []DeadLetterConsumer
	consumerSnap atomic.Value

	dropped   atomic.Int64
	processed atomic.Int64

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.RWMutex
	enqueueMu sync.Mutex
	// sendMu 保护 enqueueBlockUntilSpace 中 channel send 与 Shutdown 中 close(queue) 的并发安全。
	// 发送方持有 RLock，Shutdown 在 cancel() 之后、close(queue) 之前获取 Lock，
	// 确保 close 时不存在正在执行 send 的 goroutine。
	sendMu      sync.RWMutex
	queueClosed atomic.Bool
	closeOnce   sync.Once
	// startOnce 保证 Start() 幂等，防止多次调用启动重复 worker
	startOnce sync.Once
}

func NewDeadLetterQueue(config DeadLetterQueueConfig) *DeadLetterQueue {
	return NewDeadLetterQueueWithContext(context.Background(), config)
}

// NewDeadLetterQueueWithContext 创建带父 context 的 DeadLetterQueue。
//
// 当父 context 取消时（例如 Bot.Stop() 触发 rootCtx 取消），DLQ 会停止
// 接受新的入队请求，并等待已入队的消息处理完毕。
// 推荐在 Bot 场景下使用此构造函数，将 DLQ 生命周期与 Bot 绑定：
//
//	dlq := dlq.NewDeadLetterQueueWithContext(bot.Context(), cfg)
func NewDeadLetterQueueWithContext(parent context.Context, config DeadLetterQueueConfig) *DeadLetterQueue {
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}

	ctx, cancel := context.WithCancel(parent)

	dlq := &DeadLetterQueue{
		config:    config,
		queue:     make(chan DeadLetterItem, config.MaxSize),
		consumers: make([]DeadLetterConsumer, 0),
		ctx:       ctx,
		cancel:    cancel,
	}
	dlq.consumerSnap.Store([]DeadLetterConsumer{})
	return dlq
}

func (dlq *DeadLetterQueue) AddConsumer(consumer DeadLetterConsumer) {
	dlq.mu.Lock()
	dlq.consumers = append(dlq.consumers, consumer)
	snapshot := append([]DeadLetterConsumer(nil), dlq.consumers...)
	dlq.consumerSnap.Store(snapshot)
	dlq.mu.Unlock()
	logger.WithField("consumer_count", len(snapshot)).Info("[DeadLetterQueue] Consumer added")
}

func (dlq *DeadLetterQueue) Start() {
	dlq.startOnce.Do(func() {
		consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)
		if len(consumers) == 0 {
			logger.Warn("[DeadLetterQueue] No consumers registered, dead letters will be queued but not processed")
		}
		for i := 0; i < dlq.config.Workers; i++ {
			dlq.wg.Add(1)
			go dlq.worker(i)
		}
		logger.WithFields(logger.Fields{
			"workers":   dlq.config.Workers,
			"max_size":  dlq.config.MaxSize,
			"consumers": len(consumers),
		}).Info("[DeadLetterQueue] Started")
	})
}

func (dlq *DeadLetterQueue) worker(id int) {
	defer dlq.wg.Done()

	// range 在 close(queue) 后自动退出，不会遗漏 channel 中的任何消息。
	// 注意：cancel() 仅用于打断 enqueueBlockUntilSpace 中的阻塞生产者，
	// 消费侧不应响应 ctx.Done()，否则会在 close(queue) 之前静默丢弃已入队消息。
	for item := range dlq.queue {
		consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)
		start := time.Now()
		for _, consumer := range consumers {
			func(c DeadLetterConsumer, it DeadLetterItem) {
				defer func() {
					if r := recover(); r != nil {
						logger.WithField("worker_id", id).WithField("panic", r).Error("[DeadLetterQueue] Consumer panic recovered")
					}
				}()
				c.Consume(it)
			}(consumer, item)
		}
		duration := time.Since(start)
		dlq.processed.Add(1)
		if dlq.config.OnProcessed != nil {
			dlq.config.OnProcessed(item, duration)
		}
	}
}

func (dlq *DeadLetterQueue) Enqueue(item DeadLetterItem) {
	// 检查队列是否已关闭
	if dlq.queueClosed.Load() {
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "queue closed")
		}
		return
	}

	// DropPolicyBlockUntilSpace 需要阻塞等待，使用独立方法精确控制 recover 范围
	if dlq.config.DropPolicy == DropPolicyBlockUntilSpace {
		dlq.enqueueBlockUntilSpace(item)
		return
	}

	// 其他策略需要加锁保护状态一致性
	dlq.enqueueMu.Lock()
	defer dlq.enqueueMu.Unlock()

	select {
	case dlq.queue <- item:
		return
	default:
		// 队列满，按策略处理
		switch dlq.config.DropPolicy {
		case DropPolicyOldest:
			select {
			case old := <-dlq.queue:
				dlq.dropped.Add(1)
				if dlq.config.OnDropped != nil {
					dlq.config.OnDropped(old, "queue full, dropping oldest")
				}
				dlq.queue <- item
				return
			default:
				dlq.dropped.Add(1)
				if dlq.config.OnDropped != nil {
					dlq.config.OnDropped(item, "queue full, cannot drop oldest")
				}
				return
			}
		case DropPolicyNewest:
			dlq.dropped.Add(1)
			if dlq.config.OnDropped != nil {
				dlq.config.OnDropped(item, "queue full, dropping newest")
			}
			return
		}
	}
}

func (dlq *DeadLetterQueue) Shutdown(ctx context.Context) error {
	dlq.closeOnce.Do(func() {
		dlq.queueClosed.Store(true)
		// 先取消 dlq.ctx，使 enqueueBlockUntilSpace 中的 <-dlq.ctx.Done() 分支
		// 能够先于 close(queue) 退出，消除 close/send 并发竞态。
		dlq.cancel()
		// 持有 sendMu 写锁，等待所有正在执行 enqueueBlockUntilSpace 的 goroutine 退出。
		// sendMu.Lock 会阻塞直到所有 RLock 持有者（正在 select send 的 goroutine）完成后
		// 才获取，从而确保在 close(queue) 时没有其他 goroutine 正在向 channel 发送数据。
		dlq.sendMu.Lock()
		close(dlq.queue)
		dlq.sendMu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		dlq.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		logger.Warn("[DeadLetterQueue] Stop timeout, some dead letters may not be processed")
		return ctx.Err()
	}
}

func (dlq *DeadLetterQueue) Stats() Stats {
	consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)
	return Stats{
		QueueSize:  len(dlq.queue),
		MaxSize:    dlq.config.MaxSize,
		Processed:  dlq.processed.Load(),
		Dropped:    dlq.dropped.Load(),
		Workers:    dlq.config.Workers,
		IsClosed:   dlq.queueClosed.Load(),
		Consumers:  len(consumers),
		DropPolicy: dlq.config.DropPolicy,
	}
}

// enqueueBlockUntilSpace 处理 DropPolicyBlockUntilSpace 策略的入队
// 独立方法确保 defer recover 只作用于此函数范围，不会误捕获其他分支的 panic
func (dlq *DeadLetterQueue) enqueueBlockUntilSpace(item DeadLetterItem) {
	// 改进 B9: 使用可配置的 BlockTimeout，默认 5s
	blockTimeout := dlq.config.BlockTimeout
	if blockTimeout <= 0 {
		blockTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(dlq.ctx, blockTimeout)
	defer cancel()

	// recover 精确作用于本函数，防止 send on closed channel panic
	defer func() {
		if r := recover(); r != nil {
			// Channel 已关闭，记录为 dropped
			dlq.dropped.Add(1)
			if dlq.config.OnDropped != nil {
				dlq.config.OnDropped(item, "queue closed during send")
			}
		}
	}()

	// 持有 sendMu 读锁以防止与 Shutdown 中 close(queue) 的并发竞态。
	// Shutdown 持有写锁时，所有持有读锁的发送方都已退出 select，channel 已无活跃 send，
	// 可以安全关闭。
	dlq.sendMu.RLock()
	defer dlq.sendMu.RUnlock()

	select {
	case dlq.queue <- item:
		return
	case <-dlq.ctx.Done(): // DLQ 正在关闭（dlq.cancel() 被调用），立即返回
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "queue closing")
		}
		return
	case <-ctx.Done():
		// 等待空间超时
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "timeout waiting for space")
		}
		return
	}
}
