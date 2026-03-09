package qq

import (
	stdctx "context"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// Webhook 是 QQ webhook 事件源的最小接口（与 remilia.Webhook 兼容）
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// Adapter 是 QQ 平台的 platform.PlatformAdapter 实现。
//
// 内部使用 Webhook 接收 QQ 官方 payload，将其转换为 platform.Event 后
// 调用框架提供的 handler，实现平台解耦。
//
// 用法：
//
//	webhookConn := remilia.NewWebhookServerAdapter(":8080", botInfo)
//	qqAdapter := qq.NewAdapter(webhookConn, openAPIClient)
//
//	// 注册到多平台注册表
//	registry := platform.NewRegistry()
//	registry.Register(qqAdapter)
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

// NewAdapter 创建 QQ 平台适配器
//
// webhook 是事件源（实现 EventStream() <-chan *dto.Payload）。
// api 是 QQ OpenAPI 客户端，用于发送消息；传 nil 时无法发送。
func NewAdapter(webhook Webhook, api openapi.OpenAPI) *Adapter {
	return &Adapter{
		webhook: webhook,
		sender:  NewSender(api),
	}
}

// Platform 返回平台标识符
func (a *Adapter) Platform() string { return PlatformID }

// Sender 返回 QQ 消息发送接口
func (a *Adapter) Sender() platform.Sender { return a.sender }

// Start 启动 QQ 事件循环
//
// 从 webhook.EventStream() 读取 *dto.Payload，转换为 platform.Event 后调用 handler。
// 阻塞直到 ctx 取消或 EventStream 关闭。
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer func() {
		// 事件循环退出后才重置 starting，允许重新 Start
		a.starting.Store(false)
	}()

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
			if payload == nil {
				continue
			}
			// 将 *dto.Payload 转换为平台无关 Event
			event := NewEvent(payload)
			safeHandle(handler, event)
		}
	}
}

// Stop 优雅停止 QQ 事件循环
func (a *Adapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = false
	a.mu.Unlock()

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
		logger.Info("[qq.Adapter] Stopped")
		return nil
	case <-ctx.Done():
		logger.Warn("[qq.Adapter] Stop timeout")
		return ctx.Err()
	}
}

// safeHandle 捕获 handler 中的 panic，防止事件循环崩溃
func safeHandle(handler func(platform.Event), event platform.Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[qq.Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}
