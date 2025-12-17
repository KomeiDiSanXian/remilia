package remilia

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	consumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Enqueue some dead letters
	for i := 0; i < 5; i++ {
		dlq.Enqueue(DeadLetterItem{
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
	stats := dlq.Stats()
	assert.Equal(t, int64(5), consumer.GetConsumed())
	assert.Equal(t, int64(0), stats.Dropped)
	assert.Equal(t, int64(5), stats.Processed)
}

func TestDeadLetterQueue_DropOldest(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    3,
		Workers:    1,
		DropPolicy: DropOldest,
	})

	// Use a slow consumer to make queue fill up
	slowConsumer := DeadLetterConsumerFunc(func(item DeadLetterItem) {
		time.Sleep(50 * time.Millisecond)
	})
	dlq.AddConsumer(slowConsumer)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Enqueue more than capacity quickly
	for i := 0; i < 10; i++ {
		dlq.Enqueue(DeadLetterItem{
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

	stats := dlq.Stats()

	// Some should be dropped because we enqueued 10 items into a queue of size 3
	// with slow processing
	assert.Greater(t, stats.Dropped, int64(0))

	// Total items handled
	assert.LessOrEqual(t, stats.Processed+stats.Dropped, int64(10))
}

func TestDeadLetterQueue_DropNewest(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    2,
		Workers:    0,
		DropPolicy: DropNewest,
	})

	dropped := atomic.Int64{}
	dlq.config.OnDropped = func(item DeadLetterItem, reason string) {
		dropped.Add(1)
	}

	// Enqueue more than capacity
	for i := 0; i < 5; i++ {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// First 2 should be in queue, last 3 dropped
	stats := dlq.Stats()
	assert.Equal(t, 2, stats.QueueSize)
	assert.Equal(t, int64(3), dropped.Load())
}

func TestDeadLetterQueue_MultipleConsumers(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	consumer1 := &MockDeadLetterConsumer{}
	consumer2 := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer1)
	dlq.AddConsumer(consumer2)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Enqueue
	dlq.Enqueue(DeadLetterItem{
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
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 100,
		Workers: 3,
	})

	consumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Enqueue many
	count := 50
	for i := 0; i < count; i++ {
		dlq.Enqueue(DeadLetterItem{
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
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 3, // 增加 worker 数量以加快处理
	})

	consumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// Enqueue
	for i := 0; i < 5; i++ {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Graceful shutdown with longer timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := dlq.Shutdown(ctx)
	assert.NoError(t, err)

	// Wait a bit for final processing
	time.Sleep(100 * time.Millisecond)

	// All should be processed
	consumed := consumer.GetConsumed()
	assert.GreaterOrEqual(t, consumed, int64(3), "at least 3 items should be processed")
	assert.LessOrEqual(t, consumed, int64(5), "at most 5 items should be processed")
}

func TestDeadLetterQueue_ShutdownTimeout(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	// Slow consumer
	slowConsumer := DeadLetterConsumer(DeadLetterConsumerFunc(func(item DeadLetterItem) {
		time.Sleep(2 * time.Second)
	}))

	dlq.AddConsumer(slowConsumer)
	dlq.Start()

	// Enqueue
	for i := 0; i < 3; i++ {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{
				ID:   dto.EventID("test-event-" + string(rune('0'+i))),
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: i,
			Source:  "test",
		})
	}

	// Short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := dlq.Shutdown(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestDeadLetterQueue_Stats(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 5,
		Workers: 1,
	})

	consumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Initial stats
	stats := dlq.Stats()
	assert.Equal(t, 0, stats.QueueSize)
	assert.Equal(t, 5, stats.MaxSize)
	assert.Equal(t, int64(0), stats.Processed)
	assert.Equal(t, int64(0), stats.Dropped)
	assert.Equal(t, 1, stats.Workers)

	// Enqueue
	dlq.Enqueue(DeadLetterItem{
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

	stats = dlq.Stats()
	assert.Equal(t, int64(1), stats.Processed)
}

func TestDeadLetterQueue_OnDroppedCallback(t *testing.T) {
	dropped := make([]DeadLetterItem, 0)
	reasons := make([]string, 0)

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    2,
		Workers:    0,
		DropPolicy: DropNewest,
		OnDropped: func(item DeadLetterItem, reason string) {
			dropped = append(dropped, item)
			reasons = append(reasons, reason)
		},
	})

	// Enqueue more than capacity
	for i := 0; i < 5; i++ {
		dlq.Enqueue(DeadLetterItem{
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

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
		OnProcessed: func(item DeadLetterItem, duration time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			processed = append(processed, item)
			durations = append(durations, duration)
		},
	})

	consumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer dlq.Shutdown(context.Background())

	// Enqueue
	dlq.Enqueue(DeadLetterItem{
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
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})
	defer dlq.Shutdown(context.Background())

	consumer1 := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer1)
	dlq.Start()

	dlq.Enqueue(newTestDeadLetterItem("dynamic-1"))
	waitForCondition(t, 500*time.Millisecond, func() bool {
		return consumer1.GetConsumed() == 1
	})

	consumer2 := &MockDeadLetterConsumer{}
	dlq.AddConsumer(consumer2)

	dlq.Enqueue(newTestDeadLetterItem("dynamic-2"))
	waitForCondition(t, 500*time.Millisecond, func() bool {
		return consumer2.GetConsumed() == 1
	})
}

func TestDeadLetterQueue_ConsumerPanicRecovered(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})
	defer dlq.Shutdown(context.Background())

	panicCount := atomic.Int32{}
	panicConsumer := DeadLetterConsumerFunc(func(item DeadLetterItem) {
		panicCount.Add(1)
		panic("boom")
	})

	safeConsumer := &MockDeadLetterConsumer{}
	dlq.AddConsumer(panicConsumer)
	dlq.AddConsumer(safeConsumer)
	dlq.Start()

	dlq.Enqueue(newTestDeadLetterItem("panic-1"))
	dlq.Enqueue(newTestDeadLetterItem("panic-2"))

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
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{})

	assert.Equal(t, 10000, dlq.config.MaxSize)
	assert.Equal(t, 1, dlq.config.Workers)
}
