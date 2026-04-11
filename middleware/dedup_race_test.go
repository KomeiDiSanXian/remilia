package middleware

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestDedupRaceConditionFix 验证 Bug #2 的修复：缓存满时的竞态条件
// 这个测试专门针对高并发场景下，多个 goroutine 同时检查和添加缓存的情况
//
// 注意：此测试会产生大量 WRN [Dedup] Cache still full after cleanup 日志，
// 这是预期行为——缓存满且无过期条目时触发 Warn 日志。这些日志不代表测试失败
func TestDedupRaceConditionFix(t *testing.T) {
	// 创建一个小缓存，容易触发满载情况
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         10,
		DefaultTTL:      5 * time.Second,
		CleanupInterval: 1 * time.Minute, // 禁用自动清理
	})
	defer filter.Stop()

	// 使用 100 个并发 goroutine，每个尝试添加 100 个事件
	concurrency := 100
	eventsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(concurrency)

	// 统计错误数量
	var cacheFullErrors int32
	var mu sync.Mutex

	for i := range concurrency {
		go func(goroutineID int) {
			defer wg.Done()

			for j := range eventsPerGoroutine {
				eventID := fmt.Sprintf("event-%d-%d", goroutineID, j)
				_, err := filter.CheckDuplicate(eventID)
				if err != nil {
					mu.Lock()
					cacheFullErrors++
					mu.Unlock()
				}

				// 稍微延迟，增加竞态的可能性
				if j%10 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	wg.Wait()

	// 验证：
	// 1. 程序没有 panic
	// 2. 缓存大小不应该超过 maxSize（这是关键验证点）
	stats := filter.GetStats()
	cacheSize := stats["cache_size"].(int)

	t.Logf("Final cache size: %d, max size: 10", cacheSize)
	t.Logf("Cache full errors: %d", cacheFullErrors)

	if cacheSize > 10 {
		t.Errorf("Cache size %d exceeds maxSize 10 - race condition not fixed!", cacheSize)
	}

	// 验证缓存大小合理（应该正好是 10 或接近 10）
	if cacheSize < 8 || cacheSize > 10 {
		t.Errorf("Cache size %d is unexpected (expected 8-10)", cacheSize)
	}
}

// TestDedupConcurrentAddAndClean 测试并发添加和清理的场景
func TestDedupConcurrentAddAndClean(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         100,
		DefaultTTL:      100 * time.Millisecond, // 短 TTL
		CleanupInterval: 50 * time.Millisecond,  // 频繁清理
	})
	defer filter.Stop()

	// 持续 1 秒的并发测试
	done := make(chan struct{})
	time.AfterFunc(1*time.Second, func() { close(done) })

	var wg sync.WaitGroup

	// 10 个并发添加者
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			counter := 0
			for {
				select {
				case <-done:
					return
				default:
					eventID := fmt.Sprintf("worker-%d-event-%d", id, counter)
					filter.CheckDuplicate(eventID)
					counter++
				}
			}
		}(i)
	}

	wg.Wait()

	// 验证程序没有 panic，缓存大小合理
	stats := filter.GetStats()
	cacheSize := stats["cache_size"].(int)

	t.Logf("Final cache size: %d", cacheSize)

	if cacheSize > 100 {
		t.Errorf("Cache size %d exceeds maxSize 100!", cacheSize)
	}
}

// TestDedupAtomicOperation 验证 check-clean-add 操作的原子性
func TestDedupAtomicOperation(t *testing.T) {
	filter := NewDedupFilter(DedupConfig{
		MaxSize:         5,
		DefaultTTL:      1 * time.Hour, // 长 TTL，不会自动过期
		CleanupInterval: 1 * time.Hour, // 禁用自动清理
	})
	defer filter.Stop()

	// 先填满缓存
	for i := range 5 {
		_, err := filter.CheckDuplicate(fmt.Sprintf("initial-%d", i))
		if err != nil {
			t.Fatalf("Failed to add initial event: %v", err)
		}
	}

	// 现在缓存已满，尝试并发添加新事件
	// 这应该触发 cache full 错误，而不是让缓存超过限制
	concurrency := 50
	var wg sync.WaitGroup
	wg.Add(concurrency)

	errors := make([]error, concurrency)

	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			_, err := filter.CheckDuplicate(fmt.Sprintf("concurrent-%d", idx))
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// 验证缓存大小仍然是 5
	stats := filter.GetStats()
	cacheSize := stats["cache_size"].(int)

	if cacheSize != 5 {
		t.Errorf("Cache size should be 5, got %d - atomic operation failed!", cacheSize)
	}

	// 验证大部分请求都得到了 cache full 错误
	errorCount := 0
	for _, err := range errors {
		if err != nil {
			errorCount++
		}
	}

	if errorCount < concurrency-1 {
		t.Errorf("Expected at least %d cache full errors, got %d", concurrency-1, errorCount)
	}

	t.Logf("Cache full errors: %d/%d (as expected)", errorCount, concurrency)
}
