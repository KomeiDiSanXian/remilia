package remilia

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// DeadLetterQueue 死信队列管理器
//
// 提供带容量限制的死信队列，防止内存堆积。
// 当队列满时，根据配置的丢弃策略处理新的死信。
//
// 特性：
//   - 容量限制：防止内存无限增长
//   - 丢弃策略：可选择丢弃最旧或最新的死信
//   - 优雅关闭：等待所有消费者完成
//   - 监控指标：暴露队列大小、丢弃数等指标
//
// 使用示例：
//
//	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
//	    MaxSize: 1000,
//	    Workers: 3,
//	    DropPolicy: DropOldest,
//	})
//	dlq.AddConsumer(FileDeadLetterConsumer{Path: "deadletter.log"})
//	dlq.Start()
//	defer dlq.Shutdown(context.Background())
type DeadLetterQueue struct {
	config       DeadLetterQueueConfig
	queue        chan DeadLetterItem
	consumers    []DeadLetterConsumer
	consumerSnap atomic.Value // []DeadLetterConsumer for workers to read without locks

	// 统计信息
	dropped   atomic.Int64 // 丢弃的死信数量
	processed atomic.Int64 // 处理的死信数量

	// 生命周期管理
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex // 保护 consumers
	enqueueMu   sync.Mutex   // 保护 Enqueue 操作，避免 DropOldest 策略的竞态条件
	queueClosed atomic.Bool
	closeOnce   sync.Once
}

// DeadLetterQueueConfig 死信队列配置
type DeadLetterQueueConfig struct {
	// MaxSize 队列最大容量，0 表示不限制（默认 10000）
	MaxSize int

	// Workers 消费者 worker 数量（默认 1）
	Workers int

	// DropPolicy 队列满时的丢弃策略（默认 DropOldest）
	DropPolicy DropPolicy

	// OnDropped 死信被丢弃时的回调（可选）
	OnDropped func(item DeadLetterItem, reason string)

	// OnProcessed 死信被处理时的回调（可选）
	OnProcessed func(item DeadLetterItem, duration time.Duration)
}

// DropPolicy 丢弃策略
type DropPolicy int

const (
	// DropOldest 丢弃最旧的死信（队列头部）
	DropOldest DropPolicy = iota
	// DropNewest 丢弃最新的死信（当前提交的）
	DropNewest
	// BlockUntilSpace 阻塞直到有空间（不推荐，可能导致处理延迟）
	BlockUntilSpace
)

// NewDeadLetterQueue 创建一个新的死信队列
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

// AddConsumer 添加死信消费者
func (dlq *DeadLetterQueue) AddConsumer(consumer DeadLetterConsumer) {
	dlq.mu.Lock()
	dlq.consumers = append(dlq.consumers, consumer)
	snapshot := append([]DeadLetterConsumer(nil), dlq.consumers...)
	dlq.consumerSnap.Store(snapshot)
	dlq.mu.Unlock()
	logrus.WithField("consumer_count", len(snapshot)).Info("[DeadLetterQueue] Consumer added")
}

// Start 启动死信队列处理
func (dlq *DeadLetterQueue) Start() {
	consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)

	if len(consumers) == 0 {
		logrus.Warn("[DeadLetterQueue] No consumers registered, dead letters will be queued but not processed")
	}

	// 启动多个 worker
	for i := 0; i < dlq.config.Workers; i++ {
		dlq.wg.Add(1)
		go dlq.worker(i, consumers)
	}

	logrus.WithFields(logrus.Fields{
		"workers":   dlq.config.Workers,
		"max_size":  dlq.config.MaxSize,
		"consumers": len(consumers),
	}).Info("[DeadLetterQueue] Started")
}

// worker 死信处理 worker
func (dlq *DeadLetterQueue) worker(id int, _ []DeadLetterConsumer) {
	defer dlq.wg.Done()

	logrus.WithField("worker_id", id).Debug("[DeadLetterQueue] Worker started")

	for {
		select {
		case <-dlq.ctx.Done():
			logrus.WithField("worker_id", id).Debug("[DeadLetterQueue] Worker stopped")
			return

		case item, ok := <-dlq.queue:
			if !ok {
				// Channel closed, exit worker
				logrus.WithField("worker_id", id).Debug("[DeadLetterQueue] Queue closed, worker exiting")
				return
			}

			consumers := dlq.consumerSnap.Load().([]DeadLetterConsumer)
			start := time.Now()

			// 处理死信
			for _, consumer := range consumers {
				func(c DeadLetterConsumer, it DeadLetterItem) {
					defer func() {
						if r := recover(); r != nil {
							logrus.WithField("worker_id", id).WithField("panic", r).Error("[DeadLetterQueue] Consumer panic recovered")
						}
					}()
					c.Consume(it)
				}(consumer, item)
			}

			duration := time.Since(start)
			dlq.processed.Add(1)

			// 回调
			if dlq.config.OnProcessed != nil {
				dlq.config.OnProcessed(item, duration)
			}

			logrus.WithFields(logrus.Fields{
				"worker_id":  id,
				"event_id":   string(item.Event.ID),
				"event_type": item.Event.Type,
				"duration":   duration,
			}).Debug("[DeadLetterQueue] Dead letter processed")
		}
	}
}

// Enqueue 将死信加入队列
//
// 如果队列已满，根据配置的丢弃策略处理：
//   - DropOldest: 丢弃队列头部的死信，加入新死信
//   - DropNewest: 丢弃当前死信
//   - BlockUntilSpace: 阻塞直到有空间
//
// 使用互斥锁保护整个 enqueue 流程，避免并发竞态条件。
func (dlq *DeadLetterQueue) Enqueue(item DeadLetterItem) {
	if dlq.queueClosed.Load() {
		logrus.WithField("event_id", string(item.Event.ID)).Warn("[DeadLetterQueue] Queue closed, dropping new item")
		dlq.dropped.Add(1)
		if dlq.config.OnDropped != nil {
			dlq.config.OnDropped(item, "queue closed")
		}
		return
	}

	// 非阻塞策略使用互斥锁保护
	if dlq.config.DropPolicy != BlockUntilSpace {
		dlq.enqueueMu.Lock()
		defer dlq.enqueueMu.Unlock()
	}

	select {
	case dlq.queue <- item:
		// 成功加入队列
		return

	default:
		// 队列已满
		switch dlq.config.DropPolicy {
		case DropOldest:
			// 在互斥锁保护下，安全地移除最旧的死信
			select {
			case old := <-dlq.queue:
				// 成功移除旧元素
				dlq.dropped.Add(1)
				if dlq.config.OnDropped != nil {
					dlq.config.OnDropped(old, "queue full, dropping oldest")
				}
				logrus.WithFields(logrus.Fields{
					"old_event_id": string(old.Event.ID),
					"new_event_id": string(item.Event.ID),
				}).Debug("[DeadLetterQueue] Queue full, dropped oldest item")

				// 现在队列有空间，直接放入（在互斥锁保护下，保证成功）
				dlq.queue <- item
				return

			default:
				// 队列为空（不应该发生，因为我们刚检查到队列满）
				// 但为了安全，还是丢弃新死信
				dlq.dropped.Add(1)
				if dlq.config.OnDropped != nil {
					dlq.config.OnDropped(item, "queue full, cannot drop oldest")
				}
				logrus.WithField("event_id", string(item.Event.ID)).Warn("[DeadLetterQueue] Unexpected: queue full but cannot drop oldest")
			}

		case DropNewest:
			// 直接丢弃新死信
			dlq.dropped.Add(1)
			if dlq.config.OnDropped != nil {
				dlq.config.OnDropped(item, "queue full, dropping newest")
			}
			logrus.WithField("event_id", string(item.Event.ID)).Debug("[DeadLetterQueue] Queue full, dropped newest item")

		case BlockUntilSpace:
			// 阻塞直到有空间（使用带超时的 context 避免永久阻塞）
			ctx, cancel := context.WithTimeout(dlq.ctx, 30*time.Second)
			defer cancel()

			select {
			case dlq.queue <- item:
				logrus.WithField("event_id", string(item.Event.ID)).Debug("[DeadLetterQueue] Item enqueued after waiting")
				return

			case <-ctx.Done():
				// 超时，丢弃死信
				dlq.dropped.Add(1)
				if dlq.config.OnDropped != nil {
					dlq.config.OnDropped(item, "queue full, blocked timeout")
				}
				logrus.WithField("event_id", string(item.Event.ID)).Error("[DeadLetterQueue] Timeout waiting for queue space, dropped item")
			}
		}
	}
}

// Shutdown 优雅关闭死信队列
//
// 停止接收新的死信，等待所有已入队的死信被处理完毕。
func (dlq *DeadLetterQueue) Shutdown(ctx context.Context) error {
	logrus.Info("[DeadLetterQueue] Shutting down...")

	dlq.closeOnce.Do(func() {
		close(dlq.queue)
	})
	dlq.queueClosed.Store(true)

	// 取消所有 worker 的 context（但他们会继续处理队列中的死信）
	dlq.cancel()

	// 等待所有 worker 完成
	done := make(chan struct{})
	go func() {
		dlq.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("[DeadLetterQueue] Shutdown complete")
		return nil

	case <-ctx.Done():
		logrus.Warn("[DeadLetterQueue] Shutdown timeout, some dead letters may not be processed")
		return ctx.Err()
	}
}

// Stats 获取死信队列统计信息
func (dlq *DeadLetterQueue) Stats() DeadLetterQueueStats {
	return DeadLetterQueueStats{
		QueueSize: len(dlq.queue),
		MaxSize:   dlq.config.MaxSize,
		Dropped:   dlq.dropped.Load(),
		Processed: dlq.processed.Load(),
		Workers:   dlq.config.Workers,
	}
}

// DeadLetterQueueStats 死信队列统计信息
type DeadLetterQueueStats struct {
	QueueSize int   // 当前队列大小
	MaxSize   int   // 最大队列容量
	Dropped   int64 // 丢弃的死信数量
	Processed int64 // 处理的死信数量
	Workers   int   // Worker 数量
}
