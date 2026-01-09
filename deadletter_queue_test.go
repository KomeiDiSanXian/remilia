package remilia

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infradlq "github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// MockDeadLetterConsumer mock consumer for testing
type MockDeadLetterConsumer struct {
	consumed atomic.Int64
	items    []DeadLetterItem
	mu       sync.Mutex
}

func (m *MockDeadLetterConsumer) Consume(item DeadLetterItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consumed.Add(1)
	m.items = append(m.items, item)
}

func (m *MockDeadLetterConsumer) GetConsumed() int64 {
	return m.consumed.Load()
}

func (m *MockDeadLetterConsumer) GetItems() []DeadLetterItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DeadLetterItem(nil), m.items...)
}

func TestDeadLetterQueue_Basic(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	consumer := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	// Enqueue some dead letters
	for i := 0; i < 5; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check stats
	stats := q.Stats()
	assert.Equal(t, int64(5), consumer.GetConsumed())
	assert.Equal(t, int64(0), stats.Dropped)
	assert.Equal(t, int64(5), stats.Processed)
}

func TestDeadLetterQueue_DropOldest(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    3,
		Workers:    1,
		DropPolicy: DropOldest,
	})

	// Use a slow consumer to make queue fill up
	slowConsumer := DeadLetterConsumerFunc(func(item DeadLetterItem) {
		time.Sleep(50 * time.Millisecond)
	})
	q.AddConsumer(slowConsumer)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	// Enqueue more than capacity quickly
	for i := 0; i < 10; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Wait a bit for some processing
	time.Sleep(100 * time.Millisecond)

	stats := q.Stats()

	// Some should be dropped because we enqueued 10 items into a queue of size 3
	// with slow processing
	assert.Greater(t, stats.Dropped, int64(0))

	// Total items handled
	assert.LessOrEqual(t, stats.Processed+stats.Dropped, int64(10))
}

func TestDeadLetterQueue_DropNewest(t *testing.T) {
	dropped := atomic.Int64{}

	q := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    2,
		Workers:    0,
		DropPolicy: DropNewest,
		OnDropped: func(_ infradlq.DeadLetterItem, _ string) {
			dropped.Add(1)
		},
	})

	// Enqueue more than capacity
	for i := 0; i < 5; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	stats := q.Stats()
	assert.Equal(t, 2, stats.QueueSize)
	assert.Equal(t, int64(3), dropped.Load())
}

func TestDeadLetterQueue_MultipleConsumers(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 10, Workers: 1})

	consumer1 := &MockDeadLetterConsumer{}
	consumer2 := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer1)
	q.AddConsumer(consumer2)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	// Enqueue
	q.Enqueue(DeadLetterItem{
		Event: &dto.Payload{
			ID:   "test-event",
			Type: dto.C2CMessageCreate,
		},
		Err:     assert.AnError,
		Attempt: 1,
		Source:  "test",
	})

	// Wait
	time.Sleep(100 * time.Millisecond)

	// Both should receive
	assert.Equal(t, int64(1), consumer1.GetConsumed())
	assert.Equal(t, int64(1), consumer2.GetConsumed())
}

func TestDeadLetterQueue_MultipleWorkers(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 100, Workers: 3})

	consumer := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	// Enqueue many
	count := 50
	for i := 0; i < count; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i%10))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Wait
	time.Sleep(500 * time.Millisecond)

	// All should be processed
	assert.Equal(t, int64(count), consumer.GetConsumed())
}

func TestDeadLetterQueue_Shutdown(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 10, Workers: 3})

	consumer := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer)
	q.Start()

	// Enqueue
	for i := 0; i < 5; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Ensure at least one item is consumed before shutdown (reduces flakes on slow CI)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if consumer.GetConsumed() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Graceful shutdown with longer timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := q.Shutdown(ctx)
	assert.NoError(t, err)

	// Wait for final processing (best effort)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		consumed := consumer.GetConsumed()
		if consumed >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// All should be processed
	consumed := consumer.GetConsumed()
	assert.GreaterOrEqual(t, consumed, int64(3), "at least 3 items should be processed")
	assert.LessOrEqual(t, consumed, int64(5), "at most 5 items should be processed")
}

func TestDeadLetterQueue_ShutdownTimeout(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 10, Workers: 1})

	// Block the consumer until shutdown timeout context is done.
	blockCh := make(chan struct{})
	blocked := make(chan struct{}, 1)
	consumer := DeadLetterConsumer(DeadLetterConsumerFunc(func(item DeadLetterItem) {
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-blockCh
	}))

	q.AddConsumer(consumer)
	q.Start()

	// Enqueue at least one item so worker starts consuming.
	q.Enqueue(DeadLetterItem{
		Event: &dto.Payload{
			ID:   "test-event-timeout",
			Type: dto.C2CMessageCreate,
		},
		Err:     assert.AnError,
		Attempt: 1,
		Source:  "test",
	})

	// Ensure consumer has started.
	select {
	case <-blocked:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not start in time")
	}

	// Short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := q.Shutdown(ctx)
	close(blockCh)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestDeadLetterQueue_Stats(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 5, Workers: 1})

	consumer := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	// Initial stats
	stats := q.Stats()
	assert.Equal(t, 0, stats.QueueSize)
	assert.Equal(t, 5, stats.MaxSize)
	assert.Equal(t, int64(0), stats.Processed)
	assert.Equal(t, int64(0), stats.Dropped)
	assert.Equal(t, 1, stats.Workers)

	// Enqueue
	q.Enqueue(DeadLetterItem{
		Event: &dto.Payload{
			ID:   "test-event",
			Type: dto.C2CMessageCreate,
		},
		Err:     assert.AnError,
		Attempt: 1,
		Source:  "test",
	})

	// Wait
	time.Sleep(100 * time.Millisecond)

	stats = q.Stats()
	assert.Equal(t, int64(1), stats.Processed)
}

func TestDeadLetterQueue_OnDroppedCallback(t *testing.T) {
	dropped := make([]DeadLetterItem, 0)
	reasons := make([]string, 0)

	q := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    2,
		Workers:    0,
		DropPolicy: DropNewest,
		OnDropped: func(item infradlq.DeadLetterItem, reason string) {
			dropped = append(dropped, DeadLetterItem(item))
			reasons = append(reasons, reason)
		},
	})

	// Enqueue more than capacity
	for i := 0; i < 5; i++ {
		q.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// 3 should be dropped
	assert.Equal(t, 3, len(dropped))
	assert.Equal(t, 3, len(reasons))

	// Check reasons
	for _, reason := range reasons {
		assert.Contains(t, reason, "dropping newest")
	}
}

func TestDeadLetterQueue_OnProcessedCallback(t *testing.T) {
	var mu sync.Mutex
	processed := make([]DeadLetterItem, 0)
	durations := make([]time.Duration, 0)

	q := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
		OnProcessed: func(item infradlq.DeadLetterItem, duration time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			processed = append(processed, DeadLetterItem(item))
			durations = append(durations, duration)
		},
	})

	consumer := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer)
	q.Start()
	defer func() { _ = q.Shutdown(context.Background()) }()

	q.Enqueue(DeadLetterItem{
		Event: &dto.Payload{
			ID:   "test-event",
			Type: dto.C2CMessageCreate,
		},
		Err:     assert.AnError,
		Attempt: 1,
		Source:  "test",
	})

	// Wait
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, len(processed))
	assert.Equal(t, 1, len(durations))
	// Duration might be 0 for very fast processing, just check it's non-negative
	assert.GreaterOrEqual(t, durations[0], time.Duration(0))
}

func TestDeadLetterQueue_DynamicConsumerUpdate(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 10, Workers: 1})
	defer func() { _ = q.Shutdown(context.Background()) }()

	consumer1 := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer1)
	q.Start()

	q.Enqueue(newTestDeadLetterItem("dynamic-1"))
	waitForCondition(t, 500*time.Millisecond, func() bool {
		return consumer1.GetConsumed() == 1
	})

	consumer2 := &MockDeadLetterConsumer{}
	q.AddConsumer(consumer2)

	q.Enqueue(newTestDeadLetterItem("dynamic-2"))
	waitForCondition(t, 500*time.Millisecond, func() bool {
		return consumer2.GetConsumed() == 1
	})
}

func TestDeadLetterQueue_ConsumerPanicRecovered(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{MaxSize: 10, Workers: 1})
	defer func() { _ = q.Shutdown(context.Background()) }()

	panicCount := atomic.Int32{}
	panicConsumer := DeadLetterConsumerFunc(func(item DeadLetterItem) {
		panicCount.Add(1)
		panic("boom")
	})

	safeConsumer := &MockDeadLetterConsumer{}
	q.AddConsumer(panicConsumer)
	q.AddConsumer(safeConsumer)
	q.Start()

	q.Enqueue(newTestDeadLetterItem("panic-1"))
	q.Enqueue(newTestDeadLetterItem("panic-2"))

	waitForCondition(t, time.Second, func() bool {
		return safeConsumer.GetConsumed() >= 2
	})
	assert.Equal(t, int32(2), panicCount.Load(), "panic consumer should be invoked for each item")
}

func newTestDeadLetterItem(id string) DeadLetterItem {
	return DeadLetterItem{
		Event: &dto.Payload{
			ID:   dto.EventID(id),
			Type: dto.C2CMessageCreate,
		},
		Err:     assert.AnError,
		Attempt: 1,
		Source:  "test",
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// DeadLetterConsumerFunc adapter
type DeadLetterConsumerFunc func(item DeadLetterItem)

func (f DeadLetterConsumerFunc) Consume(item DeadLetterItem) {
	f(item)
}

func TestDeadLetterQueue_DefaultConfig(t *testing.T) {
	q := NewDeadLetterQueue(DeadLetterQueueConfig{})
	stats := q.Stats()
	assert.Equal(t, 10000, stats.MaxSize)
	assert.Equal(t, 1, stats.Workers)
}
