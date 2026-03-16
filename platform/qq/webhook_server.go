package qq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/protocol/webhook"
)

// WebhookServerAdapter 是一个内置 HTTP 服务器的 Webhook 适配器。
//
// 实现 platform.PlatformAdapter 接口，绑定 QQ Webhook 协议并将事件转为 platform.Event。
type WebhookServerAdapter struct {
	addr       string
	botInfo    *dto.BotInfo
	api        openapi.OpenAPI // 用于创建 QQ Sender
	webhook    *webhook.Conn
	server     *http.Server
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	running    bool
	workers    int
	bufferSize int
}

// Platform 实现 platform.PlatformAdapter
func (a *WebhookServerAdapter) Platform() string { return PlatformID }

// Sender 实现 platform.PlatformAdapter
func (a *WebhookServerAdapter) Sender() platform.Sender {
	if a.api != nil {
		return NewSender(a.api)
	}
	return &platform.NoopSender{}
}

// WithAPI 注入 QQ OpenAPI client，用于通过 ctx.Reply() 发送消息。
// 支持链式调用：adapter.WithAPI(api).Start(ctx, handler)
func (a *WebhookServerAdapter) WithAPI(api openapi.OpenAPI) *WebhookServerAdapter {
	a.api = api
	return a
}

// NewWebhookServerAdapter 创建一个内置 HTTP 服务器的 Webhook 适配器（使用默认配置）
//
// 参数:
//   - addr: HTTP 服务器监听地址，例如 ":8080" 或 "0.0.0.0:8080"
//   - botInfo: 机器人信息
//
// 示例:
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot := remilia.NewBot(adapter, engine)
//	bot.Start()
func NewWebhookServerAdapter(addr string, botInfo *dto.BotInfo) *WebhookServerAdapter {
	return NewWebhookServerAdapterWithConfig(addr, botInfo, config.WebhookConfig{
		WorkerCount: 0,   // 0 = 使用 CPU 核心数
		EventBuffer: 100, // 默认缓冲区大小
	})
}

// SimpleWebhookAdapter 创建最简单的 Webhook 适配器
//
// 使用默认配置，适合快速原型开发
//
// 参数:
//   - port: 监听端口（例如 8080）
//
// 示例:
//
//	adapter := qq.SimpleWebhookAdapter(8080)
//
// 注意: 此适配器不包含 botInfo，仅用于接收事件，不支持主动 API 调用
func SimpleWebhookAdapter(port int) *WebhookServerAdapter {
	return NewWebhookServerAdapter(fmt.Sprintf(":%d", port), nil)
}

// NewWebhookServerAdapterWithConfig 从配置创建 Webhook 适配器
//
// 参数:
//   - addr: HTTP 服务器监听地址
//   - botInfo: 机器人信息
//   - webhookConfig: Webhook 配置（从 config.Config.Webhook 获取）
//
// 示例:
//
//	cfg, _ := config.LoadDefault()
//	adapter := qq.NewWebhookServerAdapterWithConfig(":8080", botInfo, cfg.Webhook)
//	bot := remilia.NewBot(adapter, eng)
//	bot.Start()
func NewWebhookServerAdapterWithConfig(addr string, botInfo *dto.BotInfo, webhookConfig config.WebhookConfig) *WebhookServerAdapter {
	workers := webhookConfig.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	bufferSize := webhookConfig.EventBuffer
	if bufferSize <= 0 {
		bufferSize = 100
	}

	logger.Infof("[WebhookServerAdapter] Config: workers=%d, buffer=%d", workers, bufferSize)

	return &WebhookServerAdapter{
		addr:       addr,
		botInfo:    botInfo,
		workers:    workers,
		bufferSize: bufferSize,
	}
}

// StartPlatform 实现 platform.PlatformAdapter.StartPlatform，接受 platform.Event handler
func (a *WebhookServerAdapter) StartPlatform(ctx context.Context, handler func(platform.Event)) error {
	return a.startWithPlatformHandler(ctx, handler)
}

func (a *WebhookServerAdapter) startWithPlatformHandler(ctx context.Context, handler func(platform.Event)) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		logger.Warn("[WebhookServerAdapter] Already running")
		return nil
	}

	a.ctx, a.cancel = context.WithCancel(ctx)

	bufferSize := a.bufferSize
	if bufferSize <= 0 {
		bufferSize = 100
	}
	a.webhook = webhook.NewWithBuffer(a.ctx, a.botInfo, bufferSize)
	if a.webhook == nil {
		a.mu.Unlock()
		return errutil.ErrWebhookCreateFailed
	}

	logger.Infof("[WebhookServerAdapter] Webhook buffer size: %d", bufferSize)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", a.webhook.Handle)
	mux.HandleFunc("/", a.webhook.Handle)

	a.server = &http.Server{
		Addr:    a.addr,
		Handler: mux,
	}

	a.running = true
	a.mu.Unlock()

	eventStream := a.webhook.EventStream()

	logger.Infof("[WebhookServerAdapter] Starting %d event workers", a.workers)

	workersReady := make(chan struct{})
	workersStarted := make(chan struct{}, a.workers)

	for i := 0; i < a.workers; i++ {
		a.wg.Add(1)
		workerID := i
		go func() {
			defer a.wg.Done()
			logger.Debugf("[WebhookServerAdapter] Event worker #%d started", workerID)
			workersStarted <- struct{}{}

			for {
				select {
				case <-a.ctx.Done():
					logger.Debugf("[WebhookServerAdapter] Worker #%d stopping", workerID)
					return
				case payload, ok := <-eventStream:
					if !ok {
						logger.Warnf("[WebhookServerAdapter] Worker #%d: event stream closed", workerID)
						return
					}
					if payload != nil {
						event := NewEvent(payload)
						safeInvoke(handler, event)
					}
				}
			}
		}()
	}

	go func() {
		for i := 0; i < a.workers; i++ {
			<-workersStarted
		}
		close(workersReady)
	}()

	select {
	case <-workersReady:
		logger.Debug("[WebhookServerAdapter] All workers ready")
	case <-a.ctx.Done():
		logger.Warn("[WebhookServerAdapter] Context cancelled while waiting for workers")
		return a.ctx.Err()
	}

	ln, err := net.Listen("tcp", a.addr)
	if err != nil {
		a.cancel()
		a.wg.Wait()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return errutil.Wrapf(err, "failed to bind address %s", a.addr)
	}

	a.wg.Go(func() {
		logger.Infof("[WebhookServerAdapter] Starting HTTP server on %s", a.addr)
		if err := a.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[WebhookServerAdapter] HTTP server error")
			a.cancel()
		}
	})

	logger.Info("[WebhookServerAdapter] Started successfully")
	return nil
}

// Stop 停止适配器（关闭 HTTP 服务器和事件循环）
func (a *WebhookServerAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		logger.Debug("[WebhookServerAdapter] Not running, nothing to stop")
		return nil
	}
	a.running = false
	a.mu.Unlock()

	logger.Info("[WebhookServerAdapter] Stopping...")

	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			logger.WithError(err).Warn("[WebhookServerAdapter] HTTP server shutdown error")
		}
	}

	if a.cancel != nil {
		a.cancel()
	}

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("[WebhookServerAdapter] Stopped successfully")
		return nil
	case <-ctx.Done():
		logger.Warn("[WebhookServerAdapter] Stop timeout")
		return ctx.Err()
	}
}
