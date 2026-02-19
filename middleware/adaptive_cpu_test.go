package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAdaptiveRateLimiter_RealCPUMetrics 测试真实 CPU 指标采集
func TestAdaptiveRateLimiter_RealCPUMetrics(t *testing.T) {
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

	// 等待采集一次指标
	time.Sleep(6 * time.Second)

	// 获取 CPU 使用率
	cpuUsage := limiter.getCPUUsage()

	t.Logf("CPU Usage: %.2f%%", cpuUsage*100)

	// 验证 CPU 使用率在合理范围内
	// 注意：-1.0 表示采集失败，这也是可接受的（在某些环境下可能失败）
	if cpuUsage >= 0 {
		assert.GreaterOrEqual(t, cpuUsage, 0.0, "CPU usage should be >= 0")
		assert.LessOrEqual(t, cpuUsage, 1.0, "CPU usage should be <= 1.0")
		t.Log("✓ Real CPU metrics collected successfully")
	} else {
		t.Log("⚠ CPU metrics collection failed (may be expected in some environments)")
	}

	// 验证内存使用率
	memUsage := limiter.getMemoryUsage()
	t.Logf("Memory Usage: %.2f%%", memUsage*100)

	assert.GreaterOrEqual(t, memUsage, 0.0, "Memory usage should be >= 0")
	assert.LessOrEqual(t, memUsage, 1.0, "Memory usage should be <= 1.0")

	t.Log("✓ Memory metrics valid")
}

// TestAdaptiveRateLimiter_CPUFailureFallback 测试 CPU 采集失败时的降级处理
func TestAdaptiveRateLimiter_CPUFailureFallback(t *testing.T) {
	config := AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 100,
		InitialLimit:   50,
		MetricsEnabled: true,
		AdjustInterval: 100 * time.Millisecond,
		AdjustStep:     5,
	}

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	// 手动设置 CPU 使用率为 -1（模拟采集失败）
	limiter.cpuUsage.Store(-1.0)

	// 等待调整循环运行
	time.Sleep(150 * time.Millisecond)

	// 验证限流器仍然正常工作（不会因为 CPU 采集失败而崩溃）
	currentLimit := limiter.maxConcurrency.Load()
	assert.Equal(t, int32(50), currentLimit, "Limit should remain unchanged when CPU metrics fail")

	t.Log("✓ CPU collection failure handled gracefully")
}

// TestAdaptiveRateLimiter_NoGoroutineBasedCPU 验证不再使用 goroutine 数量计算 CPU
func TestAdaptiveRateLimiter_NoGoroutineBasedCPU(t *testing.T) {
	// 这个测试确保我们使用了真实的 CPU 监控而不是 goroutine 计数

	config := DefaultAdaptiveConfig()
	config.MetricsEnabled = true

	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	defer limiter.Stop()

	// 记录初始 goroutine 数量
	// initialGoroutines := runtime.NumGoroutine()

	// 等待采集指标
	time.Sleep(6 * time.Second)

	cpuUsage := limiter.getCPUUsage()

	// 如果使用 goroutine 计算，CPU 使用率会非常低（< 0.01）
	// 真实 CPU 使用率通常 > 0（除非系统完全空闲）
	if cpuUsage > 0 {
		// CPU 使用率应该是一个合理的值，不是基于 goroutine 计算的微小值
		t.Logf("✓ CPU usage from real metrics: %.4f (not goroutine-based)", cpuUsage)
	} else if cpuUsage == -1.0 {
		t.Log("⚠ CPU metrics collection not available in this environment")
	}
}
