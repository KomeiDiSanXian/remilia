package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	testutil "github.com/KomeiDiSanXian/remilia/middleware/testutil"
	"github.com/stretchr/testify/assert"
)

func TestNewAdaptiveRateLimiter(t *testing.T) {
	t.Run("default_config", func(t *testing.T) {
		config := DefaultAdaptiveConfig()
		limiter := NewAdaptiveRateLimiter(config)

		assert.NotNil(t, limiter)
		assert.Equal(t, int32(config.InitialLimit), limiter.maxConcurrency.Load())
	})

	t.Run("invalid_config_corrected", func(t *testing.T) {
		config := AdaptiveConfig{
			MinConcurrency: 0, // 无效，会被修正
			MaxConcurrency: 5, // 小于 min，会被修正
			InitialLimit:   0, // 无效，会被修正
		}

		limiter := NewAdaptiveRateLimiter(config)

		assert.GreaterOrEqual(t, limiter.config.MinConcurrency, 1)
		assert.Greater(t, limiter.config.MaxConcurrency, limiter.config.MinConcurrency)
		assert.GreaterOrEqual(t, limiter.maxConcurrency.Load(), int32(limiter.config.MinConcurrency))
	})
}

func TestAdaptiveRateLimiter_BasicFlow(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 5,
		MaxConcurrency: 50,
		InitialLimit:   10,
		AdjustInterval: 100 * time.Millisecond,
		MetricsEnabled: true,
	}

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	// 创建中间件
	middleware := limiter.Middleware()

	// 模拟处理函数
	var processedCount atomic.Int32
	handler := func(ctx *eventctx.Context) error {
		processedCount.Add(1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	wrappedHandler := middleware(handler)

	// 发送请求
	ctx := testutil.CreateTestContext()
	err := wrappedHandler(ctx)
	assert.NoError(t, err)

	assert.Equal(t, int32(1), processedCount.Load())
	assert.Equal(t, int64(1), limiter.totalRequests.Load())
}

func TestAdaptiveRateLimiter_ConcurrencyLimit(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 5,
		MaxConcurrency: 50,
		InitialLimit:   10,
		MetricsEnabled: false, // 关闭指标采集加快测试
	}

	limiter := NewAdaptiveRateLimiter(config)

	middleware := limiter.Middleware()

	// 阻塞的处理函数
	blockCh := make(chan struct{})
	handler := func(ctx *eventctx.Context) error {
		<-blockCh
		return nil
	}

	wrappedHandler := middleware(handler)

	// 启动 10 个请求（等于限制）
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			ctx := testutil.CreateTestContext()
			_ = wrappedHandler(ctx)
		})
	}

	// 等待所有 goroutine 启动并阻塞在 blockCh 上
	// 使用 channel 信号确保至少有一个请求持有令牌
	ready := make(chan struct{})
	go func() {
		for limiter.currentLoad.Load() < 10 {
			time.Sleep(time.Millisecond)
		}
		close(ready)
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for goroutines to acquire tokens")
	}

	// 尝试第 11 个请求（应该被拒绝）
	ctx := testutil.CreateTestContext()
	err := wrappedHandler(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrRateLimitExceeded)

	// 释放所有请求
	close(blockCh)
	wg.Wait()

	// 验证统计
	assert.Equal(t, int64(1), limiter.rejectedRequests.Load())
	assert.Equal(t, int64(11), limiter.totalRequests.Load())
}

func TestAdaptiveRateLimiter_DynamicAdjustment(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 100,
		InitialLimit:   50,
		AdjustInterval: 100 * time.Millisecond,
		AdjustStep:     10,
		CooldownPeriod: 50 * time.Millisecond,
		TargetCPU:      0.70,
		MetricsEnabled: true,
	}

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	initialLimit := limiter.maxConcurrency.Load()
	assert.Equal(t, int32(50), initialLimit)

	// 模拟高CPU使用率，直接调用决策函数
	limiter.cpuUsage.Store(0.90) // 90% CPU，超过目标70%

	// 直接执行限流调整（无需等待 adjustLoop ticker）
	newLimit := limiter.decideLimit(0.90, 0.5, 0, initialLimit)
	limiter.adjustLimit(newLimit)

	newLimit = limiter.maxConcurrency.Load()
	// 应该降低了限制
	assert.Less(t, newLimit, initialLimit)

	t.Logf("Limit adjusted from %d to %d due to high CPU", initialLimit, newLimit)
}

func TestAdaptiveRateLimiter_Stats(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.MetricsEnabled = false

	limiter := NewAdaptiveRateLimiter(config)

	middleware := limiter.Middleware()
	handler := func(ctx *eventctx.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}
	wrappedHandler := middleware(handler)

	// 发送一些请求
	for range 5 {
		ctx := testutil.CreateTestContext()
		_ = wrappedHandler(ctx)
	}

	stats := limiter.GetStats()

	assert.Equal(t, int64(5), stats.TotalRequests)
	assert.Equal(t, config.InitialLimit, int(stats.CurrentLimit))
	assert.GreaterOrEqual(t, stats.RejectionRate, 0.0)
}

func TestAdaptiveRateLimiter_HighLoad(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 50,
		InitialLimit:   20,
		MetricsEnabled: false,
	}

	limiter := NewAdaptiveRateLimiter(config)

	middleware := limiter.Middleware()

	var processed atomic.Int64
	var rejected atomic.Int64

	handler := func(ctx *eventctx.Context) error {
		processed.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	wrappedHandler := middleware(handler)

	// 发送大量并发请求
	var wg sync.WaitGroup
	requestCount := 100
	for range requestCount {
		wg.Go(func() {
			ctx := testutil.CreateTestContext()
			err := wrappedHandler(ctx)
			if err != nil {
				rejected.Add(1)
			}
		})
	}

	wg.Wait()

	// 验证
	t.Logf("Processed: %d, Rejected: %d, Total: %d",
		processed.Load(), rejected.Load(), requestCount)

	assert.Equal(t, int64(requestCount), limiter.totalRequests.Load())
	assert.Equal(t, rejected.Load(), limiter.rejectedRequests.Load())
	assert.Greater(t, rejected.Load(), int64(0), "Should reject some requests under high load")
}

func TestAdaptiveRateLimiter_StartStop(t *testing.T) {
	config := DefaultAdaptiveConfig()
	config.AdjustInterval = 50 * time.Millisecond

	limiter := NewAdaptiveRateLimiter(config)

	// 启动（验证 goroutine 启动成功：Stop 会阻塞直到它们退出）
	limiter.Start()

	// 停止（等待后台 goroutine 退出）
	limiter.Stop()

	// 再次停止应该安全
	limiter.Stop()
}

func TestAdaptiveRateLimiter_MetricsCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping adaptive rate limiter metrics collection test (6s observation window) in short mode")
	}

	config := DefaultAdaptiveConfig()
	config.MetricsEnabled = true

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	middleware := limiter.Middleware()
	handler := func(ctx *eventctx.Context) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	wrappedHandler := middleware(handler)

	// 发送请求
	for range 10 {
		ctx := testutil.CreateTestContext()
		_ = wrappedHandler(ctx)
	}

	// 等待指标采集
	time.Sleep(6 * time.Second)

	// 验证指标已更新
	cpu := limiter.getCPUUsage()
	memory := limiter.getMemoryUsage()

	t.Logf("CPU: %.2f%%, Memory: %.2f%%", cpu*100, memory*100)

	// 指标应该被更新
	assert.GreaterOrEqual(t, cpu, 0.0)
	assert.GreaterOrEqual(t, memory, 0.0)
}

func TestAdaptiveRateLimiter_DecideLimit(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 100,
		AdjustStep:     10,
		TargetCPU:      0.70,
		TargetMemory:   0.80,
		TargetLatency:  500 * time.Millisecond,
	}

	limiter := NewAdaptiveRateLimiter(config)

	tests := []struct {
		name         string
		cpu          float64
		memory       float64
		latency      time.Duration
		currentLimit int32
		expectHigher bool
		expectLower  bool
	}{
		{
			name:         "high_pressure_should_decrease",
			cpu:          0.90,
			memory:       0.85,
			latency:      800 * time.Millisecond,
			currentLimit: 50,
			expectLower:  true,
		},
		{
			name:         "low_pressure_should_increase",
			cpu:          0.30,
			memory:       0.40,
			latency:      200 * time.Millisecond,
			currentLimit: 50,
			expectHigher: true,
		},
		{
			name:         "moderate_pressure_should_maintain",
			cpu:          0.72,
			memory:       0.75,
			latency:      450 * time.Millisecond,
			currentLimit: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newLimit := limiter.decideLimit(tt.cpu, tt.memory, tt.latency, tt.currentLimit)

			if tt.expectHigher {
				assert.Greater(t, newLimit, tt.currentLimit, "Should increase limit")
			} else if tt.expectLower {
				assert.Less(t, newLimit, tt.currentLimit, "Should decrease limit")
			} else {
				// 允许小幅调整或保持不变
				diff := newLimit - tt.currentLimit
				assert.True(t, diff >= -int32(config.AdjustStep) && diff <= int32(config.AdjustStep),
					"Should maintain or slightly adjust limit")
			}

			// 验证在范围内
			assert.GreaterOrEqual(t, newLimit, int32(config.MinConcurrency))
			assert.LessOrEqual(t, newLimit, int32(config.MaxConcurrency))
		})
	}
}

func TestAdaptiveRateLimiter_AdjustLimit(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 100,
		InitialLimit:   50,
	}

	limiter := NewAdaptiveRateLimiter(config)

	// 初始限制
	assert.Equal(t, int32(50), limiter.maxConcurrency.Load())

	// 调整到30
	limiter.adjustLimit(30)
	assert.Equal(t, int32(30), limiter.maxConcurrency.Load())
	// 验证限流器仍然工作
	assert.Equal(t, int32(0), limiter.currentLoad.Load())

	// 调整到80
	limiter.adjustLimit(80)
	assert.Equal(t, int32(80), limiter.maxConcurrency.Load())
	// 验证限流器仍然工作
	assert.Equal(t, int32(0), limiter.currentLoad.Load())
}

// Benchmark tests

func BenchmarkAdaptiveRateLimiter_Middleware(b *testing.B) {
	config := AdaptiveConfig{
		MinConcurrency: 100,
		MaxConcurrency: 1000,
		InitialLimit:   500,
		MetricsEnabled: false,
	}

	limiter := NewAdaptiveRateLimiter(config)
	middleware := limiter.Middleware()

	handler := func(ctx *eventctx.Context) error {
		return nil
	}
	wrappedHandler := middleware(handler)

	ctx := testutil.CreateTestContext()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = wrappedHandler(ctx)
		}
	})
}

func BenchmarkAdaptiveRateLimiter_Stats(b *testing.B) {
	config := DefaultAdaptiveConfig()
	limiter := NewAdaptiveRateLimiter(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = limiter.GetStats()
	}
}
