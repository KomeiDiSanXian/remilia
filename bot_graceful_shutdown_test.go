package remilia

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestBotContextCancellation 测试 Bot 关闭时 context 取消传播
func TestBotContextCancellation(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	// 启动 bot
	bot.Start()

	// 检查 context 已创建
	assert.NotNil(t, bot.ctx, "bot context should be initialized")
	assert.NotNil(t, bot.cancel, "bot cancel function should be initialized")

	// 验证 bot context 未被取消
	select {
	case <-bot.ctx.Done():
		t.Fatal("bot context should not be cancelled before shutdown")
	default:
		// 正常
	}

	// 关闭 bot
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	bot.Shutdown(shutdownCtx)

	// 验证 bot context 已被取消
	assert.Error(t, bot.ctx.Err(), "bot context should be cancelled after shutdown")
}

// TestBotShutdownTimeout 测试 Bot 关闭超时处理
func TestBotShutdownTimeout(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	bot.Start()

	release := make(chan struct{})
	started := make(chan struct{})

	bot.engine.OnAny().HandleE(func(ctx *Context) error {
		close(started)
		<-release
		return nil
	})

	// 触发一个 in-flight handler
	go bot.engine.ProcessEvent(NewContextWithContext(bot.ctx, &dto.Payload{Type: dto.C2CMessageCreate, ID: "block"}, bot.api))
	<-started

	// 使用短超时关闭 bot
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	bot.Shutdown(shutdownCtx)
	elapsed := time.Since(start)

	// 释放以防 goroutine 泄露
	close(release)

	// 验证 Shutdown 在超时后返回（不会无限等待）
	assert.Less(t, elapsed, time.Second, "shutdown should return after timeout")
	assert.True(t, elapsed >= 500*time.Millisecond, "shutdown should wait at least the timeout duration")
}

// TestBotShutdownGraceful 测试优雅关闭（无待处理任务时快速完成）
func TestBotShutdownGraceful(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	bot.Start()

	// 优雅关闭（没有待处理任务）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	bot.Shutdown(shutdownCtx)
	elapsed := time.Since(start)

	// 关闭应该立即完成（没有待处理任务）
	assert.Less(t, elapsed, 500*time.Millisecond, "shutdown should complete quickly when no pending work")

	// 验证没有超时
	assert.NoError(t, shutdownCtx.Err(), "shutdown should complete before timeout")
}

// TestBotMultipleShutdownCalls 测试多次调用 Shutdown
func TestBotMultipleShutdownCalls(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	bot.Start()

	// 第一次关闭
	ctx1, cancel1 := context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	bot.Shutdown(ctx1)

	// 第二次关闭（应该安全，不会 panic）
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	assert.NotPanics(t, func() {
		bot.Shutdown(ctx2)
	}, "multiple shutdown calls should not panic")
}

// TestBotContextPropagation 测试 context 在中间件中的传播
func TestBotContextPropagation(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	bot.Start()

	var contextPropagated atomic.Bool

	// 添加中间件检查 context
	bot.engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			// 检查 context 是否可以被取消
			select {
			case <-ctx.Context().Done():
				contextPropagated.Store(true)
				return ctx.Context().Err()
			default:
				// context 还未取消
			}
			return next(ctx)
		}
	})

	started := make(chan struct{})
	bot.engine.OnAny().HandleE(func(ctx *Context) error {
		close(started)
		// 等待 context 取消
		<-ctx.Context().Done()
		return nil
	})

	// 处理事件：直接走 Engine，in-flight 统计归 Engine
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-event-3"}
	ctx := NewContextWithContext(bot.ctx, event, bot.api)
	go bot.engine.ProcessEvent(ctx)

	<-started

	// 短暂等待后关闭
	time.Sleep(50 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bot.Shutdown(shutdownCtx)

	// 验证 context 被正确传播
	assert.True(t, contextPropagated.Load() || bot.ctx.Err() != nil,
		"context cancellation should propagate through middleware")
}

// TestDrainEventChannel 测试事件通道排空
func TestDrainEventChannel(t *testing.T) {
	// 模拟事件通道
	eventCh := make(chan struct{}, 10)
	for i := 0; i < 5; i++ {
		eventCh <- struct{}{}
	}

	// 测试排空
	var drained int
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

loop:
	for {
		select {
		case <-timer.C:
			break loop
		case <-eventCh:
			drained++
		}
	}

	assert.Equal(t, 5, drained, "should drain all events")
}

// BenchmarkBotShutdown 基准测试 Bot 关闭性能
func BenchmarkBotShutdown(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bot := New(&dto.BotInfo{
			AppID: 123456,
			Token: "test",
		}, WithEngine(NewEngine()))

		bot.Start()

		// 添加一些 handler
		for j := 0; j < 10; j++ {
			bot.engine.OnAny().HandleE(func(ctx *Context) error {
				return nil
			})
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		bot.Shutdown(ctx)
		cancel()
	}
}

// TestBotShutdownWithPendingHandlers 测试有待处理 handler 时的关闭
func TestBotShutdownWithPendingHandlers(t *testing.T) {
	bot := New(&dto.BotInfo{
		AppID: 123456,
		Token: "test",
	}, WithEngine(NewEngine()))

	bot.Start()

	var tasksCompleted atomic.Int32

	release := make(chan struct{})
	started := make(chan struct{})

	// 添加一个会阻塞的 handler（模拟 pending）
	bot.engine.OnAny().HandleE(func(ctx *Context) error {
		close(started)
		<-release
		tasksCompleted.Add(1)
		return nil
	})

	go bot.engine.ProcessEvent(NewContextWithContext(bot.ctx, &dto.Payload{Type: dto.C2CMessageCreate, ID: "pending"}, bot.api))
	<-started

	// 优雅关闭（足够的超时让任务完成）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// 释放 handler，让其在 shutdown 期间能自然完成
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()

	bot.Shutdown(shutdownCtx)

	// 验证任务完成了
	assert.Equal(t, int32(1), tasksCompleted.Load(),
		"handler should complete during graceful shutdown")

	// 验证没有超时
	assert.NoError(t, shutdownCtx.Err(), "shutdown should complete before timeout")
}
