package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBotConcurrentStart tests that concurrent Start() calls don't cause race conditions
func TestBotConcurrentStart(t *testing.T) {
	eng := engine.NewEngine()
	adapter := &mockAdapter{}
	bot := remilia.NewBot(adapter, eng)

	var wg sync.WaitGroup
	errors := make([]error, 10)

	// Try to start bot concurrently from 10 goroutines
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = bot.Start()
		}(i)
	}

	wg.Wait()

	// Give some time for the lifecycle to actually call adapter.Start() in its goroutine
	time.Sleep(100 * time.Millisecond)

	// Check that the adapter's Start was called exactly once
	startCallCount := adapter.GetStartCallCount()
	assert.Equal(t, int32(1), startCallCount, "Adapter Start() should be called exactly once")
	assert.True(t, bot.IsRunning(), "Bot should be running")

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = bot.Stop(ctx)
}

// TestDedupStrictMode tests the strict mode behavior of dedup filter
func TestDedupStrictMode(t *testing.T) {
	t.Run("StrictMode=false allows events when cache is full", func(t *testing.T) {
		filter := middleware.NewDedupFilter(middleware.DedupConfig{
			MaxSize:         2,
			DefaultTTL:      time.Minute,
			CleanupInterval: time.Hour, // Disable auto cleanup
			StrictMode:      false,
		})
		defer filter.Stop()

		// Fill cache
		_, err := filter.CheckDuplicate("event1")
		require.NoError(t, err)
		_, err = filter.CheckDuplicate("event2")
		require.NoError(t, err)

		// Cache is now full, next event should be allowed (but with error)
		isDup, err := filter.CheckDuplicate("event3")
		assert.Error(t, err, "Should return error when cache is full")
		assert.False(t, isDup, "Should not be marked as duplicate")
	})

	t.Run("StrictMode=true rejects events when cache is full", func(t *testing.T) {
		filter := middleware.NewDedupFilter(middleware.DedupConfig{
			MaxSize:         2,
			DefaultTTL:      time.Minute,
			CleanupInterval: time.Hour,
			StrictMode:      true,
		})
		defer filter.Stop()

		mw := middleware.Dedup(filter)
		handler := mw(func(ctx *eventctx.Context) error {
			return nil
		})

		// Fill cache
		event1 := &dto.Payload{ID: "event1"}
		ctx1 := eventctx.NewContext(event1, nil)
		err := handler(ctx1)
		require.NoError(t, err)

		event2 := &dto.Payload{ID: "event2"}
		ctx2 := eventctx.NewContext(event2, nil)
		err = handler(ctx2)
		require.NoError(t, err)

		// Cache is full, strict mode should reject
		event3 := &dto.Payload{ID: "event3"}
		ctx3 := eventctx.NewContext(event3, nil)
		err = handler(ctx3)
		assert.Error(t, err, "Should reject event in strict mode when cache is full")
	})
}

// TestCircuitBreakerHalfOpenConcurrency tests the half-open state concurrency fix
func TestCircuitBreakerHalfOpenConcurrency(t *testing.T) {
	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		MaxFailures:         1,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 3,
		SuccessThreshold:    100, // High threshold to prevent early transition to Closed
	})

	// Trigger circuit breaker to open
	cb.Reset()
	mw := middleware.CircuitBreakerMiddleware(cb)
	failHandler := mw(func(ctx *eventctx.Context) error {
		return assert.AnError
	})

	for range 2 {
		_ = failHandler(&eventctx.Context{})
	}

	// Verify circuit is open
	require.Equal(t, middleware.StateOpen, cb.GetState(), "Circuit should be open after failures")

	// Wait for reset timeout to enter half-open state
	time.Sleep(150 * time.Millisecond)

	// Prepare concurrent requests
	var wg sync.WaitGroup
	var startBarrier sync.WaitGroup
	allowedCount := atomic.Int32{}
	rejectedCount := atomic.Int32{}

	startBarrier.Add(1) // Barrier to ensure all goroutines start at the same time

	for range 10 {
		wg.Go(func() {

			// Wait for barrier to ensure concurrent execution
			startBarrier.Wait()

			testMw := middleware.CircuitBreakerMiddleware(cb)
			handler := testMw(func(ctx *eventctx.Context) error {
				return nil // Success
			})

			err := handler(&eventctx.Context{})
			if err == nil {
				allowedCount.Add(1)
			} else {
				rejectedCount.Add(1)
			}
		})
	}

	// Release all goroutines at once
	startBarrier.Done()

	wg.Wait()

	allowed := allowedCount.Load()
	rejected := rejectedCount.Load()

	// At most HalfOpenMaxRequests (3) should be allowed
	assert.LessOrEqual(t, allowed, int32(3),
		"Should not exceed HalfOpenMaxRequests, got %d allowed", allowed)
	assert.GreaterOrEqual(t, rejected, int32(7),
		"At least 7 requests should be rejected, got %d rejected", rejected)
}

// Mock adapter for testing
type mockAdapter struct {
	started        bool
	mu             sync.Mutex
	startCallCount atomic.Int32
}

func (m *mockAdapter) Platform() string { return "test" }

func (m *mockAdapter) StartPlatform(_ context.Context, _ func(platform.Event)) error {
	m.startCallCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *mockAdapter) Stop(_ context.Context) error { return nil }

func (m *mockAdapter) Sender() platform.Sender { return &platform.NoopSender{} }

func (m *mockAdapter) GetStartCallCount() int32 {
	return m.startCallCount.Load()
}
