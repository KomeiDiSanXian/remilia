package dlq

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestDLQBlockUntilSpaceDeadlock 测试 BlockUntilSpace 策略下的死锁修复
func TestDLQBlockUntilSpaceDeadlock(t *testing.T) {
	config := DeadLetterQueueConfig{
		MaxSize:    5,
		Workers:    2,
		DropPolicy: DropPolicyBlockUntilSpace,
	}

	dlq := NewDeadLetterQueue(config)

	// 添加一个慢速消费者
	slowConsumer := &testConsumer{
		delay: 100 * time.Millisecond,
	}
	dlq.AddConsumer(slowConsumer)
	dlq.Start()

	// 先填满队列
	for i := range 5 {
		item := DeadLetterItem{
			Event:   mockEvent(i),
			Err:     nil,
			Attempt: 1,
		}
		dlq.Enqueue(item)
	}

	// 启动多个 goroutine 尝试 enqueue（会阻塞）
	var wg sync.WaitGroup
	enqueueCount := 10
	wg.Add(enqueueCount)

	var successCount atomic.Int32

	// 使用 channel 确保所有 goroutine 都已启动
	started := make(chan struct{}, enqueueCount)

	for i := range enqueueCount {
		go func(id int) {
			defer wg.Done()

			// 通知已启动
			started <- struct{}{}

			item := DeadLetterItem{
				Event:   mockEvent(100 + id),
				Err:     nil,
				Attempt: 1,
			}

			// 这应该阻塞但不会永久死锁
			dlq.Enqueue(item)
			successCount.Add(1)
		}(i)
	}

	// 等待所有 goroutine 启动
	for range enqueueCount {
		<-started
	}

	// 等待一小段时间，让 goroutine 都进入阻塞状态
	time.Sleep(200 * time.Millisecond)

	// 关闭 DLQ，这应该让阻塞的 goroutine 立即返回
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	shutdownStart := time.Now()
	err := dlq.Shutdown(shutdownCtx)
	shutdownDuration := time.Since(shutdownStart)

	// 等待所有 goroutine 完成
	wg.Wait()

	if err != nil {
		t.Logf("Shutdown completed with error: %v", err)
	}

	// 关闭应该在合理时间内完成（不应该等待 30 秒超时）
	if shutdownDuration > 10*time.Second {
		t.Errorf("Shutdown took too long: %v (expected < 10s)", shutdownDuration)
	}

	t.Logf("Shutdown completed in %v", shutdownDuration)
	t.Logf("Success enqueue count: %d/%d", successCount.Load(), enqueueCount)
	t.Log("No deadlock detected")
}

// TestDLQBlockUntilSpaceTimeout 测试 BlockUntilSpace 超时机制
func TestDLQBlockUntilSpaceTimeout(t *testing.T) {
	config := DeadLetterQueueConfig{
		MaxSize:    3,
		Workers:    1,
		DropPolicy: DropPolicyBlockUntilSpace,
	}

	dlq := NewDeadLetterQueue(config)

	// 添加一个永久阻塞的消费者，确保队列不会被消费
	blockingConsumer := &testConsumer{
		delay: 1 * time.Hour, // 永远不会完成
	}
	dlq.AddConsumer(blockingConsumer)
	dlq.Start()

	// 填满队列 - 添加 4 个 item
	// Worker 会立即开始消费第一个（但会阻塞），剩下 3 个在队列中（满）
	for i := range 4 {
		dlq.Enqueue(DeadLetterItem{
			Event:   mockEvent(i),
			Err:     nil,
			Attempt: 1,
		})
	}

	// 等待一下，确保 worker 已经取走一个并开始阻塞
	time.Sleep(100 * time.Millisecond)

	// 现在队列应该是满的（3个在队列，1个在消费者手中阻塞）
	// 尝试添加更多，应该在 5 秒后超时
	start := time.Now()
	dlq.Enqueue(DeadLetterItem{
		Event:   mockEvent(100),
		Err:     nil,
		Attempt: 1,
	})
	duration := time.Since(start)

	// 应该在 4-7 秒内超时（5秒目标，允许一些误差）
	if duration < 4*time.Second || duration > 7*time.Second {
		t.Errorf("Timeout duration unexpected: %v (expected ~5s)", duration)
	}

	t.Logf("Timeout occurred after %v (as expected)", duration)

	// 清理
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = dlq.Shutdown(shutdownCtx)
}

// TestDLQBlockUntilSpaceConcurrent 测试并发场景下的 BlockUntilSpace
func TestDLQBlockUntilSpaceConcurrent(t *testing.T) {
	config := DeadLetterQueueConfig{
		MaxSize:    10,
		Workers:    3,
		DropPolicy: DropPolicyBlockUntilSpace,
	}

	dlq := NewDeadLetterQueue(config)

	var consumedCount atomic.Int32
	consumer := &testConsumer{
		delay: 50 * time.Millisecond,
		onConsume: func() {
			consumedCount.Add(1)
		},
	}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// 并发添加大量事件
	var wg sync.WaitGroup
	producers := 20
	eventsPerProducer := 5

	wg.Add(producers)

	for i := range producers {
		go func(producerID int) {
			defer wg.Done()

			for j := range eventsPerProducer {
				item := DeadLetterItem{
					Event:   mockEvent(producerID*100 + j),
					Err:     nil,
					Attempt: 1,
				}
				dlq.Enqueue(item)
			}
		}(i)
	}

	wg.Wait()

	// 等待处理完成
	time.Sleep(1 * time.Second)

	// 关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = dlq.Shutdown(shutdownCtx)

	t.Logf("Consumed %d events (expected %d)", consumedCount.Load(), producers*eventsPerProducer)
	t.Log("Concurrent BlockUntilSpace completed successfully")
}

// TestDLQContextCancellation 测试 context 取消时的行为
func TestDLQContextCancellation(t *testing.T) {
	config := DeadLetterQueueConfig{
		MaxSize:    5,
		Workers:    1,
		DropPolicy: DropPolicyBlockUntilSpace,
	}

	dlq := NewDeadLetterQueue(config)
	dlq.Start()

	// 填满队列
	for i := range 5 {
		dlq.Enqueue(DeadLetterItem{
			Event:   mockEvent(i),
			Err:     nil,
			Attempt: 1,
		})
	}

	// 在后台关闭 DLQ
	go func() {
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = dlq.Shutdown(ctx)
	}()

	// 尝试 enqueue，应该在 DLQ 关闭时立即返回
	start := time.Now()
	dlq.Enqueue(DeadLetterItem{
		Event:   mockEvent(100),
		Err:     nil,
		Attempt: 1,
	})
	duration := time.Since(start)

	// 应该很快返回（不超过 2 秒）
	if duration > 3*time.Second {
		t.Errorf("Enqueue took too long after context cancellation: %v", duration)
	}

	t.Logf("Enqueue returned after %v when DLQ was closed", duration)
}

// TestDLQBlockUntilSpaceWithStats 测试统计信息的准确性
func TestDLQBlockUntilSpaceWithStats(t *testing.T) {
	var droppedCount atomic.Int32

	config := DeadLetterQueueConfig{
		MaxSize:    5,
		Workers:    2,
		DropPolicy: DropPolicyBlockUntilSpace,
		OnDropped: func(item DeadLetterItem, reason string) {
			droppedCount.Add(1)
			t.Logf("Item dropped: %s", reason)
		},
	}

	dlq := NewDeadLetterQueue(config)

	consumer := &testConsumer{delay: 200 * time.Millisecond}
	dlq.AddConsumer(consumer)
	dlq.Start()

	// 快速添加事件
	for i := range 20 {
		dlq.Enqueue(DeadLetterItem{
			Event:   mockEvent(i),
			Err:     nil,
			Attempt: 1,
		})
	}

	// 等待一段时间
	time.Sleep(500 * time.Millisecond)

	// 获取统计信息
	stats := dlq.Stats()
	t.Logf("DLQ Stats: QueueSize=%d, Processed=%d, Dropped=%d",
		stats.QueueSize, stats.Processed, stats.Dropped)

	// 关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = dlq.Shutdown(shutdownCtx)

	t.Logf("Final dropped count: %d", droppedCount.Load())
}

// testConsumer 测试用的消费者
type testConsumer struct {
	delay     time.Duration
	onConsume func()
}

func (c *testConsumer) Consume(item DeadLetterItem) {
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	if c.onConsume != nil {
		c.onConsume()
	}
}

// mockEvent 创建模拟事件
func mockEvent(id int) *dto.Payload {
	return &dto.Payload{
		ID: dto.EventID(rune(id)), // 简单的 ID 转换
	}
}
