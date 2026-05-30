package plugin

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventBus_PublishWithTimeout_ReturnsErrEventDropped 验证超时场景返回 ErrEventDropped。
func TestEventBus_PublishWithTimeout_ReturnsErrEventDropped(t *testing.T) {
	bus := NewEventBusWithOptions(EventBusOptions{WorkerPoolSize: 1}).(*eventBus)
	bus.workerPool <- struct{}{} // 填满唯一槽位

	_, err := bus.Subscribe("test", func(_ context.Context, data any) error { return nil })
	require.NoError(t, err)

	err = PublishWithTimeout(bus, "test", "data", 10*time.Millisecond)
	assert.ErrorIs(t, err, ErrEventDropped, "PublishWithTimeout should return ErrEventDropped when pool is full")

	<-bus.workerPool
}

// TestEventBus_PublishWithTimeout_Succeeds 验证正常场景 PublishWithTimeout 不会超时。
func TestEventBus_PublishWithTimeout_Succeeds(t *testing.T) {
	bus := NewEventBusWithOptions(EventBusOptions{WorkerPoolSize: 1}).(*eventBus)

	var called atomic.Bool
	_, err := bus.Subscribe("test", func(_ context.Context, data any) error {
		called.Store(true)
		return nil
	})
	require.NoError(t, err)

	err = PublishWithTimeout(bus, "test", "data", time.Second)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.True(t, called.Load(), "handler should have been called")
}

// TestEventBus_Publish_BlocksAndRecovers 验证 Publish 在池满时阻塞，槽位释放后继续。
func TestEventBus_Publish_BlocksAndRecovers(t *testing.T) {
	bus := NewEventBusWithOptions(EventBusOptions{WorkerPoolSize: 1}).(*eventBus)
	bus.workerPool <- struct{}{} // 占满唯一槽位

	_, err := bus.Subscribe("test", func(_ context.Context, data any) error { return nil })
	require.NoError(t, err)

	// 在 goroutine 中 publish（会阻塞）
	published := make(chan struct{})
	go func() {
		_ = bus.Publish("test", "data")
		close(published)
	}()

	// 短暂等待确认已阻塞
	select {
	case <-published:
		t.Fatal("Publish should have blocked when pool was full")
	case <-time.After(50 * time.Millisecond):
		// 确认阻塞，正常
	}

	// 释放槽位 → Publish 应继续
	<-bus.workerPool

	select {
	case <-published:
		// 继续了，OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Publish should have unblocked after slot was released")
	}
}

// TestEventBus_Subscribe_IDUnique 验证修复 #6：ID 全局唯一，订阅-取消-再订阅不会重复
func TestEventBus_Subscribe_IDUnique(t *testing.T) {
	bus := NewEventBus()
	// 第一次订阅
	sub1, err := bus.Subscribe("topic", func(_ context.Context, data any) error { return nil })
	require.NoError(t, err)
	// 取消订阅
	require.NoError(t, sub1.Unsubscribe())
	// 第二次订阅（同一 topic）
	sub2, err := bus.Subscribe("topic", func(_ context.Context, data any) error { return nil })
	require.NoError(t, err)
	// 两个订阅的 ID 应该不同
	impl1, ok1 := sub1.(*subscriptionImpl)
	impl2, ok2 := sub2.(*subscriptionImpl)
	require.True(t, ok1 && ok2)
	assert.NotEqual(t, impl1.id, impl2.id, "Subscription IDs must be unique across subscribe/unsubscribe cycles")
	// 使用第一个（已取消的）sub 对象调用 Unsubscribe 应该返回错误（找不到）
	err = sub1.Unsubscribe()
	assert.Error(t, err, "Old subscription should not be found after resubscribe")
	// 第二个订阅应该仍然有效，可以正常取消
	require.NoError(t, sub2.Unsubscribe())
}

// TestEventBus_PublishCount_Atomic 验证修复 #18：publishCount 使用 atomic，GetStats 正确返回
func TestEventBus_PublishCount_Atomic(t *testing.T) {
	bus := NewEventBus()
	received := atomic.Int32{}
	_, err := bus.Subscribe("count-topic", func(_ context.Context, data any) error {
		received.Add(1)
		return nil
	})
	require.NoError(t, err)
	// 并发发布
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_ = bus.Publish("count-topic", "data")
		})
	}
	wg.Wait()
	// 等待异步 handler 完成
	time.Sleep(100 * time.Millisecond)
	stats := bus.GetStats()
	assert.EqualValues(t, 50, stats.PublishCount, "PublishCount should be 50")
}
