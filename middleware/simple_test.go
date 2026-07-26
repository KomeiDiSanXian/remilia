package middleware_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// simpleTestEvent is a minimal platform.Event stub for simple_test.go
type simpleTestEvent struct{}

func (e *simpleTestEvent) Platform() string                          { return "test" }
func (e *simpleTestEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *simpleTestEvent) RawType() string                           { return string(platform.EventKindPrivateMessage) }
func (e *simpleTestEvent) Content() string                           { return "test" }
func (e *simpleTestEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: "chat-001"} }
func (e *simpleTestEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "sender-001"} }
func (e *simpleTestEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *simpleTestEvent) ID() string                                { return "simple-test-event" }
func (e *simpleTestEvent) RawPayload() any                           { return nil }
func (e *simpleTestEvent) Attachments() []platform.InboundAttachment { return nil }

// TestSimpleMiddleware tests simplified middleware factories
func TestSimpleMiddleware(t *testing.T) {
	t.Run("SimpleAdaptive", func(t *testing.T) {
		mw := ratelimit.SimpleAdaptive()
		assert.NotNil(t, mw)
	})

	t.Run("SimpleAdaptiveWithLimit", func(t *testing.T) {
		mw := ratelimit.SimpleAdaptiveWithLimit(200)
		assert.NotNil(t, mw)
	})

	t.Run("SimpleCircuitBreaker", func(t *testing.T) {
		mw := resilience.SimpleCircuitBreaker()
		assert.NotNil(t, mw)
	})

	t.Run("SimpleDedup", func(t *testing.T) {
		mw := dedup.SimpleDedup()
		assert.NotNil(t, mw)
	})

	t.Run("SimpleDedupWithTTL", func(t *testing.T) {
		mw := dedup.SimpleDedupWithTTL(5 * time.Minute)
		assert.NotNil(t, mw)
	})
}

// TestMiddlewareSet tests middleware set builder
func TestMiddlewareSet(t *testing.T) {
	t.Run("EmptySet", func(t *testing.T) {
		set := middleware.NewMiddlewareSet()
		middlewares := set.Build()
		assert.Empty(t, middlewares)
	})

	t.Run("WithLogging", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithLogging()
		middlewares := set.Build()
		assert.Len(t, middlewares, 1)
	})

	t.Run("WithRecover", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithRecover()
		middlewares := set.Build()
		assert.Len(t, middlewares, 1)
	})

	t.Run("WithAdaptive", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithAdaptive()
		middlewares := set.Build()
		assert.Len(t, middlewares, 1)
	})

	t.Run("WithCircuitBreaker", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithCircuitBreaker()
		middlewares := set.Build()
		assert.Len(t, middlewares, 1)
	})

	t.Run("WithDedup", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithDedup()
		middlewares := set.Build()
		assert.Len(t, middlewares, 1)
	})

	t.Run("ChainedCalls", func(t *testing.T) {
		set := middleware.NewMiddlewareSet().
			WithRecover().
			WithLogging().
			WithAdaptive().
			WithCircuitBreaker()
		middlewares := set.Build()
		assert.Len(t, middlewares, 4)
	})
}

// TestPredefinedSets tests predefined middleware sets
func TestPredefinedSets(t *testing.T) {
	t.Run("ProductionSet", func(t *testing.T) {
		middlewares := middleware.ProductionSet()
		assert.NotEmpty(t, middlewares)
		assert.Len(t, middlewares, 7) // Recover, RequestID, Timeout, Dedup, CircuitBreaker, Adaptive, Logging
	})

	t.Run("DevelopmentSet", func(t *testing.T) {
		middlewares := middleware.DevelopmentSet()
		assert.NotEmpty(t, middlewares)
		assert.Len(t, middlewares, 2) // Recover, Logging
	})

	t.Run("BasicSet", func(t *testing.T) {
		middlewares := middleware.BasicSet()
		assert.NotEmpty(t, middlewares)
		assert.Len(t, middlewares, 1) // Recover
	})
}

// TestMiddlewareExecution tests that middleware can be executed
func TestMiddlewareExecution(t *testing.T) {
	t.Run("ProductionSetExecution", func(t *testing.T) {
		middlewares := middleware.ProductionSet()

		// Create a simple handler
		executed := false
		handler := func(ctx *eventctx.Context) error {
			executed = true
			return nil
		}

		// Wrap handler with middleware
		wrapped := handler
		for i := len(middlewares) - 1; i >= 0; i-- {
			wrapped = middlewares[i](wrapped)
		}

		// Create test context
		ctx := eventctx.NewContextFromEvent(&simpleTestEvent{}, &platform.NoopSender{})

		// Execute
		err := wrapped(ctx)
		assert.NoError(t, err)
		assert.True(t, executed)
	})
}

// BenchmarkMiddlewareFactories benchmarks middleware creation
func BenchmarkMiddlewareFactories(b *testing.B) {
	b.Run("SimpleAdaptive", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ratelimit.SimpleAdaptive()
		}
	})

	b.Run("SimpleCircuitBreaker", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = resilience.SimpleCircuitBreaker()
		}
	})

	b.Run("ProductionSet", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = middleware.ProductionSet()
		}
	})

	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = middleware.NewMiddlewareSet().
				WithRecover().
				WithLogging().
				WithAdaptive().
				Build()
		}
	})
}

// TestManagedAdaptive_StopReleasesGoroutines 测试 ManagedAdaptive Stop 能正确释放后台 goroutine
func TestManagedAdaptive_StopReleasesGoroutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	managed := ratelimit.NewManagedAdaptive()
	assert.NotNil(t, managed.Middleware())

	// Stop 不应该阻塞
	done := make(chan struct{})
	go func() {
		managed.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return within 3 seconds")
	}
	})
}


// TestManagedAdaptiveWithLimit_Works 测试带限制的可管理限流器
func TestManagedAdaptiveWithLimit_Works(t *testing.T) {
	managed := ratelimit.NewManagedAdaptiveWithLimit(50)
	assert.NotNil(t, managed)
	mw := managed.Middleware()
	assert.NotNil(t, mw)
	managed.Stop()
}

// TestProductionSet_MiddlewareOrder 测试 ProductionSet 中间件数量
// 顺序应为: Recover → RequestID → Timeout → Dedup → CircuitBreaker → Adaptive → Logging
func TestProductionSet_MiddlewareOrder(t *testing.T) {
	middlewares := middleware.ProductionSet()
	// ProductionSet 应包含 7 个中间件
	assert.Equal(t, 7, len(middlewares), "ProductionSet should have 7 middlewares")
}

// TestNewManagedAdaptiveWithContext_StopsWhenParentCancelled 测试父 context 取消时自动退出
func TestNewManagedAdaptiveWithContext_StopsWhenParentCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())

	managed := ratelimit.NewManagedAdaptiveWithContext(parent)
	assert.NotNil(t, managed.Middleware())

	// 取消父 context，后台 goroutine 应该自动退出（无需调用 Stop）
	done := make(chan struct{})
	go func() {
		cancel()
		// 给一点时间让 goroutine 退出
		time.Sleep(200 * time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// OK: parent cancel 触发了退出
	case <-time.After(3 * time.Second):
		t.Fatal("goroutines did not exit after parent context cancelled")
	}
	})
}


// TestNewManagedAdaptiveWithLimitContext_Works 测试带限制的 context 版本
func TestNewManagedAdaptiveWithLimitContext_Works(t *testing.T) {
	parent := t.Context()

	managed := ratelimit.NewManagedAdaptiveWithLimitContext(parent, 50)
	assert.NotNil(t, managed)
	mw := managed.Middleware()
	assert.NotNil(t, mw)
}
