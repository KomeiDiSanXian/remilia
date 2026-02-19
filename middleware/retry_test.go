package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetry_Basic 测试基本重试功能
func TestRetry_Basic(t *testing.T) {
	t.Run("success_without_retry", func(t *testing.T) {
		var callCount atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			callCount.Add(1)
			return nil
		}

		cfg := RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
			BackoffMax:  100 * time.Millisecond,
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
		err := wrappedHandler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int32(1), callCount.Load(), "Should call handler once")
	})

	t.Run("retry_until_success", func(t *testing.T) {
		var callCount atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			count := callCount.Add(1)
			if count < 3 {
				return errors.New("temporary error")
			}
			return nil
		}

		cfg := RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 10 * time.Millisecond,
			BackoffMax:  100 * time.Millisecond,
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
		err := wrappedHandler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, int32(3), callCount.Load(), "Should retry until success")
	})

	t.Run("max_attempts_reached", func(t *testing.T) {
		var callCount atomic.Int32
		testErr := errors.New("persistent error")

		handler := func(ctx *eventctx.Context) error {
			callCount.Add(1)
			return testErr
		}

		cfg := RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
			BackoffMax:  100 * time.Millisecond,
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
		err := wrappedHandler(ctx)

		assert.Error(t, err)
		assert.Equal(t, testErr, err)
		assert.Equal(t, int32(4), callCount.Load(), "Should try initial + 3 retries")
	})
}

// TestRetry_ContextCancellation 测试 context 取消时的行为
func TestRetry_ContextCancellation(t *testing.T) {
	t.Run("cancel_during_retry", func(t *testing.T) {
		var callCount atomic.Int32

		handler := func(ctx *eventctx.Context) error {
			callCount.Add(1)
			return errors.New("error")
		}

		cfg := RetryConfig{
			MaxAttempts: 10,
			BackoffBase: 100 * time.Millisecond,
			BackoffMax:  1 * time.Second,
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		// 创建可取消的 context
		stdCtx, cancel := context.WithCancel(context.Background())
		ctx := eventctx.NewContextWithContext(stdCtx, &dto.Payload{Type: "test"}, nil)

		// 在第一次失败后取消 context
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := wrappedHandler(ctx)

		assert.Error(t, err)
		assert.True(t, engine.IsBlockError(err), "Should return BlockError on cancel")
		// 应该只调用一次（初始尝试），因为在重试等待期间被取消
		assert.Equal(t, int32(1), callCount.Load(), "Should stop retrying after cancel")
	})
}

// TestRetry_BackoffExponential 测试指数退避
func TestRetry_BackoffExponential(t *testing.T) {
	var callCount atomic.Int32
	var callTimes []time.Time

	handler := func(ctx *eventctx.Context) error {
		callCount.Add(1)
		callTimes = append(callTimes, time.Now())
		return errors.New("error")
	}

	cfg := RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 50 * time.Millisecond,
		BackoffMax:  500 * time.Millisecond,
	}

	mw := Retry(cfg)
	wrappedHandler := mw(handler)

	ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
	_ = wrappedHandler(ctx)

	assert.Equal(t, int32(4), callCount.Load(), "Should try initial + 3 retries")
	require.Len(t, callTimes, 4)

	// 验证退避时间
	// 第1次重试: ~50ms (50 * 2^0)
	// 第2次重试: ~100ms (50 * 2^1)
	// 第3次重试: ~200ms (50 * 2^2)

	delay1 := callTimes[1].Sub(callTimes[0])
	delay2 := callTimes[2].Sub(callTimes[1])
	delay3 := callTimes[3].Sub(callTimes[2])

	assert.True(t, delay1 >= 40*time.Millisecond && delay1 <= 70*time.Millisecond, "First delay should be ~50ms")
	assert.True(t, delay2 >= 90*time.Millisecond && delay2 <= 120*time.Millisecond, "Second delay should be ~100ms")
	assert.True(t, delay3 >= 190*time.Millisecond && delay3 <= 220*time.Millisecond, "Third delay should be ~200ms")
}

// TestRetry_BackoffMax 测试最大退避时间限制
func TestRetry_BackoffMax(t *testing.T) {
	var callTimes []time.Time

	handler := func(ctx *eventctx.Context) error {
		callTimes = append(callTimes, time.Now())
		return errors.New("error")
	}

	cfg := RetryConfig{
		MaxAttempts: 5,
		BackoffBase: 100 * time.Millisecond,
		BackoffMax:  150 * time.Millisecond, // 限制最大退避时间
	}

	mw := Retry(cfg)
	wrappedHandler := mw(handler)

	ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
	_ = wrappedHandler(ctx)

	require.Len(t, callTimes, 6) // initial + 5 retries

	// 后续的延迟应该不超过 BackoffMax
	for i := 3; i < len(callTimes); i++ {
		delay := callTimes[i].Sub(callTimes[i-1])
		assert.True(t, delay <= 160*time.Millisecond, "Delay should not exceed BackoffMax")
	}
}

// TestRetry_ShouldRetry 测试自定义重试条件
func TestRetry_ShouldRetry(t *testing.T) {
	t.Run("skip_non_retryable_error", func(t *testing.T) {
		var callCount atomic.Int32
		blockErr := engine.NewBlockError("non-retryable")

		handler := func(ctx *eventctx.Context) error {
			callCount.Add(1)
			return blockErr
		}

		cfg := RetryConfig{
			MaxAttempts: 5,
			BackoffBase: 10 * time.Millisecond,
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
		err := wrappedHandler(ctx)

		assert.Error(t, err)
		assert.Equal(t, blockErr, err)
		assert.Equal(t, int32(1), callCount.Load(), "Should not retry BlockError")
	})

	t.Run("custom_should_retry", func(t *testing.T) {
		var callCount atomic.Int32
		specialErr := errors.New("special error")
		normalErr := errors.New("normal error")

		handler := func(ctx *eventctx.Context) error {
			count := callCount.Add(1)
			if count == 1 {
				return specialErr
			}
			return normalErr
		}

		cfg := RetryConfig{
			MaxAttempts: 3,
			BackoffBase: 10 * time.Millisecond,
			ShouldRetry: func(err error) bool {
				// 只重试 normalErr
				return errors.Is(err, normalErr)
			},
		}

		mw := Retry(cfg)
		wrappedHandler := mw(handler)

		ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
		err := wrappedHandler(ctx)

		assert.Error(t, err)
		assert.Equal(t, specialErr, err)
		assert.Equal(t, int32(1), callCount.Load(), "Should not retry special error")
	})
}

// TestRetry_RetryAttemptTracking 测试重试次数追踪
func TestRetry_RetryAttemptTracking(t *testing.T) {
	attempts := make([]int, 0)

	handler := func(ctx *eventctx.Context) error {
		attempt, ok := ctx.GetRetryAttempt()
		assert.True(t, ok, "Retry attempt should be set")
		attempts = append(attempts, attempt)

		if attempt < 2 {
			return errors.New("error")
		}
		return nil
	}

	cfg := RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
	}

	mw := Retry(cfg)
	wrappedHandler := mw(handler)

	ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
	err := wrappedHandler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, []int{0, 1, 2}, attempts, "Should track retry attempts correctly")
}

// TestSleepWithContext_ResourceCleanup 测试 sleepWithContext 的资源清理
func TestSleepWithContext_ResourceCleanup(t *testing.T) {
	t.Run("normal_completion", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now()

		result := sleepWithContext(ctx, 50*time.Millisecond)

		elapsed := time.Since(start)
		assert.True(t, result, "Should return true on normal completion")
		assert.True(t, elapsed >= 40*time.Millisecond && elapsed <= 70*time.Millisecond, "Should sleep for correct duration")
	})

	t.Run("context_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// 提前取消
		cancel()

		start := time.Now()
		result := sleepWithContext(ctx, 1*time.Second)
		elapsed := time.Since(start)

		assert.False(t, result, "Should return false on cancel")
		assert.True(t, elapsed < 100*time.Millisecond, "Should return immediately on cancel")
	})

	t.Run("context_canceled_during_sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// 在 sleep 期间取消
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		result := sleepWithContext(ctx, 1*time.Second)
		elapsed := time.Since(start)

		assert.False(t, result, "Should return false on cancel")
		assert.True(t, elapsed >= 40*time.Millisecond && elapsed <= 100*time.Millisecond, "Should return when canceled")
	})

	t.Run("nil_context", func(t *testing.T) {
		start := time.Now()

		result := sleepWithContext(nil, 50*time.Millisecond)

		elapsed := time.Since(start)
		assert.True(t, result, "Should return true with nil context")
		assert.True(t, elapsed >= 40*time.Millisecond && elapsed <= 70*time.Millisecond, "Should sleep for correct duration")
	})

	t.Run("no_timer_leak", func(t *testing.T) {
		// 测试大量调用不会泄漏 timer
		ctx := context.Background()

		for range 1000 {
			sleepWithContext(ctx, 1*time.Millisecond)
		}

		// 如果有 timer 泄漏，这会导致内存增长
		// 这个测试主要确保 defer timer.Stop() 被调用
	})
}

// TestRetry_ConcurrentRetries 测试并发重试
func TestRetry_ConcurrentRetries(t *testing.T) {
	var totalCalls atomic.Int32

	handler := func(ctx *eventctx.Context) error {
		totalCalls.Add(1)
		return errors.New("error")
	}

	cfg := RetryConfig{
		MaxAttempts: 2,
		BackoffBase: 10 * time.Millisecond,
	}

	mw := Retry(cfg)
	wrappedHandler := mw(handler)

	// 并发执行多个重试
	const concurrency = 10
	done := make(chan bool, concurrency)

	for range concurrency {
		go func() {
			ctx := eventctx.NewContext(&dto.Payload{Type: "test"}, nil)
			_ = wrappedHandler(ctx)
			done <- true
		}()
	}

	// 等待所有完成
	for range concurrency {
		<-done
	}

	// 每个 goroutine 应该执行 3 次（initial + 2 retries）
	assert.Equal(t, int32(concurrency*3), totalCalls.Load(), "Should handle concurrent retries correctly")
}
