package remilia

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/sirupsen/logrus"
)

// WebhookServerAdapter 是一个内置 HTTP 服务器的 Webhook 适配器
type WebhookServerAdapter struct {
	addr       string
	botInfo    *dto.BotInfo
	webhook    *webhook.Conn
	server     *http.Server
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	running    bool
	workers    int // 并发事件处理的 worker 数量，默认为 1
	bufferSize int // webhook event channel 的 buffer 大小
}

// NewWebhookServerAdapter 创建一个内置 HTTP 服务器的 Webhook 适配器（使用默认配置）
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
	// 使用默认配置
	return NewWebhookServerAdapterWithConfig(addr, botInfo, config.WebhookConfig{
		WorkerCount: 0,   // 0 = 使用 CPU 核心数
		EventBuffer: 100, // 默认缓冲区大小
	})
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
//	adapter := remilia.NewWebhookServerAdapterWithConfig(":8080", global.Info, cfg.Webhook)
//	bot := remilia.NewBot(adapter, engine)
//	bot.Start()
func NewWebhookServerAdapterWithConfig(addr string, botInfo *dto.BotInfo, webhookConfig config.WebhookConfig) *WebhookServerAdapter {
	workers := webhookConfig.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU() // 0 表示使用 CPU 核心数
	}

	bufferSize := webhookConfig.EventBuffer
	if bufferSize <= 0 {
		bufferSize = 100 // 默认值
	}

	logrus.Infof("[WebhookServerAdapter] Config: workers=%d, buffer=%d", workers, bufferSize)

	return &WebhookServerAdapter{
		addr:       addr,
		botInfo:    botInfo,
		workers:    workers,
		bufferSize: bufferSize,
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

	// 创建 webhook 连接，使用配置的 buffer 大小
	bufferSize := a.bufferSize
	if bufferSize <= 0 {
		bufferSize = 100 // 默认值
	}
	a.webhook = webhook.NewWithBuffer(a.ctx, a.botInfo, bufferSize)
	if a.webhook == nil {
		a.mu.Unlock()
		return errutil.ErrWebhookCreateFailed
	}

	logrus.Infof("[WebhookServerAdapter] Webhook buffer size: %d", bufferSize)

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

	// 获取事件流
	eventStream := a.webhook.EventStream()

	// 先启动事件处理 workers，确保在 HTTP 服务器接收请求前已准备就绪
	logrus.Infof("[WebhookServerAdapter] Starting %d event workers", a.workers)

	// 使用 channel 等待所有 workers 启动完成
	workersReady := make(chan struct{})
	workersStarted := make(chan struct{}, a.workers)

	for i := 0; i < a.workers; i++ {
		a.wg.Add(1)
		workerID := i
		go func() {
			defer a.wg.Done()
			logrus.Debugf("[WebhookServerAdapter] Event worker #%d started", workerID)

			// 通知 worker 已启动
			workersStarted <- struct{}{}

			for {
				select {
				case <-a.ctx.Done():
					logrus.Debugf("[WebhookServerAdapter] Worker #%d stopping", workerID)
					return
				case event, ok := <-eventStream:
					if !ok {
						logrus.Warnf("[WebhookServerAdapter] Worker #%d: event stream closed", workerID)
						return
					}
					if event != nil {
						// 安全调用 handler
						safeHandleEvent(handler, event)
					}
				}
			}
		}()
	}

	// 等待所有 workers 启动
	go func() {
		for i := 0; i < a.workers; i++ {
			<-workersStarted
		}
		close(workersReady)
	}()

	// 等待 workers 就绪（最多等待 100ms 防止阻塞）
	select {
	case <-workersReady:
		logrus.Debug("[WebhookServerAdapter] All workers ready")
	case <-time.After(100 * time.Millisecond):
		logrus.Warn("[WebhookServerAdapter] Workers startup timeout, continuing anyway")
	}

	// 现在启动 HTTP 服务器（workers 已就绪）
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logrus.Infof("[WebhookServerAdapter] Starting HTTP server on %s", a.addr)

		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Error("[WebhookServerAdapter] HTTP server error")
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
