package remilia

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	wh     webhook.WebHook
	tm     *token.Manager
	engine *Engine
	api    openapi.OpenAPI

	// 优雅关闭相关
	srv    *http.Server
	stopCh chan struct{}
	wg     sync.WaitGroup

	// Context 传播机制：用于优雅关闭时主动取消正在执行的 handler
	ctx    context.Context
	cancel context.CancelFunc
}

// BotOption is the option for the Bot.
type BotOption func(*Bot)

// WithWebHook enables the webhook protocol for the bot.
func WithWebHook(wh webhook.WebHook) BotOption {
	return func(b *Bot) {
		b.wh = wh
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

	// 默认创建新的 Engine
	engine := NewEngine()

	b := &Bot{
		tm:     tm,
		engine: engine,
		api:    openapi.New(tm),
	}

	// 应用选项（可能覆盖默认 Engine）
	for _, opt := range options {
		opt(b)
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

// Start 启动 Bot（非阻塞），配合 Shutdown 进行优雅关闭
func (b *Bot) Start() {
	// 创建 bot 级别的 context，用于优雅关闭时主动取消所有 handler
	b.ctx, b.cancel = context.WithCancel(context.Background())

	if b.wh != nil {
		b.runWithWebhook()
	}
	logrus.Infof("[Remilia] Bot started")
}

// Shutdown 优雅关闭 Bot：停止 HTTP 服务器与事件处理协程
//
// 关闭流程：
//
//   - 1. 主动取消所有正在执行的 handler（通过 context）
//   - 2. 停止事件循环（不再接收新事件）
//   - 3. 关闭 HTTP 服务器（停止接收新连接）
//   - 4. 排空事件通道（防止 goroutine 阻塞）
//   - 5. 等待所有 handler 完成（带超时）
//
// ctx 参数控制整个关闭流程的超时时间。
func (b *Bot) Shutdown(ctx context.Context) {
	logrus.Info("[Remilia] Starting graceful shutdown...")

	// 1. 主动取消所有正在执行的 handler
	if b.cancel != nil {
		b.cancel()
		logrus.Debug("[Remilia] Cancelled all running handlers")
	}

	// 2. 停止事件循环
	if b.stopCh != nil {
		select {
		case <-b.stopCh:
		default:
			close(b.stopCh)
			logrus.Debug("[Remilia] Stopped event loop")
		}
	}

	// 3. 关闭 HTTP 服务器，沿用调用方的超时/取消
	if b.srv != nil {
		// 如果调用方未设置截止时间，兜底 5 秒以免悬挂
		shutdownCtx := ctx
		if deadline, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			shutdownCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
		} else {
			_ = deadline // 保持可读性，明确已携带截止时间
		}

		if err := b.srv.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("[Remilia] HTTP server shutdown error")
		} else {
			logrus.Debug("[Remilia] HTTP server closed")
		}
	}

	// 4. 排空事件通道（防止 goroutine 阻塞）
	b.drainEventChannel(500 * time.Millisecond)

	// 5. 等待所有 handler 完成（带超时）
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("[Remilia] All handlers completed successfully")
	case <-ctx.Done():
		logrus.Warn("[Remilia] Shutdown timeout, some handlers may be interrupted")
		// 注意：超时后 goroutine 可能仍在运行，但我们不再等待
	}

	// 6. 关闭 Engine，停止后台清理器等资源
	if b.engine != nil {
		b.engine.Close()
	}

	logrus.Info("[Remilia] Bot shutdown complete")
}

// drainEventChannel 排空事件通道，防止 goroutine 阻塞
func (b *Bot) drainEventChannel(timeout time.Duration) {
	if b.wh == nil {
		return
	}

	ch := b.wh.EventStream()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	drained := 0
	for {
		select {
		case <-timer.C:
			if drained > 0 {
				logrus.Infof("[Remilia] Drained %d pending events", drained)
			}
			return
		case _, ok := <-ch:
			if !ok {
				if drained > 0 {
					logrus.Infof("[Remilia] Drained %d pending events", drained)
				}
				return
			}
			drained++
		}
	}
}

func (b *Bot) runWithWebhook() {
	logrus.Info("[Remilia] Starting bot with webhook")
	if b.wh == nil {
		logrus.Error("[Remilia] Webhook is nil, cannot start HTTP server")
		return
	}
	if b.stopCh == nil {
		b.stopCh = make(chan struct{})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", b.wh.Handle)
	b.srv = &http.Server{Addr: b.wh.Addr(), Handler: mux}

	// Start the HTTP server in a separate goroutine
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if err := b.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Error("[Remilia] Failed to start the server")
		}
	}()

	// Start a goroutine to listen for events and publish them to the engine
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case event, ok := <-b.wh.EventStream():
				if !ok {
					logrus.Info("[Remilia] Event stream closed")
					return
				}
				logrus.WithFields(logrus.Fields{
					"type": event.Type,
					"id":   event.ID,
				}).Debug("[Remilia] Processing event")
				// 使用 bot context 作为父 context，支持优雅关闭时主动取消
				ctx := NewContextWithContext(b.ctx, event, b.api)
				b.engine.ProcessEvent(ctx)
			case <-b.stopCh:
				logrus.Info("[Remilia] Stopping event loop")
				return
			}
		}
	}()
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
