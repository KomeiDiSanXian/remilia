package remilia

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
)

// Adapter 是旧版事件适配器接口（Deprecated）。
//
// Deprecated: 请使用 engine.PlatformAdapter 替代。
// Adapter 要求 handler 接受 *dto.Payload，与 QQ 平台强绑定。
// 迁移到 engine.PlatformAdapter 可支持多平台事件处理。
type Adapter = engine.Adapter

// Webhook 是 webhook 的最小接口，只需要 EventStream。
//
// 此接口面向 QQ SDK 内部，保留 *dto.Payload 是合理的 QQ 约定。
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// webhookAdapter 将 Webhook 适配为 engine.PlatformAdapter（新接口）。
//
// 内部将从 Webhook.EventStream() 读取到的 *dto.Payload
// 通过 platform/qq.NewEvent 转换为 platform.Event，
// 再调用 platform-agnostic handler，从而解耦上层逻辑。
type webhookAdapter struct {
	webhook  Webhook
	api      openapi.OpenAPI // QQ OpenAPI client，用于创建 Sender
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool
}

// NewWebhookAdapter 创建一个 webhook adapter（实现 engine.PlatformAdapter）。
//
// api 参数用于创建 QQ platform.Sender；传 nil 时发送功能不可用。
func NewWebhookAdapter(wh Webhook) Adapter {
	return &webhookAdapter{webhook: wh}
}

// NewWebhookAdapterWithAPI 创建携带 OpenAPI client 的 webhook adapter。
//
// 相比 NewWebhookAdapter，handler 可以通过 ctx.Reply() 发送消息。
func NewWebhookAdapterWithAPI(wh Webhook, api openapi.OpenAPI) engine.PlatformAdapter {
	return &webhookAdapter{webhook: wh, api: api}
}

// Platform 实现 engine.PlatformAdapter
func (a *webhookAdapter) Platform() string { return qqplatform.PlatformID }

// Sender 实现 engine.PlatformAdapter
func (a *webhookAdapter) Sender() platform.Sender {
	if a.api != nil {
		return qqplatform.NewSender(a.api)
	}
	return &platform.NoopSender{}
}

// Start 启动 adapter（同时满足旧 Adapter 和新 PlatformAdapter 接口）。
//
// 若 handler 类型为 func(platform.Event)，走新路径；
// 若为 func(*dto.Payload)，走旧路径（向后兼容）。
//
// 注意：Bot 内部已更新为使用新路径，此处旧路径仅为外部直接调用 Start 的场景保留。
func (a *webhookAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	return a.startWithPayloadHandler(ctx, handler)
}

// StartPlatform 实现 engine.PlatformAdapter.Start，接受 platform.Event handler
func (a *webhookAdapter) StartPlatform(ctx context.Context, handler func(platform.Event)) error {
	return a.startWithPlatformHandler(ctx, handler)
}

func (a *webhookAdapter) startWithPayloadHandler(ctx context.Context, handler func(*dto.Payload)) error {
	// 包装为 platform.Event handler，内部转换
	return a.startWithPlatformHandler(ctx, func(event platform.Event) {
		if raw := event.RawPayload(); raw != nil {
			if payload, ok := raw.(*dto.Payload); ok {
				handler(payload)
				return
			}
		}
	})
}

func (a *webhookAdapter) startWithPlatformHandler(ctx context.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return errutil.New("adapter is already starting or started")
	}
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
		startSucceeded = true
		a.starting.Store(false)
		return nil
	}

	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return errutil.New("EventStream returned nil channel")
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		logger.Debug("[Adapter] Event loop started")
		for {
			select {
			case <-a.ctx.Done():
				logger.Debug("[Adapter] Context done, stopping event loop")
				return
			case payload, ok := <-eventCh:
				if !ok {
					logger.Warn("[Adapter] EventStream closed, stopping event loop")
					return
				}
				if payload != nil {
					event := qqplatform.NewEvent(payload)
					safeHandlePlatform(handler, event)
				}
			}
		}
	}()

	logger.Info("[Adapter] Started successfully")
	startSucceeded = true
	return nil
}

func safeHandlePlatform(handler func(platform.Event), event platform.Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// safeHandle 保留向后兼容（供 webhook_adapter.go 使用）
func safeHandle(handler func(*dto.Payload), event *dto.Payload) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// Stop 优雅停止 adapter
func (a *webhookAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		logger.Debug("[Adapter] Not running, nothing to stop")
		return nil
	}
	a.running = false
	a.cancel()
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("[Adapter] Stopped")
		return nil
	case <-ctx.Done():
		logger.Warn("[Adapter] Stop timed out")
		return ctx.Err()
	}
}
