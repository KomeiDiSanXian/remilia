package remilia

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// Adapter connects an event source to the Bot
// It is responsible for receiving events from external sources and delivering them to the bot
type Adapter interface {
	// Start starts the adapter and begins processing events
	// The handleFunc will be called for each received event
	Start(ctx context.Context, handleFunc func(*dto.Payload)) error

	// Stop gracefully shuts down the adapter
	Stop(ctx context.Context) error
}

// Webhook 是 webhook 的最小接口，只需要 EventStream
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// webhookAdapter 将 Webhook 适配为 core.Adapter
type webhookAdapter struct {
	webhook  Webhook
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // Track the event loop goroutine
	mu       sync.RWMutex   // Protect state
	running  bool           // Track if event loop is running
	starting atomic.Bool    // Prevent concurrent Start calls
}

// NewWebhookAdapter 创建一个 webhook adapter
func NewWebhookAdapter(wh Webhook) Adapter {
	return &webhookAdapter{
		webhook: wh,
	}
}

// Start 启动 adapter
func (a *webhookAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	// 防止并发 Start 调用
	if !a.starting.CompareAndSwap(false, true) {
		return errutil.New("adapter is already starting or started")
	}
	// 修复 B1：只在失败时重置 starting，成功路径不重置（依靠 running 字段防止重复启动）
	var startSucceeded bool
	defer func() {
		if !startSucceeded {
			a.starting.Store(false)
		}
	}()

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		logger.Warn("[Adapter] Already running")
		startSucceeded = true // 已在运行，视为成功，保持 starting=true 无意义，但不应重置
		a.starting.Store(false)
		return nil
	}

	// 验证 EventStream 是否为 nil
	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return errutil.New("EventStream returned nil channel")
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	// 启动事件循环
	a.wg.Go(func() {
		logger.Debug("[Adapter] Event loop started")

		for {
			select {
			case <-a.ctx.Done():
				logger.Debug("[Adapter] Context done, stopping event loop")
				return
			case event, ok := <-eventCh:
				if !ok {
					logger.Warn("[Adapter] EventStream closed, stopping event loop")
					return
				}
				if event != nil {
					// 使用 defer+recover 包装 handler 调用，防止 panic 导致 goroutine 退出
					safeHandle(handler, event)
				}
			}
		}
	})

	logger.Info("[Adapter] Started successfully")
	startSucceeded = true
	return nil
}

func safeHandle(handler func(*dto.Payload), event *dto.Payload) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// Stop gracefully shuts down the adapter
func (a *webhookAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		logger.Debug("[Adapter] Not running, nothing to stop")
		return nil
	}
	a.running = false
	a.mu.Unlock()

	logger.Info("[Adapter] Stopping...")

	// 1. Signal the event loop to stop
	if a.cancel != nil {
		a.cancel()
	}

	// 2. Wait for event loop goroutine to finish (with timeout from context)
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("[Adapter] Stopped successfully")
		return nil
	case <-ctx.Done():
		logger.Warn("[Adapter] Stop timeout, event loop may still be running")
		return ctx.Err()
	}
}

// NewWebhookAdapter creates a webhook adapter with built-in HTTP server
// This is a convenience function that creates a WebhookServerAdapter
//
// 参数:
//   - addr: HTTP server address, e.g., ":8080"
//   - secret: webhook secret for signature verification (currently not used, reserved for future)
//
// 示例:
//
//	adapter := remilia.NewWebhookAdapter(":8080", "your-secret")
//	bot := remilia.NewBot(adapter, engine)
//	bot.Start()
//
// 注意: 如果你需要更多控制，使用 NewWebhookServerAdapter
func NewWebhookAdapterWithServer(addr string, secret string) Adapter {
	// TODO: 使用 secret 进行签名验证
	return NewWebhookServerAdapter(addr, &dto.BotInfo{})
}
