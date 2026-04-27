package middleware

import (
	"testing"
	"time"

	appconfig "github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlowHandler(t *testing.T) {
	t.Run("handler completes under threshold", func(t *testing.T) {
		callbackCalled := false

		mw := SlowHandler(SlowHandlerConfig{
			Threshold: 200 * time.Millisecond,
			OnSlowHandler: func(name string, d time.Duration, ctx *context.Context) {
				callbackCalled = true
			},
		})
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.False(t, callbackCalled, "should not trigger for fast handler")
	})

	t.Run("handler exceeds threshold triggers callback", func(t *testing.T) {
		callbackCalled := false
		var capturedDuration time.Duration
		var capturedName string

		mw := SlowHandler(SlowHandlerConfig{
			Threshold: 20 * time.Millisecond,
			OnSlowHandler: func(name string, d time.Duration, ctx *context.Context) {
				callbackCalled = true
				capturedName = name
				capturedDuration = d
			},
		})
		handler := mw(mockHandler(nil, 100*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		// SlowHandler 注入的 deadline 会使 handler 提前返回（约 20ms 后超时），
		// 但超时错误会被 SlowHandler 屏蔽，返回 nil。
		assert.NoError(t, err)
		assert.True(t, callbackCalled, "should trigger for slow handler")
		assert.Equal(t, ctx.GetEventType(), capturedName)
		// handler 被 deadline 打断，实际耗时接近 threshold 而非完整的 100ms
		assert.GreaterOrEqual(t, capturedDuration, 15*time.Millisecond)
		assert.Less(t, capturedDuration, 100*time.Millisecond)
	})

	t.Run("zero threshold defaults to 1s and does not trigger for fast handler", func(t *testing.T) {
		callCount := 0

		mw := SlowHandler(SlowHandlerConfig{
			Threshold: 0,
			OnSlowHandler: func(name string, d time.Duration, ctx *context.Context) {
				callCount++
			},
		})
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 0, callCount, "zero threshold should default to 1s")
	})

	t.Run("very small threshold triggers for fast handler", func(t *testing.T) {
		callCount := 0

		mw := SlowHandler(SlowHandlerConfig{
			Threshold: 1,
			OnSlowHandler: func(name string, d time.Duration, ctx *context.Context) {
				callCount++
			},
		})
		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 1, callCount, "1ns threshold should trigger")
	})
}

func TestSlowHandlerSimple(t *testing.T) {
	t.Run("creates valid middleware", func(t *testing.T) {
		mw := SlowHandlerSimple(50 * time.Millisecond)
		require.NotNil(t, mw)

		handler := mw(mockHandler(nil, 10*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})
}

func TestSlowHandlerFromConfig(t *testing.T) {
	t.Run("parses threshold from config", func(t *testing.T) {
		cfg := appconfig.MiddlewareConfig{
			SlowHandler: appconfig.SlowHandlerMiddlewareConfig{
				Threshold: "300ms",
			},
		}
		mw := SlowHandlerFromConfig(cfg)
		assert.NotNil(t, mw)
	})

	t.Run("default threshold with empty config", func(t *testing.T) {
		cfg := appconfig.MiddlewareConfig{}
		mw := SlowHandlerFromConfig(cfg)
		assert.NotNil(t, mw)
	})
}

func TestSlowHandlerCustomCallback(t *testing.T) {
	t.Run("callback receives correct handler name and duration", func(t *testing.T) {
		var (
			calledName     string
			calledDuration time.Duration
			calledCtx      *context.Context
		)

		mw := SlowHandler(SlowHandlerConfig{
			Threshold: 10 * time.Millisecond,
			OnSlowHandler: func(name string, d time.Duration, ctx *context.Context) {
				calledName = name
				calledDuration = d
				calledCtx = ctx
			},
		})
		handler := mw(mockHandler(nil, 80*time.Millisecond))

		ctx := createTestContext()
		err := handler(ctx)

		// handler 被 deadline 打断（~10ms），duration 接近 threshold 而非 80ms
		assert.NoError(t, err)
		assert.Equal(t, ctx.GetEventType(), calledName)
		assert.GreaterOrEqual(t, calledDuration, 8*time.Millisecond)
		assert.Less(t, calledDuration, 80*time.Millisecond)
		assert.Same(t, ctx, calledCtx)
	})
}
