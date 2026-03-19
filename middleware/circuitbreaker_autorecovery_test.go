package middleware

import (
	"errors"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// TestCircuitBreakerAutoRecovery 测试自动恢复功能
func TestCircuitBreakerAutoRecovery(t *testing.T) {
	stateChanges := make([]CircuitBreakerState, 0)

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 5,
		SuccessThreshold:    3, // 需要 3 次成功才能关闭
		HalfOpenTimeout:     500 * time.Millisecond,
		OnStateChange: func(from, to CircuitBreakerState) {
			stateChanges = append(stateChanges, to)
		},
	})

	// 创建测试 handler
	failCount := 0
	handler := func(ctx *eventctx.Context) error {
		failCount++
		if failCount <= 3 {
			return errors.New("service unavailable")
		}
		return nil // 之后成功
	}

	middleware := CircuitBreakerMiddleware(cb)
	wrappedHandler := middleware(handler)

	// 创建测试 context
	ctx := createTestContext()

	// 1. 触发 3 次失败，打开熔断器
	t.Log("Step 1: Trigger failures to open circuit")
	for range 3 {
		err := wrappedHandler(ctx)
		if err == nil {
			t.Errorf("Expected error, got nil")
		}
	}

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state Open, got %v", cb.GetState())
	}

	// 2. 尝试请求，应该被拒绝
	t.Log("Step 2: Verify requests are rejected")
	err := wrappedHandler(ctx)
	if err == nil {
		t.Error("Expected circuit breaker error")
	}

	// 3. 等待 ResetTimeout，进入半开状态
	t.Log("Step 3: Wait for reset timeout")
	time.Sleep(150 * time.Millisecond)

	// 4. 半开状态下发送成功请求
	t.Log("Step 4: Send successful requests in half-open state")
	for i := range 3 {
		err := wrappedHandler(ctx)
		if err != nil {
			t.Errorf("Request %d failed: %v", i+1, err)
		}
		t.Logf("Success %d/%d", i+1, 3)
		time.Sleep(10 * time.Millisecond)
	}

	// 5. 验证已经转为关闭状态
	t.Log("Step 5: Verify circuit is closed")
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state Closed, got %v", cb.GetState())
	}

	// 验证状态转换顺序
	expectedStates := []CircuitBreakerState{StateOpen, StateHalfOpen, StateClosed}
	if len(stateChanges) < len(expectedStates) {
		t.Errorf("Expected at least %d state changes, got %d", len(expectedStates), len(stateChanges))
	} else {
		for i, expected := range expectedStates {
			if stateChanges[i] != expected {
				t.Errorf("State change %d: expected %v, got %v", i, expected, stateChanges[i])
			}
		}
	}

	t.Logf("State changes: %v", stateChanges)
}

// TestCircuitBreakerHalfOpenTimeout 测试半开状态超时
func TestCircuitBreakerHalfOpenTimeout(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         2,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 10,
		SuccessThreshold:    5,                      // 需要 5 次成功
		HalfOpenTimeout:     200 * time.Millisecond, // 短超时
	})

	requestCount := 0
	handler := func(ctx *eventctx.Context) error {
		requestCount++
		// 前 2 次失败（触发打开），后续成功但不够成功阈值
		if requestCount <= 2 {
			return errors.New("error")
		}
		return nil
	}

	middleware := CircuitBreakerMiddleware(cb)
	wrappedHandler := middleware(handler)

	ctx := createTestContext()

	// 触发失败，打开熔断器
	for range 2 {
		wrappedHandler(ctx)
	}

	if cb.GetState() != StateOpen {
		t.Errorf("Expected Open, got %v", cb.GetState())
	}

	// 等待进入半开状态
	time.Sleep(150 * time.Millisecond)

	// 发送 2 次成功请求，进入半开状态但不够关闭（需要 5 次）
	for i := range 2 {
		err := wrappedHandler(ctx)
		if err != nil {
			t.Errorf("Request %d should succeed, got error: %v", i+1, err)
		}
	}

	// 验证还在半开状态
	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected HalfOpen, got %v", cb.GetState())
	}

	// 等待半开状态超时
	time.Sleep(250 * time.Millisecond)

	// 现在应该重新回到 Open 状态（因为半开超时）
	err := wrappedHandler(ctx)
	if err == nil || err.Error() != "circuit breaker is open" {
		t.Errorf("Expected circuit open error, got %v", err)
	}

	if cb.GetState() != StateOpen {
		t.Errorf("Expected Open after half-open timeout, got %v", cb.GetState())
	}
}

// TestCircuitBreakerSuccessThreshold 测试成功阈值
func TestCircuitBreakerSuccessThreshold(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		HalfOpenMaxRequests: 10,
		SuccessThreshold:    3, // 需要 3 次成功
		HalfOpenTimeout:     1 * time.Second,
	})

	failFirst := true
	handler := func(ctx *eventctx.Context) error {
		if failFirst {
			return errors.New("error")
		}
		return nil
	}

	middleware := CircuitBreakerMiddleware(cb)
	wrappedHandler := middleware(handler)

	ctx := createTestContext()

	// 触发失败
	for range 2 {
		wrappedHandler(ctx)
	}

	// 等待进入半开
	time.Sleep(100 * time.Millisecond)

	// 切换为成功模式
	failFirst = false

	// 发送 2 次成功，不应该关闭（需要 3 次）
	for range 2 {
		wrappedHandler(ctx)
	}

	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected HalfOpen after 2 successes, got %v", cb.GetState())
	}

	// 第 3 次成功，应该关闭
	wrappedHandler(ctx)

	if cb.GetState() != StateClosed {
		t.Errorf("Expected Closed after 3 successes, got %v", cb.GetState())
	}

	stats := cb.Stats()
	t.Logf("Final stats: State=%v, Failures=%d, Successes=%d",
		stats.State, stats.Failures, stats.Successes)
}

// TestCircuitBreakerStats 测试统计信息
func TestCircuitBreakerStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:      2,
		ResetTimeout:     100 * time.Millisecond,
		SuccessThreshold: 2,
	})

	handler := func(ctx *eventctx.Context) error {
		return errors.New("error")
	}

	middleware := CircuitBreakerMiddleware(cb)
	wrappedHandler := middleware(handler)

	ctx := createTestContext()

	// 触发失败
	wrappedHandler(ctx)
	wrappedHandler(ctx)

	stats := cb.Stats()
	if stats.State != StateOpen {
		t.Errorf("Expected Open state, got %v", stats.State)
	}
	if stats.Failures != 2 {
		t.Errorf("Expected 2 failures, got %d", stats.Failures)
	}
	if stats.LastFailure.IsZero() {
		t.Error("LastFailure should not be zero")
	}

	t.Logf("Stats: %+v", stats)
}
