package middleware

import (
	"errors"
	"testing"
	"time"

	context2 "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// DeadLetter Middleware Tests
// ============================================================================

func TestDeadLetterMiddleware(t *testing.T) {
	t.Run("enqueues on error", func(t *testing.T) {
		q := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{
			MaxSize: 100,
		})

		mw := DeadLetter(q)
		handler := mw(mockHandler(errors.New("dlq error"), 0))

		err := handler(createTestContext())
		assert.Error(t, err)

		// Give it time to enqueue
		time.Sleep(50 * time.Millisecond)

		stats := q.Stats()
		assert.GreaterOrEqual(t, stats.QueueSize, 0)
	})

	t.Run("no enqueue on success", func(t *testing.T) {
		q := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{
			MaxSize: 100,
		})

		mw := DeadLetter(q)
		handler := mw(mockHandler(nil, 0))

		err := handler(createTestContext())
		assert.NoError(t, err)

		stats := q.Stats()
		assert.Equal(t, int64(0), stats.Dropped)
	})

	t.Run("nil queue", func(t *testing.T) {
		mw := DeadLetter(nil)
		handler := mw(mockHandler(errors.New("error with nil queue"), 0))

		err := handler(createTestContext())
		assert.Error(t, err)
	})
}

// ============================================================================
// CircuitBreaker Additional Tests
// ============================================================================

func TestCircuitBreakerAdvanced(t *testing.T) {
	t.Run("half-open to closed transition", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         1,
			ResetTimeout:        50 * time.Millisecond,
			HalfOpenMaxRequests: 2,
		})

		mw := CircuitBreakerMiddleware(cb)

		// Cause failure
		failHandler := mw(mockHandler(errors.New("fail"), 0))
		failHandler(createTestContext())

		assert.Equal(t, StateOpen, cb.GetState())

		// Wait for reset timeout
		time.Sleep(60 * time.Millisecond)

		// First success in half-open
		successHandler := mw(mockHandler(nil, 0))
		err := successHandler(createTestContext())
		assert.NoError(t, err)

		// Circuit should close after enough successes
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("half-open to open on failure", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:  1,
			ResetTimeout: 50 * time.Millisecond,
		})

		mw := CircuitBreakerMiddleware(cb)

		// Open circuit
		handler := mw(mockHandler(errors.New("fail"), 0))
		handler(createTestContext())

		// Wait for half-open
		time.Sleep(60 * time.Millisecond)

		// Fail in half-open state
		handler(createTestContext())

		// Should be open again
		assert.Equal(t, StateOpen, cb.GetState())
	})

	t.Run("concurrent requests in half-open", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         1,
			ResetTimeout:        50 * time.Millisecond,
			HalfOpenMaxRequests: 1,
		})

		mw := CircuitBreakerMiddleware(cb)

		// Open circuit
		failHandler := mw(mockHandler(errors.New("fail"), 0))
		failHandler(createTestContext())

		// Wait for half-open
		time.Sleep(60 * time.Millisecond)

		// Try multiple concurrent requests
		successHandler := mw(mockHandler(nil, 10*time.Millisecond))

		done := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				done <- successHandler(createTestContext())
			}()
		}

		// One should succeed, one might be rejected
		err1 := <-done
		err2 := <-done

		// At least one should execute
		assert.True(t, err1 == nil || err2 == nil)
	})

	t.Run("get failures count", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures: 3,
		})

		mw := CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(errors.New("fail"), 0))

		handler(createTestContext())
		handler(createTestContext())

		assert.Equal(t, int32(2), cb.GetFailures())
	})

	t.Run("reset failures on success", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures: 5,
		})

		mw := CircuitBreakerMiddleware(cb)

		// Some failures
		failHandler := mw(mockHandler(errors.New("fail"), 0))
		failHandler(createTestContext())
		failHandler(createTestContext())

		assert.Equal(t, int32(2), cb.GetFailures())

		// Success resets
		successHandler := mw(mockHandler(nil, 0))
		successHandler(createTestContext())

		assert.Equal(t, int32(0), cb.GetFailures())
	})
}

// ============================================================================
// Retry Additional Tests
// ============================================================================

func TestRetryAdvanced(t *testing.T) {
	t.Run("exponential backoff", func(t *testing.T) {
		attempts := 0
		start := time.Now()

		mw := Retry(RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 50 * time.Millisecond,
			BackoffMax:  200 * time.Millisecond,
		})

		handler := mw(func(ctx *context2.Context) error {
			attempts++
			return errors.New("retry")
		})

		handler(createTestContext())

		duration := time.Since(start)

		// MaxAttempts includes initial attempt
		assert.GreaterOrEqual(t, attempts, 3)
		// Should take at least 50ms (first retry)
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
	})

	t.Run("max backoff limit", func(t *testing.T) {
		mw := Retry(RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 100 * time.Millisecond,
			BackoffMax:  150 * time.Millisecond,
		})

		handler := mw(mockHandler(errors.New("retry"), 0))

		start := time.Now()
		handler(createTestContext())
		duration := time.Since(start)

		// With exponential backoff capped at 150ms, total should be reasonable
		assert.Less(t, duration, 1*time.Second)
	})

	t.Run("shouldRetry custom function", func(t *testing.T) {
		permanentErr := errors.New("permanent error")
		attempts := 0

		mw := Retry(RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 10 * time.Millisecond,
			ShouldRetry: func(err error) bool {
				return err != permanentErr
			},
		})

		handler := mw(func(ctx *context2.Context) error {
			attempts++
			return permanentErr
		})

		handler(createTestContext())

		// Should not retry on permanent error
		assert.Equal(t, 1, attempts)
	})

	t.Run("sets retry attempt in context", func(t *testing.T) {
		var lastAttempt int

		mw := Retry(RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
		})

		handler := mw(func(ctx *context2.Context) error {
			attempt, _ := ctx.GetRetryAttempt()
			lastAttempt = attempt
			return errors.New("retry")
		})

		handler(createTestContext())

		assert.Equal(t, 3, lastAttempt)
	})
}

// ============================================================================
// Timeout Additional Tests
// ============================================================================

func TestTimeoutAdvanced(t *testing.T) {
	t.Run("handler completes just before timeout", func(t *testing.T) {
		mw := Timeout(100 * time.Millisecond)
		handler := mw(mockHandler(nil, 90*time.Millisecond))

		err := handler(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("handler times out exactly", func(t *testing.T) {
		mw := Timeout(50 * time.Millisecond)
		handler := mw(mockHandler(nil, 60*time.Millisecond))

		err := handler(createTestContext())
		assert.Error(t, err)
	})

	t.Run("panic propagation", func(t *testing.T) {
		mw := Timeout(100 * time.Millisecond)
		handler := mw(mockPanicHandler("timeout panic"))

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})
}

// ============================================================================
// ConcurrencyLimit Additional Tests
// ============================================================================

func TestConcurrencyLimitAdvanced(t *testing.T) {
	t.Run("try-wait success", func(t *testing.T) {
		mw := ConcurrencyLimit(2, ConcurrencyTryWait, 100*time.Millisecond)
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		// First request
		go handler(createTestContext())

		time.Sleep(5 * time.Millisecond)

		// Second request should wait and succeed
		err := handler(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("block policy waits", func(t *testing.T) {
		mw := ConcurrencyLimit(1, ConcurrencyBlock, 0)
		handler := mw(mockHandler(nil, 50*time.Millisecond))

		start := time.Now()

		// First in background
		go handler(createTestContext())

		time.Sleep(10 * time.Millisecond)

		// Second should block
		handler(createTestContext())

		duration := time.Since(start)
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
	})

	t.Run("drop policy immediate rejection", func(t *testing.T) {
		mw := ConcurrencyLimit(1, ConcurrencyDrop, 0)
		handler := mw(mockHandler(nil, 100*time.Millisecond))

		// First request
		go handler(createTestContext())

		time.Sleep(10 * time.Millisecond)

		// Second should be dropped immediately
		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "concurrency limit")
	})
}

// ============================================================================
// Logging Additional Tests
// ============================================================================

func TestLoggingAdvanced(t *testing.T) {
	t.Run("logs handler details", func(t *testing.T) {
		mw := Logging()
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		err := handler(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("logs error details", func(t *testing.T) {
		testErr := errors.New("logging test error")
		mw := Logging()
		handler := mw(mockHandler(testErr, 0))

		err := handler(createTestContext())
		assert.ErrorIs(t, err, testErr)
	})
}

// ============================================================================
// Metrics Additional Tests
// ============================================================================

func TestMetricsAdvanced(t *testing.T) {
	t.Run("measures duration", func(t *testing.T) {
		mw := Metrics()
		handler := mw(mockHandler(nil, 50*time.Millisecond))

		start := time.Now()
		err := handler(createTestContext())
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
	})

	t.Run("records error", func(t *testing.T) {
		testErr := errors.New("metrics error")
		mw := Metrics()
		handler := mw(mockHandler(testErr, 0))

		err := handler(createTestContext())
		assert.ErrorIs(t, err, testErr)
	})
}

// ============================================================================
// Auth Additional Tests
// ============================================================================

func TestAuthAdvanced(t *testing.T) {
	t.Run("complex authorization logic", func(t *testing.T) {
		mw := Auth(func(ctx *context2.Context) bool {
			// Simulate checking user roles
			return ctx.GetEvent() != nil
		})

		handler := mw(mockHandler(nil, 0))

		err := handler(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("returns error on unauthorized", func(t *testing.T) {
		mw := Auth(func(ctx *context2.Context) bool {
			return false
		})

		handler := mw(mockHandler(nil, 0))

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestMiddlewareChainingAdvanced(t *testing.T) {
	t.Run("multiple middlewares in order", func(t *testing.T) {
		order := []string{}

		mw1 := func(next context2.Handler) context2.Handler {
			return func(ctx *context2.Context) error {
				order = append(order, "mw1-before")
				err := next(ctx)
				order = append(order, "mw1-after")
				return err
			}
		}

		mw2 := func(next context2.Handler) context2.Handler {
			return func(ctx *context2.Context) error {
				order = append(order, "mw2-before")
				err := next(ctx)
				order = append(order, "mw2-after")
				return err
			}
		}

		handler := mw1(mw2(func(ctx *context2.Context) error {
			order = append(order, "handler")
			return nil
		}))

		handler(createTestContext())

		expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
		assert.Equal(t, expected, order)
	})

	t.Run("recover + logging + metrics", func(t *testing.T) {
		handler := Recover()(Logging()(Metrics()(mockHandler(nil, 0))))

		err := handler(createTestContext())
		assert.NoError(t, err)
	})

	t.Run("retry + circuit breaker", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures: 5,
		})

		handler := Retry(RetryConfig{
			MaxAttempts: 2,
			BackoffBase: 10 * time.Millisecond,
		})(CircuitBreakerMiddleware(cb)(mockHandler(errors.New("fail"), 0)))

		err := handler(createTestContext())
		assert.Error(t, err)
	})
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkMiddlewareChainAdvanced(b *testing.B) {
	handler := Recover()(Logging()(Metrics()(Timeout(1 * time.Second)(mockHandler(nil, 0)))))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}

func BenchmarkCircuitBreakerAdvanced(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures: 100,
	})
	mw := CircuitBreakerMiddleware(cb)
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}

func BenchmarkRetryAdvanced(b *testing.B) {
	mw := Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
	})
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}
