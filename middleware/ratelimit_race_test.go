package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestRateLimitBucketRaceCondition 测试 Rate Limit Bucket 的并发竞态修复
func TestRateLimitBucketRaceCondition(t *testing.T) {
	// 创建限流中间件
	var accessCount atomic.Int32
	mw := RateLimitTokenBucket(10, 20, func(ctx *eventctx.Context) string {
		return string(ctx.GetEvent().ID)
	})

	// 创建 handler
	handler := mw(func(ctx *eventctx.Context) error {
		accessCount.Add(1)
		return nil
	})

	// 并发访问同一个 key
	concurrency := 100
	iterations := 10

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		go func(id int) {
			defer wg.Done()

			for j := range iterations {
				event := &dto.Payload{
					ID: "same-event-id", // 所有 goroutine 使用相同 ID
				}
				ctx := eventctx.NewContext(event, nil)

				// 调用 handler（可能被限流）
				_ = handler(ctx)

				// 短暂延迟增加竞态可能性
				if j%3 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Completed %d concurrent accesses, access count: %d", concurrency*iterations, accessCount.Load())
	t.Log("No race condition detected (test passed if no panic)")
}

// TestRateLimitBucketConcurrentKeys 测试多个不同 key 的并发访问
func TestRateLimitBucketConcurrentKeys(t *testing.T) {
	mw := RateLimitTokenBucket(100, 100, func(ctx *eventctx.Context) string {
		return string(ctx.GetEvent().ID)
	})

	handler := mw(func(ctx *eventctx.Context) error {
		return nil
	})

	concurrency := 50
	keysPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		go func(goroutineID int) {
			defer wg.Done()

			for j := range keysPerGoroutine {
				event := &dto.Payload{
					ID: dto.EventID(fmt.Sprintf("event-%d-%d", goroutineID, j)),
				}
				ctx := eventctx.NewContext(event, nil)
				_ = handler(ctx)
			}
		}(i)
	}

	wg.Wait()
	t.Log("Multiple concurrent keys handled without race condition")
}

// TestRateLimitBucketUpdateLastVisit 测试 lastVisit 更新的线程安全性
func TestRateLimitBucketUpdateLastVisit(t *testing.T) {
	mw := RateLimitTokenBucket(1000, 1000, func(ctx *eventctx.Context) string {
		return "shared-key"
	})

	handler := mw(func(ctx *eventctx.Context) error {
		return nil
	})

	// 快速连续访问，测试 lastVisit 更新
	concurrency := 100
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()

			event := &dto.Payload{ID: "test-event"}
			ctx := eventctx.NewContext(event, nil)
			_ = handler(ctx)
		}()
	}

	wg.Wait()
	t.Log("LastVisit updates completed without race condition")
}

// TestRateLimitBucketStressTest 压力测试
func TestRateLimitBucketStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	mw := RateLimitTokenBucket(50, 100, func(ctx *eventctx.Context) string {
		return string(ctx.GetEvent().ID)
	})

	handler := mw(func(ctx *eventctx.Context) error {
		// 模拟一些处理时间
		time.Sleep(time.Microsecond)
		return nil
	})

	// 持续 2 秒的压力测试
	done := make(chan struct{})
	time.AfterFunc(2*time.Second, func() { close(done) })

	var wg sync.WaitGroup
	workers := 20

	for i := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			counter := 0

			for {
				select {
				case <-done:
					return
				default:
					event := &dto.Payload{
						ID: dto.EventID(fmt.Sprintf("worker-%d-event-%d", workerID, counter%10)),
					}
					ctx := eventctx.NewContext(event, nil)
					_ = handler(ctx)
					counter++
				}
			}
		}(i)
	}

	wg.Wait()
	t.Log("Stress test completed without race condition or panic")
}

// TestRateLimitBucketCleanupDuringAccess 测试清理期间的并发访问
func TestRateLimitBucketCleanupDuringAccess(t *testing.T) {
	// 使用较小的 maxBuckets 和短 TTL 来触发清理
	config := RateLimitConfig{
		MaxBuckets:      10,
		BucketTTL:       100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	}

	mw := RateLimitTokenBucketWithConfig(config, 100, 100, func(ctx *eventctx.Context) string {
		return string(ctx.GetEvent().ID)
	})

	handler := mw(func(ctx *eventctx.Context) error {
		return nil
	})

	// 持续访问，触发清理
	done := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() { close(done) })

	var wg sync.WaitGroup
	wg.Add(10)

	for i := range 10 {
		go func(id int) {
			defer wg.Done()
			counter := 0

			for {
				select {
				case <-done:
					return
				default:
					event := &dto.Payload{
						ID: dto.EventID(fmt.Sprintf("event-%d-%d", id, counter)),
					}
					ctx := eventctx.NewContext(event, nil)
					_ = handler(ctx)
					counter++
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Log("Cleanup during access completed without race condition")
}
