package dlq

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Item represents a generic dead letter entry.
//
// This is a type-safe version of DeadLetterItem that works with any data type.
//
// Example:
//
//	// For HTTP requests
//	type FailedRequest struct {
//	    URL     string
//	    Body    []byte
//	    Headers map[string]string
//	}
//	httpItem := dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("connection timeout"),
//	    Attempt: 3,
//	    Source:  "http-client",
//	}
type Item[T any] struct {
	Data    T      // The failed data
	Err     error  // The error that caused the failure
	Attempt int    // Number of retry attempts
	Source  string // Source identifier
}

// Consumer consumes dead letter items of type T.
//
// Implementations should be thread-safe as they may be called
// concurrently by multiple workers.
type Consumer[T any] interface {
	Consume(item Item[T])
}

// Config configures a generic dead letter queue.
type Config[T any] struct {
	MaxSize     int                                        // Maximum queue size
	Workers     int                                        // Number of consumer workers
	DropPolicy  DropPolicy                                 // Policy when queue is full
	OnDropped   func(item Item[T], reason string)          // Callback when item is dropped
	OnProcessed func(item Item[T], duration time.Duration) // Callback when item is processed
}

// Queue is a generic dead letter queue that can handle any data type.
//
// It provides the same functionality as DeadLetterQueue but with type safety.
//
// Example:
//
//	// Create a DLQ for failed HTTP requests
//	type FailedRequest struct {
//	    URL  string
//	    Body []byte
//	}
//
//	dlq := dlq.New[*FailedRequest](dlq.Config[*FailedRequest]{
//	    MaxSize: 1000,
//	    Workers: 4,
//	    OnDropped: func(item dlq.Item[*FailedRequest], reason string) {
//	        log.Printf("Dropped request to %s: %s", item.Data.URL, reason)
//	    },
//	})
//
//	// Add a consumer
//	dlq.AddConsumer(myHTTPConsumer)
//
//	// Start processing
//	dlq.Start()
//
//	// Enqueue failed requests
//	dlq.Enqueue(dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("timeout"),
//	    Attempt: 3,
//	})
type Queue[T any] struct {
	config       Config[T]
	queue        chan Item[T]
	consumers    []Consumer[T]
	consumerSnap *infraatomic.Value[[]Consumer[T]]

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

// New creates a new generic dead letter queue with context.Background() as parent.
//
// For Bot scenarios, use [NewWithContext] to bind the DLQ lifetime to the Bot's
// root context so it stops automatically when the Bot stops.
//
// Default values:
//   - MaxSize: 10000 if not specified
//   - Workers: 1 if not specified
//
// Example:
//
//	dlq := dlq.New[*MyData](dlq.Config[*MyData]{
//	    MaxSize: 5000,
//	    Workers: 2,
//	})
func New[T any](config Config[T]) *Queue[T] {
	return NewWithContext[T](context.Background(), config)
}

// NewWithContext creates a new generic dead letter queue with the given parent context.
//
// When the parent context is cancelled (e.g. by Bot.Stop()), the DLQ stops
// accepting new items and waits for already-queued items to be processed.
// Recommended for Bot scenarios to tie the DLQ lifetime to the Bot:
//
//	dlq := dlq.NewWithContext[*MyData](bot.Context(), dlq.Config[*MyData]{...})
func NewWithContext[T any](parent context.Context, config Config[T]) *Queue[T] {
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}

	ctx, cancel := context.WithCancel(parent)

	q := &Queue[T]{
		config:       config,
		queue:        make(chan Item[T], config.MaxSize),
		consumers:    make([]Consumer[T], 0),
		consumerSnap: infraatomic.NewValue([]Consumer[T]{}),
		ctx:          ctx,
		cancel:       cancel,
	}
	return q
}

// AddConsumer adds a consumer to the queue.
//
// Consumers can be added before or after Start() is called.
// This method is thread-safe.
func (q *Queue[T]) AddConsumer(consumer Consumer[T]) {
	q.mu.Lock()
	q.consumers = append(q.consumers, consumer)
	snapshot := append([]Consumer[T](nil), q.consumers...)
	q.consumerSnap.Store(snapshot)
	q.mu.Unlock()
	logger.WithField("consumer_count", len(snapshot)).Info("[GenericDLQ] Consumer added")
}

// Start starts the consumer workers.
//
// This method should be called only once. Multiple calls will be ignored.
func (q *Queue[T]) Start() {
	consumers := q.consumerSnap.Load()
	if len(consumers) == 0 {
		logger.Warn("[GenericDLQ] No consumers registered, dead letters will be queued but not processed")
	}
	for i := 0; i < q.config.Workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	logger.WithFields(logger.Fields{
		"workers":   q.config.Workers,
		"max_size":  q.config.MaxSize,
		"consumers": len(consumers),
	}).Info("[GenericDLQ] Started")
}

// worker processes items from the queue.
func (q *Queue[T]) worker(id int) {
	defer q.wg.Done()

	logger.WithField("worker_id", id).Debug("[GenericDLQ] Worker started")

	for {
		select {
		case <-q.ctx.Done():
			logger.WithField("worker_id", id).Debug("[GenericDLQ] Worker stopping")
			return
		case item, ok := <-q.queue:
			if !ok {
				logger.WithField("worker_id", id).Debug("[GenericDLQ] Queue closed, worker stopping")
				return
			}

			start := time.Now()
			q.processItem(item)
			duration := time.Since(start)

			q.processed.Add(1)

			if q.config.OnProcessed != nil {
				q.config.OnProcessed(item, duration)
			}
		}
	}
}

// processItem sends an item to all consumers.
func (q *Queue[T]) processItem(item Item[T]) {
	consumers := q.consumerSnap.Load()

	for _, consumer := range consumers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithFields(logger.Fields{
						"panic":   r,
						"source":  item.Source,
						"attempt": item.Attempt,
					}).Error("[GenericDLQ] Consumer panic recovered")
				}
			}()
			consumer.Consume(item)
		}()
	}
}

// Enqueue adds an item to the dead letter queue.
//
// Behavior depends on DropPolicy:
//   - DropPolicyOldest: Drop oldest item if queue is full
//   - DropPolicyNewest: Drop the new item if queue is full
//   - DropPolicyBlockUntilSpace: Block until space is available
//
// Returns error if queue is closed or item is dropped.
func (q *Queue[T]) Enqueue(item Item[T]) error {
	if q.queueClosed.Load() {
		return ErrQueueClosed
	}

	q.enqueueMu.Lock()
	defer q.enqueueMu.Unlock()

	switch q.config.DropPolicy {
	case DropPolicyOldest:
		select {
		case q.queue <- item:
			return nil
		default:
			// Queue is full, drop oldest
			select {
			case old := <-q.queue:
				q.dropped.Add(1)
				if q.config.OnDropped != nil {
					q.config.OnDropped(old, "queue full (dropping oldest)")
				}
			default:
			}
			q.queue <- item
			return nil
		}

	case DropPolicyNewest:
		select {
		case q.queue <- item:
			return nil
		default:
			// Queue is full, drop newest
			q.dropped.Add(1)
			if q.config.OnDropped != nil {
				q.config.OnDropped(item, "queue full (dropping newest)")
			}
			return ErrQueueFull
		}

	case DropPolicyBlockUntilSpace:
		select {
		case <-q.ctx.Done():
			return q.ctx.Err()
		case q.queue <- item:
			return nil
		}

	default:
		return ErrInvalidDropPolicy
	}
}

// Stats returns current queue statistics.
func (q *Queue[T]) Stats() Stats {
	q.mu.RLock()
	consumerCount := len(q.consumers)
	q.mu.RUnlock()

	return Stats{
		QueueSize:  len(q.queue),
		MaxSize:    q.config.MaxSize,
		Processed:  q.processed.Load(),
		Dropped:    q.dropped.Load(),
		Workers:    q.config.Workers,
		IsClosed:   q.queueClosed.Load(),
		Consumers:  consumerCount,
		DropPolicy: q.config.DropPolicy,
	}
}

// Close gracefully shuts down the queue.
//
// It stops accepting new items, waits for workers to finish processing,
// and closes the queue channel.
func (q *Queue[T]) Close(timeout time.Duration) error {
	var err error
	q.closeOnce.Do(func() {
		logger.Info("[GenericDLQ] Closing...")

		// Mark queue as closed
		q.queueClosed.Store(true)

		// Close the queue channel (no more items can be added)
		close(q.queue)

		// Wait for workers to finish with timeout
		done := make(chan struct{})
		go func() {
			q.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Info("[GenericDLQ] All workers finished")
		case <-time.After(timeout):
			logger.Warn("[GenericDLQ] Close timeout, forcing shutdown")
			q.cancel()
			<-done
			err = ErrCloseTimeout
		}

		// Cancel context to stop any blocking operations
		q.cancel()

		stats := q.Stats()
		logger.WithFields(logger.Fields{
			"processed": stats.Processed,
			"dropped":   stats.Dropped,
		}).Info("[GenericDLQ] Closed")
	})
	return err
}

// Size returns the current number of items in the queue.
func (q *Queue[T]) Size() int {
	return len(q.queue)
}

// IsEmpty returns true if the queue is empty.
func (q *Queue[T]) IsEmpty() bool {
	return len(q.queue) == 0
}

// IsClosed returns true if the queue is closed.
func (q *Queue[T]) IsClosed() bool {
	return q.queueClosed.Load()
}
