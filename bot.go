package remilia

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/sirupsen/logrus"
)

// Bot is the main entry point for the Remilia.
type Bot struct {
	adapter Adapter
	tm      *token.Manager
	engine  *Engine
	api     openapi.OpenAPI

	// Context 传播机制：用于优雅关闭时主动取消正在执行的 handler
	ctx    context.Context
	cancel context.CancelFunc
}

// BotOption is the option for the Bot.
type BotOption func(*Bot)

// WithWebHook enables the webhook protocol for the bot.
func WithWebHook(wh webhook.WebHook) BotOption {
	return func(b *Bot) {
		b.adapter = NewWebhookAdapter(wh)
	}
}

// WithAdapter sets a custom adapter for the bot.
func WithAdapter(adapter Adapter) BotOption {
	return func(b *Bot) {
		b.adapter = adapter
	}
}

// WithEngine sets a custom engine for the bot.
func WithEngine(engine *Engine) BotOption {
	return func(b *Bot) {
		b.engine = engine
	}
}

// New creates a new instance of the Remilia SDK.
//
// 自动创建独立的 Engine 实例。如果需要自定义 Engine，可通过 WithEngine() 选项提供。
//
// 使用示例：
//
//	// 默认：自动创建 Engine
//	bot := remilia.New(info)
//
//	// 或提供自定义 Engine
//	engine := remilia.NewEngine()
//	bot := remilia.New(info, remilia.WithEngine(engine))
//
// 优势：
//   - ✅ 测试隔离：每个 Bot 使用独立 Engine
//   - ✅ 多实例：可同时运行多个 Bot
//   - ✅ 简单易用：零配置即可使用
func New(info *dto.BotInfo, options ...BotOption) *Bot {
	tm := token.NewManager(info)

	b := &Bot{
		tm:  tm,
		api: openapi.New(tm),
	}

	// 应用选项（可能覆盖默认 Engine）
	for _, opt := range options {
		opt(b)
	}

	// 默认创建新的 Engine (如果选项中未提供)
	if b.engine == nil {
		b.engine = NewEngine()
	}

	return b
}

// GetEngine 获取 Bot 的引擎
func (b *Bot) GetEngine() *Engine {
	return b.engine
}

// GetAPI 获取 Bot 的 OpenAPI
func (b *Bot) GetAPI() openapi.OpenAPI {
	return b.api
}

// On registers a matcher for a specific event type.
// This delegates to the underlying Engine.
func (b *Bot) On(eventType dto.EventType, rules ...Rule) *Matcher {
	return b.engine.On(eventType, rules...)
}

// OnAny registers a matcher for any event type.
func (b *Bot) OnAny(rules ...Rule) *Matcher {
	return b.engine.OnAny(rules...)
}

// OnC2C registers a matcher for C2C messages.
func (b *Bot) OnC2C(rules ...Rule) *Matcher {
	return b.engine.OnC2C(rules...)
}

// OnGroupAt registers a matcher for Group@ messages.
func (b *Bot) OnGroupAt(rules ...Rule) *Matcher {
	return b.engine.OnGroupAt(rules...)
}

// OnGroupAdd registers a matcher for GroupAddRobot events.
func (b *Bot) OnGroupAdd(rules ...Rule) *Matcher {
	return b.engine.OnGroupAdd(rules...)
}

// OnGroupDel registers a matcher for GroupDelRobot events.
func (b *Bot) OnGroupDel(rules ...Rule) *Matcher {
	return b.engine.OnGroupDel(rules...)
}

// OnCommand registers a matcher for a command.
func (b *Bot) OnCommand(eventType dto.EventType, command string, rules ...Rule) *Matcher {
	return b.engine.OnCommand(eventType, command, rules...)
}

// OnFullMatch registers a matcher for exact match.
func (b *Bot) OnFullMatch(text string, rules ...Rule) *Matcher {
	return b.engine.OnFullMatch(text, rules...)
}

// Use registers global middleware.
// This delegates to the underlying Engine but returns *Bot for chaining.
func (b *Bot) Use(mw ...HandlerMiddleware) *Bot {
	b.engine.Use(mw...)
	return b
}

// Start 启动 Bot（非阻塞），配合 Shutdown 进行优雅关闭
func (b *Bot) Start() {
	// 创建 bot 级别的 context，用于优雅关闭时主动取消所有 handler
	b.ctx, b.cancel = context.WithCancel(context.Background())

	if b.adapter != nil {
		handleFunc := func(event *dto.Payload) {
			// 使用 bot context 作为父 context，支持优雅关闭时主动取消
			ctx := NewContextWithContext(b.ctx, event, b.api)
			// IMPORTANT: in-flight tracking is owned by Engine (Engine.eventWg).
			// Adapter.Start is already required to run asynchronously.
			b.engine.ProcessEvent(ctx)
		}

		if err := b.adapter.Start(b.ctx, handleFunc); err != nil {
			logrus.WithError(err).Error("[Remilia] Failed to start adapter")
		}
	} else {
		logrus.Warn("[Remilia] No adapter configured")
	}

	logrus.Infof("[Remilia] Bot started")
}

// Shutdown 优雅关闭 Bot：停止 HTTP 服务器与事件处理协程
//
// 关闭流程：
//
//   - 1. 主动取消所有正在执行的 handler（通过 context）
//   - 2. 停止适配器（停止接收新事件）
//   - 3. 关闭 Engine：停止后台资源并等待 in-flight 事件处理完成（受 ctx 限制）
//
// ctx 参数控制整个关闭流程的超时时间。
func (b *Bot) Shutdown(ctx context.Context) {
	logrus.Info("[Remilia] Starting graceful shutdown...")

	// 1. 主动取消所有正在执行的 handler
	if b.cancel != nil {
		b.cancel()
		logrus.Debug("[Remilia] Cancelled all running handlers")
	}

	// 2. 停止适配器（停止接收新事件）
	if b.adapter != nil {
		if err := b.adapter.Shutdown(ctx); err != nil {
			logrus.WithError(err).Warn("[Remilia] Adapter shutdown error")
		}
	}

	// 3. 关闭 Engine：停止后台资源并等待 in-flight 事件处理完成（受 ctx 限制）
	if b.engine != nil {
		if err := b.engine.Shutdown(ctx); err != nil {
			logrus.WithError(err).Warn("[Remilia] Engine shutdown error")
		}
	}

	logrus.Info("[Remilia] Bot shutdown complete")
}

// Run starts the bot.
//
// ctrl + c to stop the bot.
func (b *Bot) Run() {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("recover", r).Error("[Remilia] Recovered from panic")
		}
	}()

	b.Start()
	logrus.Infof("[Remilia] Bot is running")

	sgnChan := make(chan os.Signal, 1)
	signal.Notify(sgnChan, syscall.SIGINT, syscall.SIGTERM)
	sgn := <-sgnChan
	logrus.WithField("signal", sgn).Warn("[Remilia] Received signal, shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.Shutdown(ctx)
}
