package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestDedupFilter_Basic 测试基本去重功能
func TestDedupFilter_Basic(t *testing.T) {
	t.Run("first_event_not_duplicate", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		isDup, err := filter.CheckDuplicate("event-1")

		assert.NoError(t, err)
		assert.False(t, isDup, "First event should not be duplicate")
	})

	t.Run("second_same_event_is_duplicate", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		_, _ = filter.CheckDuplicate("event-1")
		isDup, err := filter.CheckDuplicate("event-1")

		assert.NoError(t, err)
		assert.True(t, isDup, "Second same event should be duplicate")
	})

	t.Run("different_events_not_duplicate", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		_, _ = filter.CheckDuplicate("event-1")
		isDup, err := filter.CheckDuplicate("event-2")

		assert.NoError(t, err)
		assert.False(t, isDup, "Different events should not be duplicate")
	})
}

// TestDedupFilter_Expiration 测试过期处理
func TestDedupFilter_Expiration(t *testing.T) {
	t.Run("expired_event_not_duplicate", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         100,
			DefaultTTL:      100 * time.Millisecond,
			CleanupInterval: 50 * time.Millisecond,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		// 添加事件
		isDup, err := filter.CheckDuplicate("event-1")
		assert.NoError(t, err)
		assert.False(t, isDup)

		// 等待过期
		time.Sleep(150 * time.Millisecond)

		// 再次检查，应该不是重复（已过期）
		isDup, err = filter.CheckDuplicate("event-1")
		assert.NoError(t, err)
		assert.False(t, isDup, "Expired event should not be duplicate")
	})

	t.Run("cleanup_removes_expired", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         100,
			DefaultTTL:      50 * time.Millisecond,
			CleanupInterval: 100 * time.Millisecond,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		// 添加多个事件
		for i := range 10 {
			_, _ = filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
		}

		stats := filter.GetStats()
		assert.Equal(t, 10, stats["cache_size"].(int), "Should have 10 entries")

		// 等待清理
		time.Sleep(200 * time.Millisecond)

		stats = filter.GetStats()
		assert.Equal(t, 0, stats["cache_size"].(int), "All entries should be cleaned")
	})
}

// TestDedupFilter_CacheFull 测试缓存满载处理（关键测试）
func TestDedupFilter_CacheFull(t *testing.T) {
	t.Run("immediate_cleanup_on_full", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         5,
			DefaultTTL:      50 * time.Millisecond, // 短 TTL
			CleanupInterval: 10 * time.Second,      // 长清理间隔，不依赖后台清理
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		// 填满缓存
		for i := range 5 {
			isDup, err := filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
			assert.NoError(t, err)
			assert.False(t, isDup)
		}

		// 等待过期
		time.Sleep(100 * time.Millisecond)

		// 现在缓存满但都已过期，添加新事件应该触发立即清理
		isDup, err := filter.CheckDuplicate("new-event")
		assert.NoError(t, err, "Should succeed after immediate cleanup")
		assert.False(t, isDup)

		// 验证缓存中只有新事件
		stats := filter.GetStats()
		assert.Equal(t, 1, stats["cache_size"].(int), "Should have only new event after cleanup")
	})

	t.Run("error_when_full_and_no_expired", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         3,
			DefaultTTL:      10 * time.Second, // 长 TTL
			CleanupInterval: 10 * time.Second,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		// 填满缓存
		for i := range 3 {
			_, err := filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
			assert.NoError(t, err)
		}

		// 立即添加新事件，应该返回错误（无过期条目可清理）
		isDup, err := filter.CheckDuplicate("new-event")
		assert.Error(t, err, "Should return error when cache full")
		assert.False(t, isDup)
		assert.Contains(t, err.Error(), "full after cleanup")
	})

	t.Run("partial_cleanup_allows_new_entry", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         5,
			DefaultTTL:      100 * time.Millisecond,
			CleanupInterval: 10 * time.Second,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		// 添加3个事件
		for i := range 3 {
			_, _ = filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
		}

		// 等待50ms
		time.Sleep(50 * time.Millisecond)

		// 再添加2个事件，缓存满
		for i := 3; i < 5; i++ {
			_, _ = filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
		}

		// 再等待60ms，前3个过期
		time.Sleep(60 * time.Millisecond)

		// 添加新事件，应该触发清理并成功
		isDup, err := filter.CheckDuplicate("new-event")
		assert.NoError(t, err, "Should succeed after partial cleanup")
		assert.False(t, isDup)

		stats := filter.GetStats()
		assert.LessOrEqual(t, stats["cache_size"].(int), 3, "Should have at most 3 entries after cleanup")
	})
}

// TestDedupFilter_Concurrent 测试并发安全
func TestDedupFilter_Concurrent(t *testing.T) {
	t.Run("concurrent_dedup_checks", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		const concurrency = 100
		const eventsPerGoroutine = 10

		var wg sync.WaitGroup
		duplicates := make([]atomic.Int32, eventsPerGoroutine)

		// 并发添加相同的事件
		for range concurrency {
			wg.Go(func() {
				for j := range eventsPerGoroutine {
					eventID := fmt.Sprintf("event-%d", j)
					isDup, err := filter.CheckDuplicate(eventID)
					if err == nil && isDup {
						duplicates[j].Add(1)
					}
				}
			})
		}

		wg.Wait()

		// 每个事件应该只有一个首次出现，其他都是重复
		for j := range eventsPerGoroutine {
			dupCount := duplicates[j].Load()
			t.Logf("Event %d: %d duplicates out of %d", j, dupCount, concurrency)
			assert.Greater(t, dupCount, int32(0), "Should have some duplicates")
		}
	})

	t.Run("concurrent_with_cleanup", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         50,
			DefaultTTL:      100 * time.Millisecond,
			CleanupInterval: 50 * time.Millisecond,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		const duration = 500 * time.Millisecond
		const goroutines = 10

		var wg sync.WaitGroup
		stopCh := make(chan struct{})

		// 启动多个 goroutine 持续添加事件
		for i := range goroutines {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				counter := 0
				for {
					select {
					case <-stopCh:
						return
					default:
						eventID := fmt.Sprintf("g%d-event-%d", id, counter)
						_, _ = filter.CheckDuplicate(eventID)
						counter++
						time.Sleep(10 * time.Millisecond)
					}
				}
			}(i)
		}

		// 运行一段时间
		time.Sleep(duration)
		close(stopCh)
		wg.Wait()

		// 验证没有崩溃，缓存大小合理
		stats := filter.GetStats()
		cacheSize := stats["cache_size"].(int)
		t.Logf("Final cache size: %d", cacheSize)
		assert.LessOrEqual(t, cacheSize, config.MaxSize, "Cache size should not exceed max")
	})
}

// TestDedupFilter_Clear 测试清空缓存
func TestDedupFilter_Clear(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	// 添加事件
	for i := range 5 {
		_, _ = filter.CheckDuplicate(fmt.Sprintf("event-%d", i))
	}

	stats := filter.GetStats()
	assert.Equal(t, 5, stats["cache_size"].(int))

	// 清空
	filter.Clear()

	stats = filter.GetStats()
	assert.Equal(t, 0, stats["cache_size"].(int))

	// 之前的事件现在不是重复
	isDup, err := filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.False(t, isDup)
}

// TestDedupFilter_Stop 测试停止清理器
func TestDedupFilter_Stop(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())

	// 添加事件
	_, _ = filter.CheckDuplicate("event-1")

	// 停止清理器
	filter.Stop()

	// 应该仍然可以检查重复（只是后台清理停止）
	isDup, err := filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.True(t, isDup)
}

// TestDedupMiddleware 测试去重中间件
func TestDedupMiddleware(t *testing.T) {
	t.Run("blocks_duplicate_events", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		var handlerCalled atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			handlerCalled.Add(1)
			return nil
		}

		mw := Dedup(filter)
		wrappedHandler := mw(handler)

		event := &dto.Payload{
			ID:   "event-1",
			Type: "test",
		}

		// 第一次调用
		ctx1 := eventctx.NewContext(event, nil)
		err := wrappedHandler(ctx1)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), handlerCalled.Load())

		// 第二次调用（重复）
		ctx2 := eventctx.NewContext(event, nil)
		err = wrappedHandler(ctx2)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), handlerCalled.Load(), "Handler should not be called for duplicate")
	})

	t.Run("allows_different_events", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		var handlerCalled atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			handlerCalled.Add(1)
			return nil
		}

		mw := Dedup(filter)
		wrappedHandler := mw(handler)

		// 不同的事件
		event1 := &dto.Payload{ID: "event-1", Type: "test"}
		event2 := &dto.Payload{ID: "event-2", Type: "test"}

		ctx1 := eventctx.NewContext(event1, nil)
		err := wrappedHandler(ctx1)
		assert.NoError(t, err)

		ctx2 := eventctx.NewContext(event2, nil)
		err = wrappedHandler(ctx2)
		assert.NoError(t, err)

		assert.Equal(t, int32(2), handlerCalled.Load(), "Both events should be processed")
	})

	t.Run("continues_on_cache_full", func(t *testing.T) {
		config := DedupConfig{
			MaxSize:         2,
			DefaultTTL:      10 * time.Second,
			CleanupInterval: 10 * time.Second,
		}
		filter := NewDedupFilter(config)
		defer filter.Stop()

		var handlerCalled atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			handlerCalled.Add(1)
			return nil
		}

		mw := Dedup(filter)
		wrappedHandler := mw(handler)

		// 填满缓存
		for i := range 2 {
			event := &dto.Payload{
				ID:   dto.EventID(fmt.Sprintf("event-%d", i)),
				Type: "test",
			}
			ctx := eventctx.NewContext(event, nil)
			_ = wrappedHandler(ctx)
		}

		// 添加第3个事件（缓存满）
		event3 := &dto.Payload{ID: "event-3", Type: "test"}
		ctx3 := eventctx.NewContext(event3, nil)
		err := wrappedHandler(ctx3)

		// 应该继续处理（带警告）
		assert.NoError(t, err)
		assert.Equal(t, int32(3), handlerCalled.Load(), "Should continue processing even when cache full")
	})

	t.Run("skips_events_without_id", func(t *testing.T) {
		filter := NewDedupFilter(DefaultDedupConfig())
		defer filter.Stop()

		var handlerCalled atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			handlerCalled.Add(1)
			return nil
		}

		mw := Dedup(filter)
		wrappedHandler := mw(handler)

		// 没有 ID 的事件
		event := &dto.Payload{ID: "", Type: "test"}
		ctx := eventctx.NewContext(event, nil)
		err := wrappedHandler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int32(1), handlerCalled.Load(), "Should process events without ID")
	})
}

// TestDedupFilter_GetStats 测试统计信息
func TestDedupFilter_GetStats(t *testing.T) {
	config := DedupConfig{
		MaxSize:         100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
	filter := NewDedupFilter(config)
	defer filter.Stop()

	stats := filter.GetStats()

	assert.Equal(t, 0, stats["cache_size"].(int))
	assert.Equal(t, 100, stats["max_size"].(int))
	assert.Equal(t, "5m0s", stats["ttl"].(string))

	// 添加事件后
	_, _ = filter.CheckDuplicate("event-1")
	_, _ = filter.CheckDuplicate("event-2")

	stats = filter.GetStats()
	assert.Equal(t, 2, stats["cache_size"].(int))
}

// TestDefaultDedupConfig 测试默认配置
func TestDefaultDedupConfig(t *testing.T) {
	config := DefaultDedupConfig()

	assert.Equal(t, 10000, config.MaxSize)
	assert.Equal(t, 5*time.Minute, config.DefaultTTL)
	assert.Equal(t, 1*time.Minute, config.CleanupInterval)
}

// TestDedupFilter_DoubleStop tests that calling Stop() twice doesn't panic
func TestDedupFilter_DoubleStop(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())

	// First stop should work
	assert.NotPanics(t, func() {
		filter.Stop()
	}, "First Stop() should not panic")

	// Second stop should also not panic
	assert.NotPanics(t, func() {
		filter.Stop()
	}, "Second Stop() should not panic")

	// Third stop to be sure
	assert.NotPanics(t, func() {
		filter.Stop()
	}, "Third Stop() should not panic")
}

// TestDedupFilter_SubSecondTTL tests that TTL below 1 second works correctly
func TestDedupFilter_SubSecondTTL(t *testing.T) {
	config := DedupConfig{
		MaxSize:         100,
		DefaultTTL:      100 * time.Millisecond, // Sub-second TTL
		CleanupInterval: 50 * time.Millisecond,
	}
	filter := NewDedupFilter(config)
	defer filter.Stop()

	// Add event
	isDup, err := filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.False(t, isDup, "First check should not be duplicate")

	// Immediately check again - should be duplicate
	time.Sleep(1 * time.Millisecond) // tiny sleep to ensure consistent behavior
	isDup, err = filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.True(t, isDup, "Second check should be duplicate")

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Check again - should not be duplicate (expired)
	isDup, err = filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.False(t, isDup, "After expiration should not be duplicate")
}
