package middleware

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestNewDedupFilter 测试创建去重过滤器
func TestNewDedupFilter(t *testing.T) {
	config := DefaultDedupConfig()
	filter := NewDedupFilter(config)
	defer filter.Stop()

	assert.NotNil(t, filter)
	assert.Equal(t, 10000, filter.maxSize)
	assert.Equal(t, 5*time.Minute, filter.defaultTTL)
}

// TestDedupFilterBasic 测试基本去重功能
func TestDedupFilterBasic(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         100,
		DefaultTTL:      time.Second,
		CleanupInterval: 100 * time.Millisecond,
	})
	defer filter.Stop()

	// 第一次检查，应该不重复
	isDup, err := filter.IsDuplicate("event-1")
	assert.NoError(t, err)
	assert.False(t, isDup, "first check should not be duplicate")

	// 第二次检查，应该重复
	isDup, err = filter.IsDuplicate("event-1")
	assert.NoError(t, err)
	assert.True(t, isDup, "second check should be duplicate")

	// 不同事件，不重复
	isDup, err = filter.IsDuplicate("event-2")
	assert.NoError(t, err)
	assert.False(t, isDup, "different event should not be duplicate")
}

// TestDedupFilterMaxSize 测试缓存大小限制
func TestDedupFilterMaxSize(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         3,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	})
	defer filter.Stop()

	// 添加 3 个事件，应该成功
	for i := 0; i < 3; i++ {
		eventID := string(rune('a' + i))
		isDup, err := filter.IsDuplicate(eventID)
		assert.NoError(t, err)
		assert.False(t, isDup)
	}

	// 第 4 个事件，应该返回错误（缓存满）
	isDup, err := filter.IsDuplicate("d")
	assert.Error(t, err)
	assert.False(t, isDup)
	assert.Contains(t, err.Error(), "cache full")
}

// TestDedupFilterCleanup 测试后台清理
func TestDedupFilterCleanup(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         100,
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	})
	defer filter.Stop()

	// 添加多个事件
	for i := 0; i < 5; i++ {
		eventID := string(rune('a' + i))
		filter.IsDuplicate(eventID)
	}

	stats := filter.GetStats()
	assert.Equal(t, 5, stats["cache_size"])

	// 等待过期和清理
	time.Sleep(150 * time.Millisecond)

	stats = filter.GetStats()
	// 过期条目应该被清理
	assert.Equal(t, 0, stats["cache_size"], "expired entries should be cleaned")
}

// TestDedupFilterClear 测试清空缓存
func TestDedupFilterClear(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	// 添加事件
	filter.IsDuplicate("event-1")
	filter.IsDuplicate("event-2")

	stats := filter.GetStats()
	assert.Equal(t, 2, stats["cache_size"])

	// 清空缓存
	filter.Clear()

	stats = filter.GetStats()
	assert.Equal(t, 0, stats["cache_size"])

	// 清空后，相同事件应该不重复
	isDup, _ := filter.IsDuplicate("event-1")
	assert.False(t, isDup)
}

// TestDedupMiddleware 测试去重中间件
func TestDedupMiddleware(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	var executionCount int
	handler := func(ctx *remilia.Context) error {
		executionCount++
		return nil
	}

	// 创建中间件
	mw := Dedup(filter)
	wrappedHandler := mw(handler)

	// 第一次执行，应该成功
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-1",
	}
	ctx := remilia.NewContext(event, nil)
	err := wrappedHandler(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)

	// 第二次执行相同事件，应该被阻断
	ctx2 := remilia.NewContext(event, nil)
	err = wrappedHandler(ctx2)
	assert.NoError(t, err) // 去重不返回错误
	assert.Equal(t, 1, executionCount, "handler should not be called for duplicate event")

	// 不同事件，应该执行
	event2 := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-2",
	}
	ctx3 := remilia.NewContext(event2, nil)
	err = wrappedHandler(ctx3)
	assert.NoError(t, err)
	assert.Equal(t, 2, executionCount)
}

// TestDedupMiddlewareNoEventID 测试没有 eventID 的情况
func TestDedupMiddlewareNoEventID(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	var executed bool
	handler := func(ctx *remilia.Context) error {
		executed = true
		return nil
	}

	mw := Dedup(filter)
	wrappedHandler := mw(handler)

	// 没有 eventID 的事件，应该直接执行
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "",
	}
	ctx := remilia.NewContext(event, nil)
	err := wrappedHandler(ctx)
	assert.NoError(t, err)
	assert.True(t, executed)
}

// TestDedupMiddlewareCacheFull 测试缓存满的情况
func TestDedupMiddlewareCacheFull(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         2,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	})
	defer filter.Stop()

	var executionCount int
	handler := func(ctx *remilia.Context) error {
		executionCount++
		return nil
	}

	mw := Dedup(filter)
	wrappedHandler := mw(handler)

	// 填满缓存
	for i := 0; i < 2; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("event-" + string(rune(i))),
		}
		ctx := remilia.NewContext(event, nil)
		wrappedHandler(ctx)
	}

	assert.Equal(t, 2, executionCount)

	// 缓存满时，应该继续处理（记录警告）
	event3 := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "event-3",
	}
	ctx3 := remilia.NewContext(event3, nil)
	err := wrappedHandler(ctx3)
	assert.NoError(t, err)
	assert.Equal(t, 3, executionCount, "should continue processing when cache is full")
}

// TestDedupWithReject 测试严格的去重中间件
func TestDedupWithReject(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         2,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	})
	defer filter.Stop()

	var executionCount int
	handler := func(ctx *remilia.Context) error {
		executionCount++
		return nil
	}

	mw := DedupWithReject(filter)
	wrappedHandler := mw(handler)

	// 填满缓存
	for i := 0; i < 2; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("event-" + string(rune(i))),
		}
		ctx := remilia.NewContext(event, nil)
		wrappedHandler(ctx)
	}

	assert.Equal(t, 2, executionCount)

	// 缓存满时，应该返回错误
	event3 := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "event-3",
	}
	ctx3 := remilia.NewContext(event3, nil)
	err := wrappedHandler(ctx3)
	assert.Error(t, err, "should return error when cache is full with DedupWithReject")
	assert.Equal(t, 2, executionCount, "handler should not be called when cache is full")
}

// TestDedupFilterConcurrent 测试并发安全
func TestDedupFilterConcurrent(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	})
	defer filter.Stop()

	done := make(chan bool)

	// 启动多个 goroutine 并发访问
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 100; j++ {
				eventID := string(rune('a'+id)) + string(rune('0'+j))
				filter.IsDuplicate(eventID)
			}
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证没有崩溃
	stats := filter.GetStats()
	assert.Greater(t, stats["cache_size"].(int), 0)
}

// BenchmarkDedupFilter 基准测试去重性能
func BenchmarkDedupFilter(b *testing.B) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := string(rune(i % 1000))
		filter.IsDuplicate(eventID)
	}
}

// BenchmarkDedupMiddleware 基准测试中间件性能
func BenchmarkDedupMiddleware(b *testing.B) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	handler := func(ctx *remilia.Context) error {
		return nil
	}

	mw := Dedup(filter)
	wrappedHandler := mw(handler)

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "bench-event",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := remilia.NewContext(event, nil)
		wrappedHandler(ctx)
	}
}
