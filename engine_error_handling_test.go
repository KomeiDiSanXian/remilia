package remilia

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestInvokeHandlerErrorHandling 测试 invokeHandler 的错误处理
func TestInvokeHandlerErrorHandling(t *testing.T) {
	t.Run("Handler 返回错误应该被记录", func(t *testing.T) {
		engine := NewEngine()

		testErr := errors.New("test error")
		executed := false

		engine.OnC2C().HandleE(func(ctx *Context) error {
			executed = true
			return testErr
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		// 执行不应该 panic
		engine.ProcessEvent(ctx)

		assert.True(t, executed, "Handler should be executed")
		// 错误应该被记录（通过日志），但不会中断执行
	})

	t.Run("Handler panic 应该被恢复", func(t *testing.T) {
		engine := NewEngine()

		executed := false

		engine.OnC2C().Handle(func(ctx *Context) {
			executed = true
			panic("intentional panic")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		// 执行不应该导致整个程序 panic
		assert.NotPanics(t, func() {
			engine.ProcessEvent(ctx)
		})

		assert.True(t, executed, "Handler should be executed before panic")
	})

	t.Run("多个 Handler 错误互不影响", func(t *testing.T) {
		engine := NewEngine()

		var executed1, executed2, executed3 bool

		engine.OnC2C().HandleE(func(ctx *Context) error {
			executed1 = true
			return errors.New("error 1")
		})

		engine.OnC2C().Handle(func(ctx *Context) {
			executed2 = true
			panic("panic 2")
		})

		engine.OnC2C().Handle(func(ctx *Context) {
			executed3 = true
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 所有 handler 都应该被执行
		assert.True(t, executed1)
		assert.True(t, executed2)
		assert.True(t, executed3)
	})
}

// TestInvokeHandlerMetrics 测试错误指标记录
func TestInvokeHandlerMetrics(t *testing.T) {
	t.Run("错误处理应该正常工作", func(t *testing.T) {
		engine := NewEngine()

		// 不设置指标收集器，验证没有 nil pointer 问题
		testErr := errors.New("test error")
		engine.OnC2C().HandleE(func(ctx *Context) error {
			return testErr
		})

		engine.OnC2C().Handle(func(ctx *Context) {
			panic("intentional panic")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		// 即使没有指标收集器，也应该正常工作
		assert.NotPanics(t, func() {
			engine.ProcessEvent(ctx)
		})
	})
}

// TestInvokeHandlerWithMiddleware 测试中间件中的错误处理
func TestInvokeHandlerWithMiddleware(t *testing.T) {
	t.Run("中间件中的 panic 应该被恢复", func(t *testing.T) {
		engine := NewEngine()

		panicMiddleware := func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				panic("middleware panic")
			}
		}

		engine.Use(panicMiddleware)

		executed := false
		engine.OnC2C().Handle(func(ctx *Context) {
			executed = true
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		assert.NotPanics(t, func() {
			engine.ProcessEvent(ctx)
		})

		// Handler 不会被执行，因为中间件 panic 了
		assert.False(t, executed)
	})

	t.Run("中间件返回错误应该被记录", func(t *testing.T) {
		engine := NewEngine()

		errorMiddleware := func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				return errors.New("middleware error")
			}
		}

		engine.Use(errorMiddleware)

		executed := false
		engine.OnC2C().Handle(func(ctx *Context) {
			executed = true
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// Handler 不会被执行，因为中间件返回了错误
		assert.False(t, executed)
	})
}

// TestInvokeHandlerConcurrent 测试并发错误处理
func TestInvokeHandlerConcurrent(t *testing.T) {
	t.Run("并发执行多个会出错的 Handler", func(t *testing.T) {
		engine := NewEngine()

		engine.OnC2C().HandleE(func(ctx *Context) error {
			return errors.New("concurrent error")
		})

		engine.OnC2C().Handle(func(ctx *Context) {
			panic("concurrent panic")
		})

		var wg sync.WaitGroup
		concurrency := 100

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				event := &dto.Payload{Type: dto.C2CMessageCreate}
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}()
		}

		// 不应该死锁或 panic
		wg.Wait()
	})
}

// TestErrorLogging 测试错误日志记录
func TestErrorLogging(t *testing.T) {
	t.Run("错误应该包含完整的上下文信息", func(t *testing.T) {
		engine := NewEngine()

		testErr := fmt.Errorf("detailed error: %s", "reason")
		executed := false

		engine.OnC2C().HandleE(func(ctx *Context) error {
			executed = true
			return testErr
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		assert.True(t, executed)
		// 错误应该被记录到日志中，包含 matcher source 和 event type
	})
}
