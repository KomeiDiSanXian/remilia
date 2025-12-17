package remilia

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestHandlerCancellation 测试 Handler 响应 Context 取消
func TestHandlerCancellation(t *testing.T) {
	engine := NewEngine()

	var cancelled atomic.Bool
	engine.OnC2C().HandleE(func(ctx *Context) error {
		// 模拟长时间任务
		select {
		case <-ctx.Context().Done():
			cancelled.Store(true)
			return ctx.Context().Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	})

	// 创建可取消的 context
	cancelCtx, cancel := context.WithCancel(context.Background())

	// 创建事件 context
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-cancel"}
	remiliaCtx := NewContextWithContext(cancelCtx, event, nil)

	// 在 goroutine 中处理
	done := make(chan struct{})
	go func() {
		engine.ProcessEvent(remiliaCtx)
		close(done)
	}()

	// 等待一小段时间后取消
	time.Sleep(100 * time.Millisecond)
	cancel()

	// 等待完成
	select {
	case <-done:
		// 验证 handler 被取消
		assert.True(t, cancelled.Load(), "Handler should be cancelled")
	case <-time.After(time.Second):
		t.Fatal("Handler did not complete in time")
	}
}

// TestHandlerWithTimeout 测试 Handler 超时处理
func TestHandlerWithTimeout(t *testing.T) {
	engine := NewEngine()

	var timedOut atomic.Bool
	engine.OnC2C().HandleE(func(ctx *Context) error {
		// 创建带超时的 context
		timeoutCtx, cancel := context.WithTimeout(ctx.Context(), 100*time.Millisecond)
		defer cancel()

		// 模拟长时间操作
		select {
		case <-timeoutCtx.Done():
			timedOut.Store(true)
			return timeoutCtx.Err()
		case <-time.After(time.Second):
			return nil
		}
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-timeout"}
	remiliaCtx := NewContext(event, nil)

	engine.ProcessEvent(remiliaCtx)

	assert.True(t, timedOut.Load(), "Handler should timeout")
}

// TestHandlerPeriodicCancellationCheck 测试周期性取消检查
func TestHandlerPeriodicCancellationCheck(t *testing.T) {
	engine := NewEngine()

	processedCount := 0
	engine.OnC2C().HandleE(func(ctx *Context) error {
		for i := 0; i < 100; i++ {
			// 每10次迭代检查一次
			if i%10 == 0 {
				select {
				case <-ctx.Context().Done():
					return ctx.Context().Err()
				default:
				}
			}

			processedCount++
			time.Sleep(10 * time.Millisecond)
		}
		return nil
	})

	cancelCtx, cancel := context.WithCancel(context.Background())
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-periodic"}
	remiliaCtx := NewContextWithContext(cancelCtx, event, nil)

	done := make(chan struct{})
	go func() {
		engine.ProcessEvent(remiliaCtx)
		close(done)
	}()

	// 在处理过程中取消
	time.Sleep(150 * time.Millisecond)
	cancel()

	<-done

	// 验证没有处理完所有项目
	assert.Less(t, processedCount, 100, "Should not process all items after cancellation")
	assert.Greater(t, processedCount, 0, "Should process some items before cancellation")
}

// TestContextPropagationToNestedHandlers 测试 Context 传播到嵌套 Handler
func TestContextPropagationToNestedHandlers(t *testing.T) {
	engine := NewEngine()

	var mainCancelled, nestedCancelled atomic.Bool

	engine.OnC2C().HandleE(func(ctx *Context) error {
		// 启动嵌套任务
		go func() {
			select {
			case <-ctx.Context().Done():
				nestedCancelled.Store(true)
			case <-time.After(5 * time.Second):
			}
		}()

		// 主任务
		select {
		case <-ctx.Context().Done():
			mainCancelled.Store(true)
			return ctx.Context().Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	cancelCtx, cancel := context.WithCancel(context.Background())
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-nested"}
	remiliaCtx := NewContextWithContext(cancelCtx, event, nil)

	done := make(chan struct{})
	go func() {
		engine.ProcessEvent(remiliaCtx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	<-done

	// 等待嵌套任务完成
	time.Sleep(100 * time.Millisecond)

	assert.True(t, mainCancelled.Load(), "Main handler should be cancelled")
	assert.True(t, nestedCancelled.Load(), "Nested task should be cancelled")
}

// TestMultipleHandlersShutdown 测试多个 Handler 同时关闭
func TestMultipleHandlersShutdown(t *testing.T) {
	handlerCount := 3
	engines := make([]*Engine, handlerCount)
	cancelledCounts := make([]*atomic.Int32, handlerCount)

	// 为每个 handler 创建独立的 engine
	for i := 0; i < handlerCount; i++ {
		engines[i] = NewEngine()
		cancelledCounts[i] = &atomic.Int32{}

		count := cancelledCounts[i]
		engines[i].OnC2C().HandleE(func(ctx *Context) error {
			select {
			case <-ctx.Context().Done():
				count.Add(1)
				return ctx.Context().Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		})
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-multiple"}

	// 启动多个处理
	var doneChans []chan struct{}
	for i := 0; i < handlerCount; i++ {
		done := make(chan struct{})
		doneChans = append(doneChans, done)

		engine := engines[i]
		go func() {
			remiliaCtx := NewContextWithContext(cancelCtx, event, nil)
			engine.ProcessEvent(remiliaCtx)
			close(done)
		}()
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	// 等待所有 handler 完成
	for _, done := range doneChans {
		<-done
	}

	// 验证所有 handler 都被取消
	for i := 0; i < handlerCount; i++ {
		assert.Equal(t, int32(1), cancelledCounts[i].Load(),
			"Handler %d should be cancelled once", i)
	}
}

// TestHandlerCleanupOnCancellation 测试取消时的资源清理
func TestHandlerCleanupOnCancellation(t *testing.T) {
	engine := NewEngine()

	var cleanedUp atomic.Bool
	engine.OnC2C().HandleE(func(ctx *Context) error {
		defer func() {
			cleanedUp.Store(true)
		}()

		select {
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	cancelCtx, cancel := context.WithCancel(context.Background())
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-cleanup"}
	remiliaCtx := NewContextWithContext(cancelCtx, event, nil)

	done := make(chan struct{})
	go func() {
		engine.ProcessEvent(remiliaCtx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	<-done

	assert.True(t, cleanedUp.Load(), "Cleanup should execute even on cancellation")
}

// TestContextDeadlineExceeded 测试 Context 超时
func TestContextDeadlineExceeded(t *testing.T) {
	engine := NewEngine()

	var deadlineErr error
	engine.OnC2C().HandleE(func(ctx *Context) error {
		// 等待超过 deadline
		time.Sleep(200 * time.Millisecond)
		deadlineErr = ctx.Context().Err()
		return deadlineErr
	})

	// 创建带 deadline 的 context (100ms)
	deadlineCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-deadline"}
	remiliaCtx := NewContextWithContext(deadlineCtx, event, nil)

	engine.ProcessEvent(remiliaCtx)

	assert.Equal(t, context.DeadlineExceeded, deadlineErr,
		"Should get DeadlineExceeded error")
}

// TestBotShutdownCancelsHandlers 测试 Bot Shutdown 取消 Handlers
func TestBotShutdownCancelsHandlers(t *testing.T) {
	bot := New(&dto.BotInfo{AppID: 123})
	engine := bot.GetEngine()

	var cancelled atomic.Bool
	engine.OnC2C().HandleE(func(ctx *Context) error {
		select {
		case <-ctx.Context().Done():
			cancelled.Store(true)
			return ctx.Context().Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	bot.Start()

	// 模拟处理事件
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-bot-shutdown"}
	remiliaCtx := NewContextWithContext(bot.ctx, event, bot.api)
	done := make(chan struct{})
	go func() {
		bot.wg.Add(1)
		defer bot.wg.Done()
		engine.ProcessEvent(remiliaCtx)
		close(done)
	}()

	// 等待一下然后关闭
	time.Sleep(100 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bot.Shutdown(shutdownCtx)

	// 等待 handler 完成
	<-done

	assert.True(t, cancelled.Load(), "Handler should be cancelled by bot shutdown")
}

// BenchmarkContextCancellationCheck 基准测试 Context 取消检查的开销
func BenchmarkContextCancellationCheck(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// BenchmarkContextWithTimeout 基准测试带超时的 Context 创建
func BenchmarkContextWithTimeout(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
		cancel()
		_ = timeoutCtx
	}
}
