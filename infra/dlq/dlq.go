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
	MaxSize     int
	Workers     int
	DropPolicy  DropPolicy
	OnDropped   func(item DeadLetterItem, reason string)
	OnProcessed func(item DeadLetterItem, duration time.Duration)
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

	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	enqueueMu   sync.Mutex
	queueClosed atomic.Bool
	closeOnce   sync.Once
}

func NewDeadLetterQueue(config DeadLetterQueueConfig) *DeadLetterQueue {
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())

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
}

func (dlq *DeadLetterQueue) worker(id int) {
	defer dlq.wg.Done()

	for {
		select {
		case item, ok := <-dlq.queue:
			if !ok {
				return
			}
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
		case <-dlq.ctx.Done():
			// If Stop() closed the queue, we'll naturally exit via ok==false above.
			// Otherwise, honor cancellation.
			return
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
		// Close the queue so workers can drain remaining items and exit.
		close(dlq.queue)
		// Cancel context only for external force-cancel users; it should not be required
		// for normal shutdown.
		dlq.cancel()
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
	// 使用较短的超时时间，避免在关闭时长时间阻塞
	ctx, cancel := context.WithTimeout(dlq.ctx, 5*time.Second)
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

	select {
	case dlq.queue <- item:
		return
	case <-dlq.ctx.Done():
		// DLQ 正在关闭，立即返回
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "queue closing")
		}
		return
	case <-ctx.Done():
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "timeout waiting for space")
		}
		return
	}
}
