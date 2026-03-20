package qq

import (
	stdctx "context"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// Webhook is the minimal interface for a QQ webhook event source.
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// Adapter is the QQ platform.PlatformAdapter implementation.
//
// It reads *dto.Payload from a Webhook, converts them to platform.Event via
// NewEvent(), and invokes the framework-provided handler.
//
// Usage (multi-platform registry, wrapping an existing webhook.Conn):
//
//	// webhookConn must implement EventStream() <-chan *dto.Payload
//	// e.g. a *webhook.Conn from openapi/protocol/webhook
//	qqAdapter := qq.NewAdapter(webhookConn, openAPIClient)
//	registry := platform.NewRegistry()
//	registry.Register(qqAdapter)
//
// For a self-contained QQ setup (single platform), use WebhookServerAdapter directly:
//
//	webhookServer := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(webhookServer).Build()
type Adapter struct {
	webhook Webhook
	sender  platform.Sender
	// workers 是事件处理 goroutine 数量（0 表示使用 runtime.NumCPU()）
	workers int

	ctx      stdctx.Context
	cancel   stdctx.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool
}

// NewAdapter creates a QQ platform adapter.
//
// webhook is the event source (must implement EventStream()).
// api is the QQ OpenAPI client used for sending messages; pass nil to disable sending.
func NewAdapter(webhook Webhook, api openapi.OpenAPI) *Adapter {
	return &Adapter{
		webhook: webhook,
		sender:  NewSender(api),
	}
}

// WithWorkers 设置事件处理 worker goroutine 数量。
//
// 0 或负值表示使用 runtime.NumCPU()（默认行为）。
// 链式调用：qq.NewAdapter(wh, api).WithWorkers(4)
func (a *Adapter) WithWorkers(n int) *Adapter {
	a.workers = n
	return a
}

// Platform returns the platform identifier.
func (a *Adapter) Platform() string { return PlatformID }

// Sender returns the QQ message sender.
func (a *Adapter) Sender() platform.Sender { return a.sender }

// Capabilities returns QQ platform feature capabilities.
func (a *Adapter) Capabilities() platform.PlatformCapabilities { return QQCapabilities }

// StartPlatform starts the QQ event loop.
//
// 使用有界 worker pool 处理事件，避免高频事件下无限创建 goroutine。
// worker 数量默认为 runtime.NumCPU()，可通过 WithWorkers 调整。
// Blocks until ctx is canceled or the stream is closed.
func (a *Adapter) StartPlatform(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return nil
	}
	a.ctx, a.cancel = stdctx.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	// 计算 worker 数量
	numWorkers := a.workers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	logger.Infof("[qq.Adapter] Started with %d workers", numWorkers)

	// 有界事件队列：缓冲区为 worker 数量的 2 倍，避免分发时阻塞
	workCh := make(chan platform.Event, numWorkers*2)

	// 启动固定数量的 worker goroutine
	for i := 0; i < numWorkers; i++ {
		a.wg.Go(func() {
			for event := range workCh {
				safeInvoke(handler, event)
			}
		})
	}

	// 主分发循环：从平台 channel 读取 payload，转换后投递到 workCh
	for {
		select {
		case <-a.ctx.Done():
			close(workCh)
			logger.Debug("[qq.Adapter] Context done, stopping")
			return nil
		case payload, ok := <-eventCh:
			if !ok {
				close(workCh)
				logger.Warn("[qq.Adapter] EventStream closed")
				return nil
			}
			if payload != nil {
				event := NewEvent(payload)
				// 投递事件；若 workCh 满（worker 来不及处理），等待或随 ctx 取消
				select {
				case workCh <- event:
				case <-a.ctx.Done():
					close(workCh)
					return nil
				}
			}
		}
	}
}

// Stop gracefully stops the QQ adapter.
func (a *Adapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = false
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("[qq.Adapter] Stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func safeInvoke(handler func(platform.Event), event platform.Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[qq.Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}
