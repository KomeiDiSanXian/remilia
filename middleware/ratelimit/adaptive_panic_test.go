package ratelimit

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	testutil "github.com/KomeiDiSanXian/remilia/middleware/testutil"
)

// TestAdaptiveRateLimiter_PanicRecovery 测试 handler panic 时信号量正确释放
func TestAdaptiveRateLimiter_PanicRecovery(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.InitialLimit = 5
	config.MinConcurrency = 5
	config.MaxConcurrency = 10
	config.MetricsEnabled = false

	arl := NewAdaptiveRateLimiter(config)
	defer arl.Stop()

	mw := arl.Middleware()

	// 创建一个会 panic 的 handler
	panicHandler := func(ctx *eventctx.Context) error {
		panic("test panic")
	}

	// 包装 handler
	wrappedHandler := mw(panicHandler)

	// 创建测试 context
	ctx := testutil.CreateTestContext()

	// 执行多次，确保每次都能正确释放信号量
	for range 10 {
		err := wrappedHandler(ctx)
		if err == nil {
			t.Fatalf("Expected error from panic, got nil")
		}
		if err.Error() != "panic in handler: test panic" {
			t.Errorf("Expected panic error, got: %v", err)
		}
	}

	// 验证所有令牌都已归还（currentLoad 应该为 0）
	load := arl.currentLoad.Load()
	if load != 0 {
		t.Errorf("Expected currentLoad=0 after all calls, got: %d", load)
	}

	// 验证限流器仍然可以正常工作
	testCtx := testutil.CreateTestContext()

	err := mw(func(ctx *eventctx.Context) error {
		return nil
	})(testCtx)

	if err != nil {
		t.Errorf("Limiter should still work after panic recovery, got error: %v", err)
	}

	// 再次验证负载为 0
	if arl.currentLoad.Load() != 0 {
		t.Errorf("Expected currentLoad=0 after verification test, got: %d", arl.currentLoad.Load())
	}

}

// TestAdaptiveRateLimiter_ConcurrentPanicRecovery 测试并发场景下的 panic 恢复
func TestAdaptiveRateLimiter_ConcurrentPanicRecovery(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.InitialLimit = 10
	config.MinConcurrency = 10
	config.MaxConcurrency = 20
	config.MetricsEnabled = false

	arl := NewAdaptiveRateLimiter(config)
	defer arl.Stop()

	mw := arl.Middleware()

	// 创建一个会随机 panic 的 handler
	counter := atomic.Int32{}
	handler := func(ctx *eventctx.Context) error {
		c := counter.Add(1)
		if c%3 == 0 {
			panic(fmt.Sprintf("test panic %d", c))
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	wrappedHandler := mw(handler)

	// 并发执行
	var wg sync.WaitGroup
	numGoroutines := 50
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			ctx := testutil.CreateTestContext()
			_ = wrappedHandler(ctx)
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(200 * time.Millisecond)

	// 验证信号量没有泄漏
	load := arl.currentLoad.Load()
	if load != 0 {
		t.Errorf("Expected currentLoad=0 after all concurrent calls, got: %d", load)
	}

	// 验证统计数据
	total := arl.totalRequests.Load()
	if total != int64(numGoroutines) {
		t.Errorf("Expected %d total requests, got: %d", numGoroutines, total)
	}

	t.Logf("Processed %d requests successfully with panic recovery", total)
}

// TestAdaptiveRateLimiter_SemaphoreNoLeak 测试正常情况下信号量不泄漏
func TestAdaptiveRateLimiter_SemaphoreNoLeak(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.InitialLimit = 3
	config.MinConcurrency = 3
	config.MaxConcurrency = 10
	config.MetricsEnabled = false

	arl := NewAdaptiveRateLimiter(config)
	defer arl.Stop()

	mw := arl.Middleware()

	// 正常 handler
	handler := func(ctx *eventctx.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	wrappedHandler := mw(handler)

	// 启动多个 goroutine，超过信号量限制
	var wg sync.WaitGroup
	numGoroutines := 20
	wg.Add(numGoroutines)

	startTime := time.Now()

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			ctx := testutil.CreateTestContext()
			_ = wrappedHandler(ctx)
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// 验证信号量没有泄漏
	load := arl.currentLoad.Load()
	if load != 0 {
		t.Errorf("Expected currentLoad=0 after all calls, got: %d", load)
	}

	// 验证统计数据
	total := arl.totalRequests.Load()
	rejected := arl.rejectedRequests.Load()

	t.Logf("Total requests: %d, Rejected: %d, Duration: %v", total, rejected, duration)

	// 由于限流，应该有部分请求被拒绝
	if rejected == 0 {
		t.Logf("Warning: No requests were rejected, rate limit might not be working properly")
	}

	// 验证 total = 成功 + 拒绝
	if total != int64(numGoroutines) {
		t.Errorf("Expected %d total requests, got: %d", numGoroutines, total)
	}
}

// TestAdaptiveRateLimiter_ErrorPropagation 测试错误正确传播
func TestAdaptiveRateLimiter_ErrorPropagation(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.InitialLimit = 5
	config.MetricsEnabled = false

	arl := NewAdaptiveRateLimiter(config)
	defer arl.Stop()

	mw := arl.Middleware()

	// 返回错误的 handler
	expectedErr := fmt.Errorf("test error")
	handler := func(ctx *eventctx.Context) error {
		return expectedErr
	}

	wrappedHandler := mw(handler)

	ctx := testutil.CreateTestContext()

	err := wrappedHandler(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected error %v, got: %v", expectedErr, err)
	}

	// 验证信号量正确释放
	load := arl.currentLoad.Load()
	if load != 0 {
		t.Errorf("Expected currentLoad=0 after error, got: %d", load)
	}
}
