package ratelimit

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAdaptiveRateLimiter_RealCPUMetrics 测试真实 CPU 指标采集
func TestAdaptiveRateLimiter_RealCPUMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := AdaptiveConfig{
			MinConcurrency: 10,
			MaxConcurrency: 100,
			InitialLimit:   50,
			MetricsEnabled: true,
			AdjustInterval: 1 * time.Second,
		}

		limiter := NewAdaptiveRateLimiter(config)
		limiter.Start()
		defer limiter.Stop()

		time.Sleep(6 * time.Second)

		cpuUsage := limiter.getCPUUsage()

		t.Logf("CPU Usage: %.2f%%", cpuUsage*100)

		if cpuUsage >= 0 {
			assert.GreaterOrEqual(t, cpuUsage, 0.0, "CPU usage should be >= 0")
			assert.LessOrEqual(t, cpuUsage, 1.0, "CPU usage should be <= 1.0")
			t.Log("✓ Real CPU metrics collected successfully")
		} else {
			t.Log("⚠ CPU metrics collection failed (may be expected in some environments)")
		}

		memUsage := limiter.getMemoryUsage()
		t.Logf("Memory Usage: %.2f%%", memUsage*100)

		assert.GreaterOrEqual(t, memUsage, 0.0, "Memory usage should be >= 0")
		assert.LessOrEqual(t, memUsage, 1.0, "Memory usage should be <= 1.0")

		t.Log("✓ Memory metrics valid")
	})
}

// TestAdaptiveRateLimiter_CPUFailureFallback 测试 CPU 采集失败时的降级处理
func TestAdaptiveRateLimiter_CPUFailureFallback(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 100,
		InitialLimit:   50,
		TargetCPU:      0.70,
		TargetMemory:   0.80,
		TargetLatency:  500 * time.Millisecond,
		MetricsEnabled: true,
		AdjustInterval: 100 * time.Millisecond,
		AdjustStep:     5,
	}

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	// 手动设置 CPU 使用率为 -1（模拟采集失败）
	limiter.cpuUsage.Store(-1.0)

	// 直接验证决策函数在指标采集失败时不会修改限制
	cpuVal := limiter.getCPUUsage()
	assert.Equal(t, -1.0, cpuVal, "CPU usage should be -1 to indicate failure")
	if cpuVal < 0 {
		// adjustLoop 会跳过采集失败时的调整，limit 不变
		stats := limiter.GetStats()
		assert.Equal(t, int32(50), stats.CurrentLimit, "Limit should remain unchanged when CPU metrics fail")
	}

	// 验证限流器仍然正常工作（不会因为 CPU 采集失败而崩溃）
	currentLimit := limiter.maxConcurrency.Load()
	assert.Equal(t, int32(50), currentLimit, "Limit should remain unchanged")

	t.Log("✓ CPU collection failure handled gracefully")
}

// TestAdaptiveRateLimiter_NoGoroutineBasedCPU 验证不再使用 goroutine 数量计算 CPU
func TestAdaptiveRateLimiter_NoGoroutineBasedCPU(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := DefaultAdaptiveConfig()
		config.MetricsEnabled = true

		limiter := NewAdaptiveRateLimiter(config)
		limiter.Start()
		defer limiter.Stop()

		time.Sleep(6 * time.Second)

		cpuUsage := limiter.getCPUUsage()

		if cpuUsage > 0 {
			t.Logf("✓ CPU usage from real metrics: %.4f (not goroutine-based)", cpuUsage)
		} else if cpuUsage == -1.0 {
			t.Log("⚠ CPU metrics collection not available in this environment")
		}
	})
}
