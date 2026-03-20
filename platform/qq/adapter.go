package qq

import (
	stdctx "context"
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
//	webhookServer := remilia.NewWebhookServerAdapter(":8080", botInfo)
//	bot := remilia.NewBot(webhookServer, engine)
type Adapter struct {
	webhook Webhook
	sender  platform.Sender

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

// Platform returns the platform identifier.
func (a *Adapter) Platform() string { return PlatformID }

// Sender returns the QQ message sender.
func (a *Adapter) Sender() platform.Sender { return a.sender }

// StartPlatform starts the QQ event loop.
//
// Reads *dto.Payload from webhook.EventStream(), converts to platform.Event,
// then calls handler. Blocks until ctx is canceled or the stream is closed.
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

	logger.Info("[qq.Adapter] Started")

	for {
		select {
		case <-a.ctx.Done():
			logger.Debug("[qq.Adapter] Context done, stopping")
			return nil
		case payload, ok := <-eventCh:
			if !ok {
				logger.Warn("[qq.Adapter] EventStream closed")
				return nil
			}
			if payload != nil {
				event := NewEvent(payload)
				a.wg.Go(func() {
					safeInvoke(handler, event)
				})
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
