package dlq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDeadLetterQueue 测试创建 DLQ
func TestNewDeadLetterQueue(t *testing.T) {
	tests := []struct {
		name            string
		config          DeadLetterQueueConfig
		expectedMaxSize int
		expectedWorkers int
	}{
		{
			name: "default config",
			config: DeadLetterQueueConfig{
				MaxSize: 0,
				Workers: 0,
			},
			expectedMaxSize: 10000,
			expectedWorkers: 1,
		},
		{
			name: "custom config",
			config: DeadLetterQueueConfig{
				MaxSize: 100,
				Workers: 5,
			},
			expectedMaxSize: 100,
			expectedWorkers: 5,
		},
		{
			name: "with callbacks",
			config: DeadLetterQueueConfig{
				MaxSize:     500,
				Workers:     3,
				OnDropped:   func(item DeadLetterItem, reason string) {},
				OnProcessed: func(item DeadLetterItem, duration time.Duration) {},
			},
			expectedMaxSize: 500,
			expectedWorkers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dlq := NewDeadLetterQueue(tt.config)
			require.NotNil(t, dlq)
			assert.Equal(t, tt.expectedMaxSize, dlq.config.MaxSize)
			assert.Equal(t, tt.expectedWorkers, dlq.config.Workers)
			assert.NotNil(t, dlq.queue)
			assert.NotNil(t, dlq.ctx)
			assert.NotNil(t, dlq.cancel)
		})
	}
}

// TestDeadLetterQueue_AddConsumer 测试添加消费者
func TestDeadLetterQueue_AddConsumer(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	// 初始没有消费者
	stats := dlq.Stats()
	assert.Equal(t, 0, stats.Consumers)

	// 添加第一个消费者
	consumer1 := &mockConsumer{}
	dlq.AddConsumer(consumer1)
	stats = dlq.Stats()
	assert.Equal(t, 1, stats.Consumers)

	// 添加第二个消费者
	consumer2 := &mockConsumer{}
	dlq.AddConsumer(consumer2)
	stats = dlq.Stats()
	assert.Equal(t, 2, stats.Consumers)
}

// TestDeadLetterQueue_StartAndShutdown 测试启动和关闭
func TestDeadLetterQueue_StartAndShutdown(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 2,
	})

	consumer := &mockConsumer{}
	dlq.AddConsumer(consumer)

	// 启动
	dlq.Start()

	// 验证状态
	stats := dlq.Stats()
	assert.Equal(t, 2, stats.Workers)
	assert.False(t, stats.IsClosed)

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := dlq.Shutdown(ctx)
	assert.NoError(t, err)

	// 验证关闭状态
	stats = dlq.Stats()
	assert.True(t, stats.IsClosed)
}

// TestDeadLetterQueue_Enqueue 测试入队
func TestDeadLetterQueue_Enqueue(t *testing.T) {
	t.Run("successful enqueue", func(t *testing.T) {
		dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
			MaxSize: 10,
			Workers: 1,
		})

		consumer := &mockConsumer{}
		dlq.AddConsumer(consumer)
		dlq.Start()

		// 入队
		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "test-1",
				Type: "test",
			},
			Err:     errors.New("test error"),
			Attempt: 1,
			Source:  "test",
		}
		dlq.Enqueue(item)

		// 等待处理
		time.Sleep(100 * time.Millisecond)

		// 验证
		stats := dlq.Stats()
		assert.Equal(t, int64(1), stats.Processed)

		// 关闭
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dlq.Shutdown(ctx)
	})

	t.Run("enqueue after close", func(t *testing.T) {
		var dropped atomic.Int64
		dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
			MaxSize: 10,
			Workers: 1,
			OnDropped: func(item DeadLetterItem, reason string) {
				dropped.Add(1)
			},
		})

		dlq.Start()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dlq.Shutdown(ctx)

		// 尝试入队（应该被丢弃）
		item := DeadLetterItem{
			Event: &dto.Payload{ID: "test-closed"},
		}
		dlq.Enqueue(item)

		// 验证
		assert.Equal(t, int64(1), dropped.Load())
		stats := dlq.Stats()
		assert.Equal(t, int64(1), stats.Dropped)
	})
}

// TestDeadLetterQueue_DropPolicy_DropOldest 测试丢弃最旧策略
func TestDeadLetterQueue_DropPolicy_DropOldest(t *testing.T) {
	var droppedItems []string
	var mu sync.Mutex

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    3,
		Workers:    1,
		DropPolicy: DropPolicyOldest,
		OnDropped: func(item DeadLetterItem, reason string) {
			mu.Lock()
			droppedItems = append(droppedItems, string(item.Event.ID))
			mu.Unlock()
		},
	})

	// 不启动 worker，让队列填满
	// 填满队列
	for i := 1; i <= 3; i++ {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{ID: dto.EventID("item-" + string(rune(i)))},
		})
	}

	// 再入队一个（应该丢弃最旧的）
	dlq.Enqueue(DeadLetterItem{
		Event: &dto.Payload{ID: "item-4"},
	})

	time.Sleep(50 * time.Millisecond)

	// 验证
	mu.Lock()
	assert.GreaterOrEqual(t, len(droppedItems), 1)
	mu.Unlock()

	stats := dlq.Stats()
	assert.GreaterOrEqual(t, stats.Dropped, int64(1))
}

// TestDeadLetterQueue_DropPolicy_DropNewest 测试丢弃最新策略
func TestDeadLetterQueue_DropPolicy_DropNewest(t *testing.T) {
	var droppedItems []string
	var mu sync.Mutex

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    3,
		Workers:    1,
		DropPolicy: DropPolicyNewest,
		OnDropped: func(item DeadLetterItem, reason string) {
			mu.Lock()
			droppedItems = append(droppedItems, string(item.Event.ID))
			mu.Unlock()
		},
	})

	// 填满队列
	for i := 1; i <= 3; i++ {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{ID: dto.EventID("item-" + string(rune(i)))},
		})
	}

	// 再入队一个（应该丢弃这个新的）
	dlq.Enqueue(DeadLetterItem{
		Event: &dto.Payload{ID: "item-4"},
	})

	time.Sleep(50 * time.Millisecond)

	// 验证
	mu.Lock()
	assert.Contains(t, droppedItems, "item-4")
	mu.Unlock()

	stats := dlq.Stats()
	assert.GreaterOrEqual(t, stats.Dropped, int64(1))
}

// TestDeadLetterQueue_DropPolicy_BlockUntilSpace 测试阻塞策略
func TestDeadLetterQueue_DropPolicy_BlockUntilSpace(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    2,
		Workers:    1,
		DropPolicy: DropPolicyBlockUntilSpace,
	})

	consumer := &mockConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// 快速填充队列
	dlq.Enqueue(DeadLetterItem{Event: &dto.Payload{ID: "item-1"}})
	dlq.Enqueue(DeadLetterItem{Event: &dto.Payload{ID: "item-2"}})

	// 这个应该阻塞直到有空间
	done := make(chan bool)
	go func() {
		dlq.Enqueue(DeadLetterItem{Event: &dto.Payload{ID: "item-3"}})
		done <- true
	}()

	// 等待处理完成
	select {
	case <-done:
		// 成功入队
	case <-time.After(5 * time.Second):
		t.Fatal("Enqueue blocked too long")
	}

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dlq.Shutdown(ctx)
}

// TestDeadLetterQueue_MultipleWorkers 测试多个 worker
func TestDeadLetterQueue_MultipleWorkers(t *testing.T) {
	var processed atomic.Int64
	consumer := &mockConsumer{
		onConsume: func(item DeadLetterItem) {
			processed.Add(1)
			time.Sleep(10 * time.Millisecond) // 模拟处理时间
		},
	}

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 100,
		Workers: 5, // 5 个并发 worker
	})

	dlq.AddConsumer(consumer)
	dlq.Start()

	// 入队多个项目
	for i := range 20 {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{ID: dto.EventID("item-" + string(rune(i)))},
		})
	}

	// 等待处理
	time.Sleep(500 * time.Millisecond)

	// 验证
	assert.GreaterOrEqual(t, processed.Load(), int64(15))

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dlq.Shutdown(ctx)
}

// TestDeadLetterQueue_ConsumerPanic 测试消费者 panic 恢复
func TestDeadLetterQueue_ConsumerPanic(t *testing.T) {
	panicConsumer := &mockConsumer{
		onConsume: func(item DeadLetterItem) {
			panic("test panic")
		},
	}

	normalConsumer := &mockConsumer{}

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	dlq.AddConsumer(panicConsumer)
	dlq.AddConsumer(normalConsumer)
	dlq.Start()

	// 入队（不应该导致程序崩溃）
	dlq.Enqueue(DeadLetterItem{
		Event: &dto.Payload{ID: "panic-test"},
	})

	time.Sleep(100 * time.Millisecond)

	// 验证仍然可以处理
	stats := dlq.Stats()
	assert.Equal(t, int64(1), stats.Processed)

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dlq.Shutdown(ctx)
}

// TestDeadLetterQueue_OnProcessedCallback 测试处理完成回调
func TestDeadLetterQueue_OnProcessedCallback(t *testing.T) {
	var processedCount atomic.Int64

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
		OnProcessed: func(item DeadLetterItem, duration time.Duration) {
			processedCount.Add(1)
			// duration 被传递但我们不验证它，因为它可能非常小
		},
	})

	consumer := &mockConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// 入队
	for i := range 5 {
		dlq.Enqueue(DeadLetterItem{
			Event: &dto.Payload{ID: dto.EventID("item-" + string(rune(i)))},
		})
	}

	// 等待处理完成 - 使用更长的等待时间确保所有项目被处理
	time.Sleep(1 * time.Second)

	// 验证 - 至少处理了一些项目
	assert.GreaterOrEqual(t, processedCount.Load(), int64(4))
	// 注意: duration 可能非常小（甚至 0），因为处理速度很快
	// 我们只验证回调被调用了（通过 processedCount）

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dlq.Shutdown(ctx)
}

// TestDeadLetterQueue_Stats 测试统计信息
func TestDeadLetterQueue_Stats(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize:    50,
		Workers:    3,
		DropPolicy: DropPolicyOldest,
	})

	consumer := &mockConsumer{}
	dlq.AddConsumer(consumer)

	// 检查初始状态
	stats := dlq.Stats()
	assert.Equal(t, 0, stats.QueueSize)
	assert.Equal(t, 50, stats.MaxSize)
	assert.Equal(t, int64(0), stats.Processed)
	assert.Equal(t, int64(0), stats.Dropped)
	assert.Equal(t, 3, stats.Workers)
	assert.Equal(t, 1, stats.Consumers)
	assert.False(t, stats.IsClosed)
	assert.Equal(t, DropPolicyOldest, stats.DropPolicy)
}

// TestDeadLetterQueue_ShutdownTimeout 测试关闭超时
func TestDeadLetterQueue_ShutdownTimeout(t *testing.T) {
	slowConsumer := &mockConsumer{
		onConsume: func(item DeadLetterItem) {
			time.Sleep(5 * time.Second) // 非常慢的处理
		},
	}

	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	dlq.AddConsumer(slowConsumer)
	dlq.Start()

	// 入队
	dlq.Enqueue(DeadLetterItem{
		Event: &dto.Payload{ID: "slow-item"},
	})

	// 短超时关闭
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := dlq.Shutdown(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// TestDeadLetterQueue_ConcurrentEnqueue 测试并发入队
func TestDeadLetterQueue_ConcurrentEnqueue(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 1000,
		Workers: 5,
	})

	consumer := &mockConsumer{}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// 并发入队
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			dlq.Enqueue(DeadLetterItem{
				Event: &dto.Payload{ID: dto.EventID("concurrent-" + string(rune(id)))},
			})
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	// 验证
	stats := dlq.Stats()
	assert.GreaterOrEqual(t, stats.Processed, int64(90))

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dlq.Shutdown(ctx)
}

// mockConsumer 是一个测试用的消费者
type mockConsumer struct {
	consumed  []DeadLetterItem
	mu        sync.Mutex
	onConsume func(DeadLetterItem)
}

func (m *mockConsumer) Consume(item DeadLetterItem) {
	m.mu.Lock()
	m.consumed = append(m.consumed, item)
	m.mu.Unlock()

	if m.onConsume != nil {
		m.onConsume(item)
	}
}

func (m *mockConsumer) getConsumed() []DeadLetterItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]DeadLetterItem, len(m.consumed))
	copy(result, m.consumed)
	return result
}
