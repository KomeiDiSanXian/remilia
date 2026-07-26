package resilience

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	testutil "github.com/KomeiDiSanXian/remilia/middleware/testutil"
)

// TestCircuitBreakerAutoRecovery 测试自动恢复功能
func TestCircuitBreakerAutoRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stateChanges := make([]CircuitBreakerState, 0)

		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         3,
			ResetTimeout:        100 * time.Millisecond,
			HalfOpenMaxRequests: 5,
			SuccessThreshold:    3,
			HalfOpenTimeout:     500 * time.Millisecond,
			OnStateChange: func(from, to CircuitBreakerState) {
				stateChanges = append(stateChanges, to)
			},
		})

		failCount := 0
		handler := func(ctx *eventctx.Context) error {
			failCount++
			if failCount <= 3 {
				return errors.New("service unavailable")
			}
			return nil
		}

		middleware := CircuitBreakerMiddleware(cb)
		wrappedHandler := middleware(handler)
		ctx := testutil.CreateTestContext()

		for range 3 {
			err := wrappedHandler(ctx)
			if err == nil {
				t.Errorf("Expected error, got nil")
			}
		}

		if cb.GetState() != StateOpen {
			t.Errorf("Expected state Open, got %v", cb.GetState())
		}

		err := wrappedHandler(ctx)
		if err == nil {
			t.Error("Expected circuit breaker error")
		}

		time.Sleep(150 * time.Millisecond)

		for i := range 3 {
			err := wrappedHandler(ctx)
			if err != nil {
				t.Errorf("Request %d failed: %v", i+1, err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		if cb.GetState() != StateClosed {
			t.Errorf("Expected state Closed, got %v", cb.GetState())
		}

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
	})
}

// TestCircuitBreakerHalfOpenTimeout 测试半开状态超时
func TestCircuitBreakerHalfOpenTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         2,
			ResetTimeout:        100 * time.Millisecond,
			HalfOpenMaxRequests: 10,
			SuccessThreshold:    5,
			HalfOpenTimeout:     200 * time.Millisecond,
		})

		requestCount := 0
		handler := func(ctx *eventctx.Context) error {
			requestCount++
			if requestCount <= 2 {
				return errors.New("error")
			}
			return nil
		}

		middleware := CircuitBreakerMiddleware(cb)
		wrappedHandler := middleware(handler)
		ctx := testutil.CreateTestContext()

		for range 2 {
			wrappedHandler(ctx)
		}

		if cb.GetState() != StateOpen {
			t.Errorf("Expected Open, got %v", cb.GetState())
		}

		time.Sleep(150 * time.Millisecond)

		for i := range 2 {
			err := wrappedHandler(ctx)
			if err != nil {
				t.Errorf("Request %d should succeed, got error: %v", i+1, err)
			}
		}

		if cb.GetState() != StateHalfOpen {
			t.Errorf("Expected HalfOpen, got %v", cb.GetState())
		}

		time.Sleep(250 * time.Millisecond)

		err := wrappedHandler(ctx)
		if err == nil || !errors.Is(err, errutil.ErrCircuitBreakerOpen) {
			t.Errorf("Expected circuit open error, got %v", err)
		}

		if cb.GetState() != StateOpen {
			t.Errorf("Expected Open after half-open timeout, got %v", cb.GetState())
		}
	})
}

// TestCircuitBreakerSuccessThreshold 测试成功阈值
func TestCircuitBreakerSuccessThreshold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         2,
			ResetTimeout:        50 * time.Millisecond,
			HalfOpenMaxRequests: 10,
			SuccessThreshold:    3,
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
		ctx := testutil.CreateTestContext()

		for range 2 {
			wrappedHandler(ctx)
		}

		time.Sleep(100 * time.Millisecond)

		failFirst = false

		for range 2 {
			wrappedHandler(ctx)
		}

		if cb.GetState() != StateHalfOpen {
			t.Errorf("Expected HalfOpen after 2 successes, got %v", cb.GetState())
		}

		wrappedHandler(ctx)

		if cb.GetState() != StateClosed {
			t.Errorf("Expected Closed after 3 successes, got %v", cb.GetState())
		}
	})
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

	ctx := testutil.CreateTestContext()

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
