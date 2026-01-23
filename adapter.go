package remilia

import (
	"context"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
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
	webhook Webhook
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup // Track the event loop goroutine
	mu      sync.RWMutex   // Protect state
	running bool           // Track if event loop is running
}

// NewWebhookAdapter 创建一个 webhook adapter
func NewWebhookAdapter(wh Webhook) Adapter {
	return &webhookAdapter{
		webhook: wh,
	}
}

// Start 启动 adapter
func (a *webhookAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		logrus.Warn("[Adapter] Already running")
		return nil
	}

	// 验证 EventStream 是否为 nil
	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return fmt.Errorf("EventStream returned nil channel")
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	// 启动事件循环
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logrus.Debug("[Adapter] Event loop started")

		for {
			select {
			case <-a.ctx.Done():
				logrus.Debug("[Adapter] Context done, stopping event loop")
				return
			case event, ok := <-eventCh:
				if !ok {
					logrus.Warn("[Adapter] EventStream closed, stopping event loop")
					return
				}
				if event != nil {
					// 使用 defer+recover 包装 handler 调用，防止 panic 导致 goroutine 退出
					safeHandle(handler, event)
				}
			}
		}
	}()

	logrus.Info("[Adapter] Started successfully")
	return nil
}

func safeHandle(handler func(*dto.Payload), event *dto.Payload) {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("panic", r).Error("[Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// Stop gracefully shuts down the adapter
func (a *webhookAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		logrus.Debug("[Adapter] Not running, nothing to stop")
		return nil
	}
	a.running = false
	a.mu.Unlock()

	logrus.Info("[Adapter] Stopping...")

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
		logrus.Info("[Adapter] Stopped successfully")
		return nil
	case <-ctx.Done():
		logrus.Warn("[Adapter] Stop timeout, event loop may still be running")
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
