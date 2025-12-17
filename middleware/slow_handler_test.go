package middleware

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestSlowHandler tests slow handler detection
func TestSlowHandler(t *testing.T) {
	engine := remilia.NewEngine()

	var slowDetected int32
	var slowDuration time.Duration

	// Configure slow handler detector
	config := SlowHandlerConfig{
		Threshold: 100 * time.Millisecond,
		Logger: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			atomic.StoreInt32(&slowDetected, 1)
			slowDuration = duration
		},
	}

	engine.Use(SlowHandler(config))

	// Add a slow handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(150 * time.Millisecond) // Slower than threshold
		return nil
	})

	// Process event
	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// Verify slow handler was detected
	assert.Equal(t, int32(1), atomic.LoadInt32(&slowDetected))
	assert.GreaterOrEqual(t, slowDuration, 150*time.Millisecond)
}

// TestSlowHandler_BelowThreshold tests that fast handlers are not flagged
func TestSlowHandler_BelowThreshold(t *testing.T) {
	engine := remilia.NewEngine()

	var slowDetected int32

	config := SlowHandlerConfig{
		Threshold: 200 * time.Millisecond,
		Logger: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			atomic.StoreInt32(&slowDetected, 1)
		},
	}

	engine.Use(SlowHandler(config))

	// Add a fast handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(50 * time.Millisecond) // Faster than threshold
		return nil
	})

	// Process event
	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// Verify slow handler was NOT detected
	assert.Equal(t, int32(0), atomic.LoadInt32(&slowDetected))
}

// TestSlowHandler_OnSlowHandler tests custom callback
func TestSlowHandler_OnSlowHandler(t *testing.T) {
	engine := remilia.NewEngine()

	var callbackCalled int32
	var capturedHandler string

	config := SlowHandlerConfig{
		Threshold: 100 * time.Millisecond,
		Logger: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			// Default logger
		},
		OnSlowHandler: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			atomic.StoreInt32(&callbackCalled, 1)
			capturedHandler = handlerName
		},
	}

	engine.Use(SlowHandler(config))

	// Add a slow handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	})

	// Process event
	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// Verify callback was called
	assert.Equal(t, int32(1), atomic.LoadInt32(&callbackCalled))
	assert.NotEmpty(t, capturedHandler)
}

// TestSlowHandlerSimple tests the simple version
func TestSlowHandlerSimple(t *testing.T) {
	engine := remilia.NewEngine()

	// Use simple version with custom threshold
	engine.Use(SlowHandlerSimple(100 * time.Millisecond))

	// Add a slow handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	})

	// Process event (should log warning)
	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// No assertion - just ensure it doesn't panic
}

// TestSlowHandler_DefaultThreshold tests default threshold
func TestSlowHandler_DefaultThreshold(t *testing.T) {
	config := SlowHandlerConfig{
		// No threshold specified
	}

	mw := SlowHandler(config)
	assert.NotNil(t, mw)

	// Test that it uses default 1 second threshold
	engine := remilia.NewEngine()
	engine.Use(mw)

	var detected int32

	// Override logger to track detection
	config2 := SlowHandlerConfig{
		Threshold: 0, // Should default to 1 second
		Logger: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			atomic.StoreInt32(&detected, 1)
		},
	}

	engine2 := remilia.NewEngine()
	engine2.Use(SlowHandler(config2))

	// Add a handler that's slow but under 1 second
	engine2.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine2.ProcessEvent(ctx)

	// Should not detect (under 1 second)
	assert.Equal(t, int32(0), atomic.LoadInt32(&detected))
}

// BenchmarkSlowHandler tests overhead of slow handler middleware
func BenchmarkSlowHandler(b *testing.B) {
	engine := remilia.NewEngine()
	engine.Use(SlowHandlerSimple(1 * time.Second)) // High threshold

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		// Fast handler
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessEvent(ctx)
	}
}
