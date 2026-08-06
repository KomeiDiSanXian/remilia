package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureEvent 是 platform.Event 的最小测试桩，带有可配置的 ID。
type fixtureEvent struct{ id string }

func (e *fixtureEvent) Platform() string                          { return "test" }
func (e *fixtureEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *fixtureEvent) RawType() string                           { return "TEST_EVENT" }
func (e *fixtureEvent) Content() string                           { return "" }
func (e *fixtureEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{} }
func (e *fixtureEvent) Sender() platform.UserInfo                 { return platform.UserInfo{} }
func (e *fixtureEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *fixtureEvent) ID() string                                { return e.id }
func (e *fixtureEvent) RawPayload() any                           { return nil }
func (e *fixtureEvent) Attachments() []platform.Attachment { return nil }

func newFixtureEvent(id string) platform.Event { return &fixtureEvent{id: id} }

// TestBotConcurrentStart tests that concurrent Start() calls don't cause race conditions
func TestBotConcurrentStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := engine.NewEngine()
		adapter := &mockAdapter{}
		bot := remilia.MustNewBot(adapter, eng)

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
	})
}

// TestDedupStrictMode tests the strict mode behavior of dedup filter
func TestDedupStrictMode(t *testing.T) {
	t.Run("StrictMode=false allows events when cache is full", func(t *testing.T) {
		filter := dedup.NewDedupFilter(dedup.DedupConfig{
			MaxSize:         2,
			DefaultTTL:      time.Minute,
			CleanupInterval: time.Hour, // Disable auto cleanup
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

	t.Run("DedupWithRejectMiddleware rejects events when cache is full", func(t *testing.T) {
		filter := dedup.NewDedupFilter(dedup.DedupConfig{
			MaxSize:         2,
			DefaultTTL:      time.Minute,
			CleanupInterval: time.Hour,
		})
		defer filter.Stop()

		mw := dedup.DedupWithReject(filter)
		handler := mw(func(ctx *eventctx.Context) error {
			return nil
		})

		// Fill cache
		err := handler(eventctx.NewContextFromEvent(
			newFixtureEvent("event1"), &platform.NoopSender{}))
		require.NoError(t, err)

		err = handler(eventctx.NewContextFromEvent(
			newFixtureEvent("event2"), &platform.NoopSender{}))
		require.NoError(t, err)

		// Cache is full, strict mode should reject
		err = handler(eventctx.NewContextFromEvent(
			newFixtureEvent("event3"), &platform.NoopSender{}))
		assert.Error(t, err, "Should reject event in strict mode when cache is full")
	})
}

// TestCircuitBreakerHalfOpenConcurrency tests the half-open state concurrency fix
//
// v1.21.1 起 SuccessThreshold 会被钳制到 ≤ HalfOpenMaxRequests，
// 无法再用"超高阈值"阻止熔断器闭合——顺序完成的成功探测在槽位耗尽前
// 必然已闭合。因此改用"在途探测占住槽位"的方式验证并发限流：
// 被放行的 handler 阻塞在 release 通道上（模拟未完成的探测请求），
// 其余请求在此期间到达即被槽位上限拒绝，结果完全确定。
func TestCircuitBreakerHalfOpenConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			MaxFailures:         1,
			ResetTimeout:        100 * time.Millisecond,
			HalfOpenMaxRequests: 3,
			SuccessThreshold:    3,
		})

		// Trigger circuit breaker to open
		cb.Reset()
		mw := resilience.CircuitBreakerMiddleware(cb)
		failHandler := mw(func(ctx *eventctx.Context) error {
			return assert.AnError
		})

		for range 2 {
			_ = failHandler(&eventctx.Context{})
		}

		// Verify circuit is open
		require.Equal(t, resilience.StateOpen, cb.GetState(), "Circuit should be open after failures")

		// Wait for reset timeout to enter half-open state
		time.Sleep(150 * time.Millisecond)

		// Prepare concurrent requests
		var wg sync.WaitGroup
		var startBarrier sync.WaitGroup
		allowedCount := atomic.Int32{}
		rejectedCount := atomic.Int32{}
		release := make(chan struct{}) // 在途探测的放行门

		startBarrier.Add(1) // Barrier to ensure all goroutines start at the same time

		for range 10 {
			wg.Go(func() {

				// Wait for barrier to ensure concurrent execution
				startBarrier.Wait()

				testMw := resilience.CircuitBreakerMiddleware(cb)
				handler := testMw(func(ctx *eventctx.Context) error {
					// 占住半开槽位直到统一放行，模拟尚未完成的在途探测
					<-release
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

		// 等待泡内所有 goroutine 稳定：3 个被放行的探测阻塞在 release 上，
		// 其余 7 个已被槽位上限拒绝并结束
		synctest.Wait()
		require.Equal(t, int32(7), rejectedCount.Load(),
			"exactly HalfOpenMaxRequests probes admitted, the rest rejected")
		require.Equal(t, resilience.StateHalfOpen, cb.GetState(),
			"breaker must stay half-open while probes are in flight")

		// 放行在途探测：3 次成功达到阈值，熔断器闭合
		close(release)
		wg.Wait()

		allowed := allowedCount.Load()
		rejected := rejectedCount.Load()

		// At most HalfOpenMaxRequests (3) should be allowed
		assert.LessOrEqual(t, allowed, int32(3),
			"Should not exceed HalfOpenMaxRequests, got %d allowed", allowed)
		assert.GreaterOrEqual(t, rejected, int32(7),
			"At least 7 requests should be rejected, got %d rejected", rejected)
		assert.Equal(t, resilience.StateClosed, cb.GetState(),
			"breaker should close after in-flight probes succeed")
	})
}

// Mock adapter for testing
type mockAdapter struct {
	started        bool
	mu             sync.Mutex
	startCallCount atomic.Int32
}

func (m *mockAdapter) Platform() string { return "test" }

func (m *mockAdapter) Start(_ context.Context, _ func(platform.Event)) error {
	m.startCallCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *mockAdapter) Stop(_ context.Context) error { return nil }
func (m *mockAdapter) IsRunning() bool              { return false }

func (m *mockAdapter) Sender() platform.Sender { return &platform.NoopSender{} }

func (m *mockAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}

func (m *mockAdapter) GetStartCallCount() int32 {
	return m.startCallCount.Load()
}
