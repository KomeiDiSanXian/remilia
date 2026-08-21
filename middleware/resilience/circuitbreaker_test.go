package resilience

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/middleware/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  5,
		ResetTimeout: time.Minute,
	})

	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(0), cb.GetFailures())

	stats := cb.Stats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, int32(0), stats.Failures)
	assert.Equal(t, int32(0), stats.Successes)
	assert.Equal(t, int32(0), stats.HalfOpenReqs)
	assert.True(t, stats.LastFailure.IsZero(), "LastFailure should be zero")
	assert.True(t, stats.HalfOpenStartTime.IsZero(), "HalfOpenStartTime should be zero")
}

func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, 5, cb.config.MaxFailures)
	assert.Equal(t, 30*time.Second, cb.config.ResetTimeout)
	assert.Equal(t, 1, cb.config.HalfOpenMaxRequests)
	assert.Equal(t, 1, cb.config.SuccessThreshold)
	assert.Equal(t, 10*time.Second, cb.config.HalfOpenTimeout)
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: time.Minute,
	})

	cb.onFailure()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(1), cb.GetFailures())

	cb.onFailure()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(2), cb.GetFailures())

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())
	assert.Equal(t, int32(3), cb.GetFailures())
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 1 * time.Millisecond,
	})

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))

	err := cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     1 * time.Millisecond,
		SuccessThreshold: 1,
	})

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	err := cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.onSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(0), cb.GetFailures())
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 1 * time.Millisecond,
	})

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	err := cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())
	assert.Equal(t, cb.config.MaxFailures, int(cb.GetFailures()))
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  5,
		ResetTimeout: time.Minute,
	})

	cb.onFailure()
	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, int32(3), cb.GetFailures())

	cb.onSuccess()
	assert.Equal(t, int32(0), cb.GetFailures())
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_SuccessThreshold(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 1 * time.Millisecond,
		// v1.21.1 起 SuccessThreshold 会被钳制到 HalfOpenMaxRequests（防止
		// 开↔半开无限震荡），因此必须显式给足半开槽位，阈值 3 才有效。
		HalfOpenMaxRequests: 3,
		SuccessThreshold:    3,
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	require.NoError(t, cb.canExecute())
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.onSuccess()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.onSuccess()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.onSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_HalfOpenTimeout(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         1,
		ResetTimeout:        1 * time.Millisecond,
		HalfOpenTimeout:     10 * time.Millisecond,
		HalfOpenMaxRequests: 10,
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))

	err := cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// Simulate HalfOpenTimeout elapsing
	cb.halfOpenStarted.Store(time.Now().Add(-20 * time.Millisecond))

	err = cb.canExecute()
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrCircuitBreakerOpen)
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_MiddlewareRejectsWhenOpen(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
	})

	mw := CircuitBreakerMiddleware(cb)
	handler := mw(testutil.MockHandler(errors.New("fail"), 0))
	ctx := testutil.CreateTestContext()

	err := handler(ctx)
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	err = handler(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrCircuitBreakerOpen)
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_MiddlewareHalfOpenMaxRequests(t *testing.T) {
	t.Parallel()
	// v1.21.1 起 SuccessThreshold 被钳制到 ≤ HalfOpenMaxRequests，
	// "成功完成后再发下一个"的顺序探测在槽位耗尽前必然已闭合熔断器。
	// 槽位上限如今约束的是**在途并发**探测：先用 canExecute 占满槽位
	// （模拟两个尚未完成的探测请求），再验证第三个请求被拒绝。
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         1,
		ResetTimeout:        1 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    2,
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))

	// 两个在途探测占满半开槽位（只获取执行许可，尚未完成）
	require.NoError(t, cb.canExecute())
	assert.Equal(t, StateHalfOpen, cb.GetState())
	require.NoError(t, cb.canExecute())
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 第三个请求经中间件进入：槽位耗尽，应被拒绝且不影响半开状态
	mw := CircuitBreakerMiddleware(cb)
	handler := mw(testutil.MockHandler(nil, 0))
	ctx := testutil.CreateTestContext()

	err := handler(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrCircuitBreakerHalfOpen)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 在途探测陆续成功：达到阈值后闭合
	cb.onSuccess()
	assert.Equal(t, StateHalfOpen, cb.GetState())
	cb.onSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
}

// TestCircuitBreaker_ClampSuccessThreshold 验证 v1.21.1 的钳制不变式：
// 构造与热更新时 SuccessThreshold 都不允许超过 HalfOpenMaxRequests。
func TestCircuitBreaker_ClampSuccessThreshold(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         1,
		ResetTimeout:        time.Minute,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    10,
	})
	assert.Equal(t, 2, cb.config.SuccessThreshold, "constructor should clamp SuccessThreshold")

	cb.UpdateConfig(CircuitBreakerConfig{SuccessThreshold: 5})
	assert.Equal(t, 2, cb.config.SuccessThreshold, "hot-reload should clamp SuccessThreshold")
}

func TestCircuitBreaker_Reset(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  2,
		ResetTimeout: time.Minute,
	})

	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())
	assert.Equal(t, int32(2), cb.GetFailures())

	cb.Reset()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(0), cb.GetFailures())

	stats := cb.Stats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, int32(0), stats.Failures)
}

func TestCircuitBreaker_UpdateConfig(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  5,
		ResetTimeout: time.Minute,
	})

	cb.UpdateConfig(CircuitBreakerConfig{
		MaxFailures: 2,
	})

	cb.onFailure()
	assert.Equal(t, StateClosed, cb.GetState())
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_UpdateConfig_ResetTimeout(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Hour,
	})

	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	cb.UpdateConfig(CircuitBreakerConfig{
		ResetTimeout: 1 * time.Millisecond,
	})

	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	err := cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())
}

func TestCircuitBreaker_UpdateConfig_ShrinkHalfOpenMaxRequests(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         1,
		ResetTimeout:        1 * time.Millisecond,
		HalfOpenMaxRequests: 10,
		SuccessThreshold:    10,
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	require.NoError(t, cb.canExecute())
	assert.Equal(t, StateHalfOpen, cb.GetState())

	cb.UpdateConfig(CircuitBreakerConfig{
		HalfOpenMaxRequests: 1,
	})

	err := cb.canExecute()
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrCircuitBreakerHalfOpen)
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		oldState CircuitBreakerState
		newState CircuitBreakerState
		called   bool
	)

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
		OnStateChange: func(from, to CircuitBreakerState) {
			mu.Lock()
			oldState = from
			newState = to
			called = true
			mu.Unlock()
		},
	})

	cb.onFailure()

	mu.Lock()
	assert.True(t, called)
	assert.Equal(t, StateClosed, oldState)
	assert.Equal(t, StateOpen, newState)
	mu.Unlock()
}

func TestCircuitBreaker_OnStateChange_HalfOpenToClosed(t *testing.T) {
	t.Parallel()
	var transitions []struct{ from, to CircuitBreakerState }
	var mu sync.Mutex

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 1 * time.Millisecond,
		OnStateChange: func(from, to CircuitBreakerState) {
			mu.Lock()
			transitions = append(transitions, struct{ from, to CircuitBreakerState }{from, to})
			mu.Unlock()
		},
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	_ = cb.canExecute()
	cb.onSuccess()

	mu.Lock()
	require.Len(t, transitions, 3)
	assert.Equal(t, StateClosed, transitions[0].from)
	assert.Equal(t, StateOpen, transitions[0].to)
	assert.Equal(t, StateOpen, transitions[1].from)
	assert.Equal(t, StateHalfOpen, transitions[1].to)
	assert.Equal(t, StateHalfOpen, transitions[2].from)
	assert.Equal(t, StateClosed, transitions[2].to)
	mu.Unlock()
}

func TestCircuitBreaker_Stats(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: time.Minute,
	})

	cb.onFailure()
	cb.onFailure()

	stats := cb.Stats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, int32(2), stats.Failures)
	assert.Equal(t, int32(0), stats.Successes)
	assert.Equal(t, int32(0), stats.HalfOpenReqs)
	assert.False(t, stats.LastFailure.IsZero(), "LastFailure should be set")
}

func TestCircuitBreaker_Stats_HalfOpen(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: 1 * time.Millisecond,
	})

	cb.onFailure()
	cb.lastFailure.Store(time.Now().Add(-10 * time.Millisecond))
	_ = cb.canExecute()

	stats := cb.Stats()
	assert.Equal(t, StateHalfOpen, stats.State)
	assert.Equal(t, int32(1), stats.HalfOpenReqs)
	assert.False(t, stats.HalfOpenStartTime.IsZero(), "HalfOpenStartTime should be set")
}

func TestCircuitBreaker_Middleware_PassesThroughClosed(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  5,
		ResetTimeout: time.Minute,
	})

	var called atomic.Int32
	mw := CircuitBreakerMiddleware(cb)
	handler := mw(func(ctx *eventctx.Context) error {
		called.Add(1)
		return nil
	})

	ctx := testutil.CreateTestContext()
	assert.NoError(t, handler(ctx))
	assert.Equal(t, int32(1), called.Load())
}

func TestCircuitBreaker_Middleware_ErrorPassesThroughToOnFailure(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
	})

	mw := CircuitBreakerMiddleware(cb)
	handler := mw(testutil.MockHandler(errors.New("handler error"), 0))
	ctx := testutil.CreateTestContext()

	err := handler(ctx)
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_Middleware_SuccessPassesThroughToOnSuccess(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  5,
		ResetTimeout: time.Minute,
	})

	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, int32(2), cb.GetFailures())

	mw := CircuitBreakerMiddleware(cb)
	handler := mw(testutil.MockHandler(nil, 0))
	ctx := testutil.CreateTestContext()

	assert.NoError(t, handler(ctx))
	assert.Equal(t, int32(0), cb.GetFailures())
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 20,
		SuccessThreshold:    3,
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			cb.canExecute()
		})
		wg.Go(func() {
			cb.onSuccess()
		})
		wg.Go(func() {
			cb.onFailure()
		})
		wg.Go(func() {
			cb.GetState()
		})
		wg.Go(func() {
			cb.GetFailures()
		})
		wg.Go(func() {
			cb.Stats()
		})
		wg.Go(func() {
			cb.Reset()
		})
	}

	wg.Wait()

	state := cb.GetState()
	valid := state == StateClosed || state == StateOpen || state == StateHalfOpen
	assert.True(t, valid, "unexpected state: %s", state)
}
