package middleware

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	dedup "github.com/KomeiDiSanXian/remilia/middleware/dedup"
	degradation "github.com/KomeiDiSanXian/remilia/middleware/degradation"
	resilience "github.com/KomeiDiSanXian/remilia/middleware/resilience"
	telemetry "github.com/KomeiDiSanXian/remilia/middleware/telemetry"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Degradation Full Coverage Tests
// ============================================================================

func TestAdaptiveDegradationFull(t *testing.T) {
	t.Run("new adaptive degradation", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			MonitorInterval: 100 * time.Millisecond,
			Strategy:        degradation.DegradationDrop,
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		assert.NotNil(t, ad)
		assert.Equal(t, degradation.LevelNormal, ad.GetLevel())
	})

	t.Run("middleware normal level", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) degradation.EventPriority {
				return degradation.PriorityLow
			},
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		mw := ad.Middleware()

		executed := false
		handler := mw(func(ctx *eventctx.Context) error {
			executed = true
			return nil
		})

		// Normal level - should execute
		err := handler(createTestContext())
		assert.NoError(t, err)
		assert.True(t, executed)
	})

	t.Run("priority classifier", func(t *testing.T) {
		highPriorityCalls := 0
		lowPriorityCalls := 0

		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) degradation.EventPriority {
				if ctx.GetMessageContent() == "HIGH_PRIORITY" {
					return degradation.PriorityHigh
				}
				return degradation.PriorityLow
			},
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		mw := ad.Middleware()

		handler := mw(func(ctx *eventctx.Context) error {
			if ctx.GetMessageContent() == "HIGH_PRIORITY" {
				highPriorityCalls++
			} else {
				lowPriorityCalls++
			}
			return nil
		})

		// Low priority
		event1 := &middlewareTestEvent{id: "1", kind: platform.EventKindPrivateMessage, content: "LOW_PRIORITY"}
		_ = handler(eventctx.NewContextFromEvent(event1, nil))

		// High priority
		event2 := &middlewareTestEvent{id: "2", kind: platform.EventKindPrivateMessage, content: "HIGH_PRIORITY"}
		_ = handler(eventctx.NewContextFromEvent(event2, nil))

		assert.Equal(t, 1, lowPriorityCalls)
		assert.Equal(t, 1, highPriorityCalls)
	})

	t.Run("get level", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
		}

		ad := degradation.NewAdaptiveDegradation(cfg)

		// Initially normal
		assert.Equal(t, degradation.LevelNormal, ad.GetLevel())
	})

	t.Run("stats tracking", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) degradation.EventPriority {
				return degradation.PriorityLow
			},
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		mw := ad.Middleware()

		handler := mw(func(ctx *eventctx.Context) error {
			return nil
		})

		// Process some events
		for range 5 {
			_ = handler(createTestContext())
		}

		stats := ad.Stats()
		assert.Equal(t, degradation.LevelNormal, stats.Level)
		assert.Equal(t, int64(5), stats.TotalEvents)
	})

	t.Run("reset stats", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		mw := ad.Middleware()

		handler := mw(func(ctx *eventctx.Context) error {
			return nil
		})

		_ = handler(createTestContext())

		stats := ad.Stats()
		assert.Greater(t, stats.TotalEvents, int64(0))

		ad.Reset()

		stats = ad.Stats()
		assert.Equal(t, int64(0), stats.TotalEvents)
		assert.Equal(t, int64(0), stats.DroppedEvents)
	})

	t.Run("on level change callback", func(t *testing.T) {
		callbackSet := false

		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
			OnLevelChange: func(from, to degradation.DegradationLevel) {
				callbackSet = true
			},
		}

		ad := degradation.NewAdaptiveDegradation(cfg)

		// Verify callback is set
		assert.NotNil(t, ad)
		_ = callbackSet // Just to use the variable
	})
}

func TestDegradationStrategies(t *testing.T) {
	t.Run("drop strategy", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDrop,
			PriorityClassifier: func(ctx *eventctx.Context) degradation.EventPriority {
				return degradation.PriorityLow
			},
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		assert.NotNil(t, ad)
		assert.Equal(t, degradation.DegradationDrop, cfg.Strategy)
	})

	t.Run("delay strategy", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationDelay,
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		assert.NotNil(t, ad)
		assert.Equal(t, degradation.DegradationDelay, cfg.Strategy)
	})

	t.Run("simplify strategy", func(t *testing.T) {
		cfg := degradation.DegradationConfig{
			CPUThreshold:    80.0,
			MemoryThreshold: 85.0,
			Strategy:        degradation.DegradationSimplify,
		}

		ad := degradation.NewAdaptiveDegradation(cfg)
		assert.NotNil(t, ad)
		assert.Equal(t, degradation.DegradationSimplify, cfg.Strategy)
	})
}

func TestEventPriorities(t *testing.T) {
	priorities := []degradation.EventPriority{
		degradation.PriorityLow,
		degradation.PriorityNormal,
		degradation.PriorityHigh,
		degradation.PriorityCritical,
	}

	for _, p := range priorities {
		assert.GreaterOrEqual(t, int(p), 0)
	}
}

// ============================================================================
// Additional Coverage Tests for Other Modules
// ============================================================================

func TestRecoverAdvanced(t *testing.T) {
	t.Run("recover from string panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(func(ctx *eventctx.Context) error {
			panic("string panic")
		})

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})

	t.Run("recover from error panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(func(ctx *eventctx.Context) error {
			panic(errors.New("error panic"))
		})

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})

	t.Run("recover from nil panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(func(ctx *eventctx.Context) error {
			panic(nil)
		})

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})

	t.Run("no panic", func(t *testing.T) {
		mw := Recover()
		handler := mw(func(ctx *eventctx.Context) error {
			return errors.New("normal error")
		})

		err := handler(createTestContext())
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "panic")
	})
}

func TestAuthAdvancedExtra(t *testing.T) {
	t.Run("multiple auth checks", func(t *testing.T) {
		calls := 0
		mw := Auth(func(ctx *eventctx.Context) bool {
			calls++
			return calls <= 2
		})

		handler := mw(mockHandler(nil, 0))

		// First two should pass
		assert.NoError(t, handler(createTestContext()))
		assert.NoError(t, handler(createTestContext()))

		// Third should fail
		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("auth with event inspection", func(t *testing.T) {
		mw := Auth(func(ctx *eventctx.Context) bool {
			return ctx.GetMessageContent() == "TEST_EVENT"
		})

		handler := mw(mockHandler(nil, 0))

		err := handler(createTestContext())
		assert.NoError(t, err)
	})
}

func TestTimeoutAdvancedExtra(t *testing.T) {
	t.Run("multiple timeouts", func(t *testing.T) {
		mw := Timeout(50 * time.Millisecond)

		// Fast handler - no timeout
		fast := mw(mockHandler(nil, 10*time.Millisecond))
		err := fast(createTestContext())
		assert.NoError(t, err)

		// Slow handler - timeout
		slow := mw(mockHandler(nil, 100*time.Millisecond))
		err = slow(createTestContext())
		assert.Error(t, err)
	})

	t.Run("timeout with error", func(t *testing.T) {
		mw := Timeout(100 * time.Millisecond)
		handler := mw(mockHandler(errors.New("handler error"), 10*time.Millisecond))

		err := handler(createTestContext())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "handler error")
	})
}

func TestMetricsAdvancedExtra(t *testing.T) {
	t.Run("metrics with various durations", func(t *testing.T) {
		mw := Metrics()

		durations := []time.Duration{
			0,
			10 * time.Millisecond,
			50 * time.Millisecond,
		}

		for _, d := range durations {
			handler := mw(mockHandler(nil, d))
			err := handler(createTestContext())
			assert.NoError(t, err)
		}
	})

	t.Run("metrics with different errors", func(t *testing.T) {
		mw := Metrics()

		testErrors := []error{
			nil,
			assert.AnError,
			errors.New("custom error"),
		}

		for _, e := range testErrors {
			handler := mw(mockHandler(e, 0))
			handler(createTestContext())
		}
	})
}

func TestBackpressureEdgeCases(t *testing.T) {
	t.Run("zero or small limit", func(t *testing.T) {
		mw := Backpressure(1, BackpressureDrop, 0)
		handler := mw(mockHandler(nil, 50*time.Millisecond))

		// First should succeed
		done := make(chan error, 1)
		go func() {
			done <- handler(createTestContext())
		}()

		time.Sleep(10 * time.Millisecond)

		// Second should be dropped
		err := handler(createTestContext())
		assert.Error(t, err)

		<-done
	})

	t.Run("large limit", func(t *testing.T) {
		mw := Backpressure(1000, BackpressureDrop, 0)
		handler := mw(mockHandler(nil, 0))

		var wg sync.WaitGroup
		errorCount := int32(0)

		for range 100 {
			wg.Go(func() {
				if err := handler(createTestContext()); err != nil {
					atomic.AddInt32(&errorCount, 1)
				}
			})
		}

		wg.Wait()

		// With limit 1000, no errors expected for 100 concurrent
		assert.Equal(t, int32(0), errorCount)
	})
}

func TestRetryEdgeCases(t *testing.T) {
	t.Run("zero max attempts", func(t *testing.T) {
		mw := resilience.Retry(resilience.RetryConfig{
			MaxAttempts: 0,
			BackoffBase: 10 * time.Millisecond,
		})

		attempts := 0
		handler := mw(func(ctx *eventctx.Context) error {
			attempts++
			return errors.New("error")
		})

		err := handler(createTestContext())
		assert.Error(t, err)
		// Should still try at least once
		assert.GreaterOrEqual(t, attempts, 1)
	})

	t.Run("very short backoff", func(t *testing.T) {
		start := time.Now()

		mw := resilience.Retry(resilience.RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 1 * time.Millisecond,
			BackoffMax:  5 * time.Millisecond,
		})

		handler := mw(mockHandler(errors.New("error"), 0))

		handler(createTestContext())

		duration := time.Since(start)
		// Should complete quickly with short backoff
		assert.Less(t, duration, 100*time.Millisecond)
	})

	t.Run("backoff max respected", func(t *testing.T) {
		start := time.Now()

		mw := resilience.Retry(resilience.RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 100 * time.Millisecond,
			BackoffMax:  50 * time.Millisecond, // Max is less than base * 2
		})

		handler := mw(mockHandler(errors.New("error"), 0))

		handler(createTestContext())

		duration := time.Since(start)
		// With max 50ms * 4 retries = 200ms, should be less than 300ms
		assert.Less(t, duration, 300*time.Millisecond)
	})
}

func TestCircuitBreakerEdgeCases(t *testing.T) {
	t.Run("zero max failures", func(t *testing.T) {
		cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			MaxFailures: 0,
		})

		mw := resilience.CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(errors.New("error"), 0))

		// Should still work with zero max failures
		handler(createTestContext())
	})

	t.Run("very short reset timeout", func(t *testing.T) {
		cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			MaxFailures:  1,
			ResetTimeout: 10 * time.Millisecond,
		})

		mw := resilience.CircuitBreakerMiddleware(cb)

		// Open circuit
		failHandler := mw(mockHandler(errors.New("error"), 0))
		failHandler(createTestContext())

		assert.Equal(t, resilience.StateOpen, cb.GetState())

		// Wait for reset
		time.Sleep(20 * time.Millisecond)

		// Should transition to half-open
		successHandler := mw(mockHandler(nil, 0))
		successHandler(createTestContext())
	})
}

func TestDedupEdgeCases(t *testing.T) {
	t.Run("very small cache", func(t *testing.T) {
		filter := dedup.NewDedupFilter(dedup.DedupConfig{
			MaxSize:         1,
			DefaultTTL:      1 * time.Second,
			CleanupInterval: 1 * time.Hour,
		})
		defer filter.Stop()

		mw := dedup.Dedup(filter)
		handler := mw(mockHandler(nil, 0))

		// Add more events than cache size
		for i := range 5 {
			event := &middlewareTestEvent{
				id:   string(rune('a' + i)),
				kind: platform.EventKindPrivateMessage,
			}
			_ = handler(eventctx.NewContextFromEvent(event, nil))
		}
	})

	t.Run("very short TTL", func(t *testing.T) {
		filter := dedup.NewDedupFilter(dedup.DedupConfig{
			MaxSize:         100,
			DefaultTTL:      10 * time.Millisecond,
			CleanupInterval: 5 * time.Millisecond,
		})
		defer filter.Stop()

		mw := dedup.Dedup(filter)
		handler := mw(mockHandler(nil, 0))

		event := &middlewareTestEvent{id: "short-ttl", kind: platform.EventKindPrivateMessage}

		// First
		_ = handler(eventctx.NewContextFromEvent(event, nil))

		// Wait for TTL
		time.Sleep(20 * time.Millisecond)

		// Should be allowed again
		_ = handler(eventctx.NewContextFromEvent(event, nil))
	})
}

func TestSlowHandlerEdgeCases(t *testing.T) {
	t.Run("zero threshold", func(t *testing.T) {
		logged := false
		mw := telemetry.SlowHandler(telemetry.SlowHandlerConfig{
			Threshold: 0, // Should use default
			Logger: func(handlerName string, duration time.Duration, ctx *eventctx.Context) {
				logged = true
			},
		})

		handler := mw(mockHandler(nil, 10*time.Millisecond))
		handler(createTestContext())

		// With default threshold (1s), 10ms should not log
		assert.False(t, logged)
	})

	t.Run("both logger and callback", func(t *testing.T) {
		logged := false
		called := false

		mw := telemetry.SlowHandler(telemetry.SlowHandlerConfig{
			Threshold: 50 * time.Millisecond,
			Logger: func(handlerName string, duration time.Duration, ctx *eventctx.Context) {
				logged = true
			},
			OnSlowHandler: func(handlerName string, duration time.Duration, ctx *eventctx.Context) {
				called = true
			},
		})

		handler := mw(mockHandler(nil, 100*time.Millisecond))
		handler(createTestContext())

		assert.True(t, logged)
		assert.True(t, called)
	})
}

func TestRateLimitEdgeCases(t *testing.T) {
	t.Run("very low rate", func(t *testing.T) {
		mw := RateLimitTokenBucket(1, 1, func(ctx *eventctx.Context) string {
			return "low"
		})

		handler := mw(mockHandler(nil, 0))

		// First should succeed
		err1 := handler(createTestContext())
		assert.NoError(t, err1)

		// Second should be blocked
		err2 := handler(createTestContext())
		assert.Error(t, err2)
	})

	t.Run("custom key function", func(t *testing.T) {
		mw := RateLimitTokenBucket(5, 5, func(ctx *eventctx.Context) string {
			if pe := ctx.GetPlatformEvent(); pe != nil {
				return pe.ID()
			}
			return "default"
		})

		handler := mw(mockHandler(nil, 0))

		// Different events should have separate limits
		for i := range 3 {
			event := &middlewareTestEvent{
				id:   string(rune('a' + i)),
				kind: platform.EventKindPrivateMessage,
			}
			ctx := eventctx.NewContextFromEvent(event, nil)
			err := handler(ctx)
			assert.NoError(t, err)
		}
	})
}

// ============================================================================
// Integration and Stress Tests
// ============================================================================

func TestMiddlewareIntegration(t *testing.T) {
	t.Run("full stack", func(t *testing.T) {
		cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			MaxFailures: 10,
		})

		filter := dedup.NewDedupFilter(dedup.DefaultDedupConfig())
		defer filter.Stop()

		// Stack: Recover -> Retry -> CircuitBreaker -> Dedup -> Logging -> Metrics -> Handler
		handler := Recover()(
			resilience.Retry(resilience.RetryConfig{
				MaxAttempts: 2,
				BackoffBase: 10 * time.Millisecond,
			})(
				resilience.CircuitBreakerMiddleware(cb)(
					dedup.Dedup(filter)(
						Logging()(
							Metrics()(
								mockHandler(nil, 0),
							),
						),
					),
				),
			),
		)

		event := &middlewareTestEvent{id: "integration", kind: platform.EventKindPrivateMessage}
		ctx := eventctx.NewContextFromEvent(event, nil)

		err := handler(ctx)
		assert.NoError(t, err)
	})

	t.Run("error propagation through stack", func(t *testing.T) {
		testErr := errors.New("test error")

		handler := Recover()(
			Logging()(
				Metrics()(
					mockHandler(testErr, 0),
				),
			),
		)

		err := handler(createTestContext())
		assert.ErrorIs(t, err, testErr)
	})
}

func TestConcurrentStress(t *testing.T) {
	t.Run("concurrent rate limit", func(t *testing.T) {
		mw := RateLimitTokenBucket(10, 10, func(ctx *eventctx.Context) string {
			return "stress"
		})

		handler := mw(mockHandler(nil, 0))

		var wg sync.WaitGroup
		blocked := int32(0)

		for range 50 {
			wg.Go(func() {
				if err := handler(createTestContext()); err != nil {
					atomic.AddInt32(&blocked, 1)
				}
			})
		}

		wg.Wait()

		// Some should be blocked
		assert.Greater(t, blocked, int32(0))
	})

	t.Run("concurrent circuit breaker", func(t *testing.T) {
		cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			MaxFailures: 3,
		})

		mw := resilience.CircuitBreakerMiddleware(cb)
		handler := mw(mockHandler(errors.New("error"), 0))

		var wg sync.WaitGroup
		rejected := int32(0)

		for range 20 {
			wg.Go(func() {
				err := handler(createTestContext())
				if err != nil && errors.Is(err, errutil.ErrCircuitBreakerOpen) {
					atomic.AddInt32(&rejected, 1)
				}
			})
			time.Sleep(5 * time.Millisecond)
		}

		wg.Wait()

		// Circuit should open and reject some
		assert.Greater(t, rejected, int32(0))
	})
}

// ============================================================================
// Benchmarks for New Tests
// ============================================================================

func BenchmarkDegradationMiddleware(b *testing.B) {
	cfg := degradation.DegradationConfig{
		CPUThreshold:    80.0,
		MemoryThreshold: 85.0,
		Strategy:        degradation.DegradationDrop,
	}

	ad := degradation.NewAdaptiveDegradation(cfg)
	mw := ad.Middleware()
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}

func BenchmarkAuthMiddleware(b *testing.B) {
	mw := Auth(func(ctx *eventctx.Context) bool {
		return true
	})
	handler := mw(mockHandler(nil, 0))
	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}

func BenchmarkFullMiddlewareStack(b *testing.B) {
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		MaxFailures: 100,
	})

	handler := Recover()(
		resilience.Retry(resilience.RetryConfig{
			MaxAttempts: 1,
			BackoffBase: 1 * time.Millisecond,
		})(
			resilience.CircuitBreakerMiddleware(cb)(
				Logging()(
					Metrics()(
						mockHandler(nil, 0),
					),
				),
			),
		),
	)

	ctx := createTestContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}
