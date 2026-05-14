package plugin

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventBus_Publish_NonBlocking 验证修复 #1：Publish 池满时不阻塞调用方
func TestEventBus_Publish_NonBlocking(t *testing.T) {
	bus := NewEventBus().(*eventBus)
	// 先填满 worker pool（100 个令牌）
	for range 100 {
		bus.workerPool <- struct{}{}
	}
	// 订阅一个 handler
	_, err := bus.Subscribe("test", func(data any) {})
	require.NoError(t, err)
	// Publish 应该立即返回，不阻塞（即使 pool 满了）
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = bus.Publish("test", "data")
	}()
	select {
	case <-done:
		// OK: 没有阻塞
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Publish blocked when worker pool was full")
	}
	// 清空 pool
	for range 100 {
		<-bus.workerPool
	}
}

// TestEventBus_Subscribe_IDUnique 验证修复 #6：ID 全局唯一，订阅-取消-再订阅不会重复
func TestEventBus_Subscribe_IDUnique(t *testing.T) {
	bus := NewEventBus()
	// 第一次订阅
	sub1, err := bus.Subscribe("topic", func(data any) {})
	require.NoError(t, err)
	// 取消订阅
	require.NoError(t, sub1.Unsubscribe())
	// 第二次订阅（同一 topic）
	sub2, err := bus.Subscribe("topic", func(data any) {})
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
	_, err := bus.Subscribe("count-topic", func(data any) {
		received.Add(1)
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
