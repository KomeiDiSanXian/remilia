package remilia

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/sirupsen/logrus"
)

// WebhookServerAdapter 是一个内置 HTTP 服务器的 Webhook 适配器
type WebhookServerAdapter struct {
	addr    string
	botInfo *dto.BotInfo
	webhook *webhook.Conn
	server  *http.Server
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	running bool
}

// NewWebhookServerAdapter 创建一个内置 HTTP 服务器的 Webhook 适配器
//
// 参数:
//   - addr: HTTP 服务器监听地址，例如 ":8080" 或 "0.0.0.0:8080"
//   - botInfo: 机器人信息
//
// 示例:
//
//	adapter := remilia.NewWebhookServerAdapter(":8080", global.Info)
//	bot := remilia.NewBot(adapter, engine)
//	bot.Start()
func NewWebhookServerAdapter(addr string, botInfo *dto.BotInfo) *WebhookServerAdapter {
	return &WebhookServerAdapter{
		addr:    addr,
		botInfo: botInfo,
	}
}

// NewWebhookServerAdapterWithBuffer 创建一个指定缓冲区大小的 Webhook 适配器
func NewWebhookServerAdapterWithBuffer(addr string, botInfo *dto.BotInfo, bufferSize int) *WebhookServerAdapter {
	return &WebhookServerAdapter{
		addr:    addr,
		botInfo: botInfo,
	}
}

// Start 启动适配器（启动 HTTP 服务器和事件循环）
func (a *WebhookServerAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		logrus.Warn("[WebhookServerAdapter] Already running")
		return nil
	}

	// 创建 context
	a.ctx, a.cancel = context.WithCancel(ctx)

	// 创建 webhook 连接
	a.webhook = webhook.NewWebhook(a.ctx, a.botInfo)
	if a.webhook == nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to create webhook connection")
	}

	// 创建 HTTP 服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", a.webhook.Handle)
	mux.HandleFunc("/", a.webhook.Handle) // 兼容根路径

	a.server = &http.Server{
		Addr:    a.addr,
		Handler: mux,
	}

	a.running = true
	a.mu.Unlock()

	// 启动 HTTP 服务器
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logrus.Infof("[WebhookServerAdapter] Starting HTTP server on %s", a.addr)

		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("[WebhookServerAdapter] HTTP server error")
		}
	}()

	// 启动事件循环（从 webhook 读取事件并转发给 handler）
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logrus.Debug("[WebhookServerAdapter] Event loop started")

		eventStream := a.webhook.EventStream()
		for {
			select {
			case <-a.ctx.Done():
				logrus.Debug("[WebhookServerAdapter] Context done, stopping event loop")
				return
			case event, ok := <-eventStream:
				if !ok {
					logrus.Warn("[WebhookServerAdapter] Event stream closed")
					return
				}
				if event != nil {
					// 安全调用 handler
					safeHandleEvent(handler, event)
				}
			}
		}
	}()

	logrus.Info("[WebhookServerAdapter] Started successfully")
	return nil
}

// Stop 停止适配器（关闭 HTTP 服务器和事件循环）
func (a *WebhookServerAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		logrus.Debug("[WebhookServerAdapter] Not running, nothing to stop")
		return nil
	}
	a.running = false
	a.mu.Unlock()

	logrus.Info("[WebhookServerAdapter] Stopping...")

	// 1. 关闭 HTTP 服务器
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			logrus.WithError(err).Warn("[WebhookServerAdapter] HTTP server shutdown error")
		}
	}

	// 2. 取消 context（停止事件循环）
	if a.cancel != nil {
		a.cancel()
	}

	// 3. 等待所有 goroutine 完成
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("[WebhookServerAdapter] Stopped successfully")
		return nil
	case <-ctx.Done():
		logrus.Warn("[WebhookServerAdapter] Stop timeout")
		return ctx.Err()
	}
}

// safeHandleEvent 安全地调用事件处理器，捕获 panic
func safeHandleEvent(handler func(*dto.Payload), event *dto.Payload) {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithFields(logrus.Fields{
				"panic":    r,
				"event_id": event.ID,
			}).Error("[WebhookServerAdapter] Handler panic recovered")
		}
	}()
	handler(event)
}
