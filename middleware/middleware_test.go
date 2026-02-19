package middleware

import (
	"errors"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandler creates a mock handler for testing
func mockHandler(err error, delay time.Duration) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		if delay > 0 {
			time.Sleep(delay)
		}
		return err
	}
}

// mockPanicHandler creates a handler that panics
func mockPanicHandler(panicValue any) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		panic(panicValue)
	}
}

// createTestContext creates a test context
func createTestContext() *eventctx.Context {
	event := &dto.Payload{
		ID:   "test-event",
		Type: "TEST_EVENT",
	}
	return eventctx.NewContext(event, nil)
}

// TestLogging tests the Logging middleware
func TestLogging(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		mw := Logging()
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("failed execution", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		mw := Logging()
		handler := mw(mockHandler(expectedErr, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, expectedErr)
	})
}

// TestRecover tests the Recover middleware
func TestRecover(t *testing.T) {
	t.Run("recover from panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(mockPanicHandler("test panic"))

		ctx := createTestContext()
		err := handler(ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "test panic")
	})

	t.Run("recover from nil panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(mockPanicHandler(nil))

		ctx := createTestContext()
		err := handler(ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")
	})

	t.Run("no panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})
}

// TestAuth tests the Auth middleware
func TestAuth(t *testing.T) {
	t.Run("authorized", func(t *testing.T) {
		mw := Auth(func(ctx *eventctx.Context) bool {
			return true
		})
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("unauthorized", func(t *testing.T) {
		mw := Auth(func(ctx *eventctx.Context) bool {
			return false
		})
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

// TestTimeout tests the Timeout middleware
func TestTimeout(t *testing.T) {
	t.Run("completes before timeout", func(t *testing.T) {
		mw := Timeout(100 * time.Millisecond)
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("timeout", func(t *testing.T) {
		mw := Timeout(50 * time.Millisecond)
		handler := mw(mockHandler(nil, 200*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("panic in handler", func(t *testing.T) {
		mw := Timeout(100 * time.Millisecond)
		handler := mw(mockPanicHandler("timeout panic"))

		ctx := createTestContext()
		err := handler(ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic in handler")
	})
}

// TestMetrics tests the Metrics middleware
func TestMetrics(t *testing.T) {
	t.Run("records metrics", func(t *testing.T) {
		mw := Metrics()
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("records metrics with error", func(t *testing.T) {
		expectedErr := errors.New("metrics error")
		mw := Metrics()
		handler := mw(mockHandler(expectedErr, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, expectedErr)
	})
}

// TestConcurrencyLimit tests the ConcurrencyLimit middleware
func TestConcurrencyLimit(t *testing.T) {
	t.Run("drop policy - within limit", func(t *testing.T) {
		mw := ConcurrencyLimit(2, ConcurrencyDrop, 0)
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("drop policy - exceeds limit", func(t *testing.T) {
		mw := ConcurrencyLimit(1, ConcurrencyDrop, 0)
		handler := mw(mockHandler(nil, 100*time.Millisecond))

		// Start first request
		done1 := make(chan error, 1)
		go func() {
			ctx1 := createTestContext()
			done1 <- handler(ctx1)
		}()

		time.Sleep(10 * time.Millisecond) // Ensure first request acquired token

		// Second request should be dropped
		ctx2 := createTestContext()
		err := handler(ctx2)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "concurrency limit exceeded")

		<-done1 // Wait for first request
	})

	t.Run("block policy", func(t *testing.T) {
		mw := ConcurrencyLimit(1, ConcurrencyBlock, 0)
		handler := mw(mockHandler(nil, 50*time.Millisecond))

		start := time.Now()

		// Start first request
		done1 := make(chan error, 1)
		go func() {
			ctx1 := createTestContext()
			done1 <- handler(ctx1)
		}()

		time.Sleep(10 * time.Millisecond)

		// Second request should block
		ctx2 := createTestContext()
		err := handler(ctx2)

		duration := time.Since(start)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)

		<-done1
	})

	t.Run("try-wait policy - timeout", func(t *testing.T) {
		mw := ConcurrencyLimit(1, ConcurrencyTryWait, 30*time.Millisecond)
		handler := mw(mockHandler(nil, 100*time.Millisecond))

		// Start first request
		done1 := make(chan error, 1)
		go func() {
			ctx1 := createTestContext()
			done1 <- handler(ctx1)
		}()

		time.Sleep(10 * time.Millisecond)

		// Second request should timeout
		ctx2 := createTestContext()
		err := handler(ctx2)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "concurrency limit exceeded")

		<-done1
	})
}

// TestRetryConfig tests retry configuration
func TestRetryConfig(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := RetryConfig{}
		mw := Retry(cfg)

		require.NotNil(t, mw)
	})

	t.Run("custom config", func(t *testing.T) {
		cfg := RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 100 * time.Millisecond,
			BackoffMax:  1 * time.Second,
		}
		mw := Retry(cfg)

		require.NotNil(t, mw)
	})
}

// TestRetry tests the Retry middleware
func TestRetry(t *testing.T) {
	t.Run("succeeds on first try", func(t *testing.T) {
		mw := Retry(RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
		})
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("succeeds after retries", func(t *testing.T) {
		attempts := 0
		mw := Retry(RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
		})

		handler := mw(func(ctx *eventctx.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary error")
			}
			return nil
		})

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})

	t.Run("max attempts reached", func(t *testing.T) {
		expectedErr := errors.New("persistent error")
		mw := Retry(RetryConfig{
			MaxAttempts: 2,
			BackoffBase: 10 * time.Millisecond,
		})
		handler := mw(mockHandler(expectedErr, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("custom should retry function", func(t *testing.T) {
		attempts := 0
		specialErr := errors.New("special error")

		mw := Retry(RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
			ShouldRetry: func(err error) bool {
				return !errors.Is(err, specialErr)
			},
		})

		handler := mw(func(ctx *eventctx.Context) error {
			attempts++
			return specialErr
		})

		ctx := createTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, specialErr)
		assert.Equal(t, 1, attempts) // Should not retry
	})
}

// TestCircuitBreaker tests CircuitBreaker functionality
func TestCircuitBreaker(t *testing.T) {
	t.Run("new circuit breaker", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         3,
			ResetTimeout:        100 * time.Millisecond,
			HalfOpenMaxRequests: 1,
		})

		assert.NotNil(t, cb)
		assert.Equal(t, StateClosed, cb.GetState())
		assert.Equal(t, int32(0), cb.GetFailures())
	})

	t.Run("default config", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{})

		assert.NotNil(t, cb)
		assert.Equal(t, StateClosed, cb.GetState())
	})

	t.Run("closed to open transition", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:  2,
			ResetTimeout: 100 * time.Millisecond,
		})

		mw := CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(errors.New("test error"), 0))

		ctx := createTestContext()

		// First failure
		err := handler(ctx)
		assert.Error(t, err)
		assert.Equal(t, StateClosed, cb.GetState())

		// Second failure - should open
		err = handler(ctx)
		assert.Error(t, err)
		assert.Equal(t, StateOpen, cb.GetState())

		// Third call should be rejected
		err = handler(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circuit breaker is open")
	})

	t.Run("open to half-open transition", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:  1,
			ResetTimeout: 50 * time.Millisecond,
		})

		mw := CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(errors.New("test error"), 0))

		ctx := createTestContext()

		// Trigger open
		_ = handler(ctx)
		assert.Equal(t, StateOpen, cb.GetState())

		// Wait for reset timeout
		time.Sleep(60 * time.Millisecond)

		// Should be half-open now
		successHandler := mw(mockHandler(nil, 0))
		err := successHandler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, StateClosed, cb.GetState())
	})

	t.Run("success in closed state", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures: 3,
		})

		mw := CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, StateClosed, cb.GetState())
		assert.Equal(t, int32(0), cb.GetFailures())
	})
}

// TestConcurrencyPolicy tests concurrency policy enum
func TestConcurrencyPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy ConcurrencyPolicy
	}{
		{"drop", ConcurrencyDrop},
		{"block", ConcurrencyBlock},
		{"try-wait", ConcurrencyTryWait},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := ConcurrencyLimit(1, tt.policy, 100*time.Millisecond)
			assert.NotNil(t, mw)
		})
	}
}

// TestCircuitBreakerState tests circuit breaker state enum
func TestCircuitBreakerState(t *testing.T) {
	tests := []struct {
		name  string
		state CircuitBreakerState
	}{
		{"closed", StateClosed},
		{"open", StateOpen},
		{"half-open", StateHalfOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.state)
		})
	}
}

// TestMiddlewareChaining tests chaining multiple middlewares
func TestMiddlewareChaining(t *testing.T) {
	t.Run("chain multiple middlewares", func(t *testing.T) {
		executed := false

		finalHandler := func(ctx *eventctx.Context) error {
			executed = true
			return nil
		}

		// Chain: Recover -> Logging -> Metrics -> Handler
		handler := Recover()(Logging()(Metrics()(finalHandler)))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.True(t, executed)
	})

	t.Run("chain with error propagation", func(t *testing.T) {
		expectedErr := errors.New("chain error")

		finalHandler := mockHandler(expectedErr, 0)

		// Chain middlewares
		handler := Recover()(Logging()(finalHandler))

		ctx := createTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, expectedErr)
	})
}

// BenchmarkLogging benchmarks the Logging middleware
func BenchmarkLogging(b *testing.B) {
	mw := Logging()
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// BenchmarkRecover benchmarks the Recover middleware
func BenchmarkRecover(b *testing.B) {
	mw := Recover()
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// BenchmarkRetry benchmarks the Retry middleware
func BenchmarkRetry(b *testing.B) {
	mw := Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
	})
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// BenchmarkCircuitBreaker benchmarks the CircuitBreaker middleware
func BenchmarkCircuitBreaker(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures: 10,
	})
	mw := CircuitBreakerMiddleware(cb)
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// BenchmarkMiddlewareChain benchmarks chained middlewares
func BenchmarkMiddlewareChain(b *testing.B) {
	handler := Recover()(Logging()(Metrics()(mockHandler(nil, 0))))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}
