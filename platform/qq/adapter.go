package qq

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// EventSource 是 QQ 平台事件源的抽象接口。
//
// 可由 Webhook（*WebhookService）或 WebSocket（*WSConn）实现，
// 两者均通过 EventStream() 提供 *dto.Payload 事件流。
type EventSource interface {
	EventStream() <-chan *dto.Payload
}

// connectionType 标识当前适配器使用的事件源类型。
type connectionType string

const (
	connWebhook connectionType = "webhook"
	connWS      connectionType = "websocket"
)

// Adapter 是 QQ 的 platform.Adapter 实现。
//
// 它从 EventSource（Webhook 或 WebSocket）中读取 *dto.Payload，
// 经过 NewEvent() 转换为 platform.Event 后直接交给框架提供的 handler 处理。
// handler 的并发控制由 Engine 的 ExecPool 负责。
//
// 多平台注册表用法示例（Webhook）：
//
//	qqAdapter := qq.NewAdapter(webhookConn, openAPIClient)
//	registry := platform.NewRegistry()
//	registry.Register(qqAdapter)
//
// 单平台（自包含）场景可直接使用 WebhookServerAdapter：
//
//	webhookServer := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(webhookServer).Build()
//
// WebSocket 用法：
//
//	wsConn := qq.NewWSConn(api, tokenMgr)
//	wsConn.Start(ctx)
//	qqAdapter := qq.NewAdapter(wsConn, api)
type Adapter struct {
	eventSource EventSource
	sender      platform.Sender
	api         openapi.OpenAPI

	ctx      stdctx.Context
	cancel   stdctx.CancelFunc
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool

	// BotIdentity
	botID   string
	botName string

	connType connectionType
}

// NewAdapter 创建 QQ 平台适配器。
//
// eventSource 是事件源，需实现 EventStream() <-chan *dto.Payload。
// 可以是 *WebhookService、*webhook.Conn 或 *WSConn。
// api 是用于发送消息的 QQ OpenAPI 客户端，传 nil 可禁用发送能力。
func NewAdapter(eventSource EventSource, api openapi.OpenAPI) *Adapter {
	connType := connWebhook
	if _, ok := eventSource.(*WSConn); ok {
		connType = connWS
	}
	return &Adapter{
		eventSource: eventSource,
		sender:      NewSender(api),
		api:         api,
		connType:    connType,
	}
}

// WithAPI 设置适配器的 OpenAPI 客户端，用于异步获取机器人身份和消息发送。
// 在 Start 后调用，允许延迟绑定 WebhookConn 中创建的 API 客户端。
func (a *Adapter) WithAPI(api openapi.OpenAPI) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.api = api
	if api != nil {
		a.sender = NewSender(api)
	}
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

// BotID 返回机器人在 QQ 平台的唯一标识符。
func (a *Adapter) BotID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botID
}

// BotName 返回机器人的 QQ 昵称。
func (a *Adapter) BotName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botName
}

// Platform returns the platform identifier.
func (a *Adapter) Platform() string { return PlatformID }

// Sender returns the QQ message sender.
func (a *Adapter) Sender() platform.Sender { return a.sender }

// Capabilities returns QQ platform feature capabilities.
func (a *Adapter) Capabilities() platform.Capabilities { return qqCapabilities() }

// IsRunning 返回适配器当前是否处于运行状态。
func (a *Adapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 启动 QQ 事件循环。
//
// 从 EventSource 读取事件，转换后直接调用 handler。handler 的并发控制
// 由 Engine 的 ExecPool 负责，适配器不再维护自己的 worker pool。
//
// 直到 ctx 被取消或事件流关闭前此方法会一直阻塞。
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	eventCh := a.eventSource.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return fmt.Errorf("qq adapter: EventStream is nil")
	}
	a.ctx, a.cancel = stdctx.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	// 异步获取机器人身份（尽力而为）
	go a.fetchBotIdentity(a.ctx)

	logger.Info("[qq.Adapter] Started")

	// 事件循环：从平台 channel 读取 payload，转换后直接调用 handler
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
				platform.SafeDispatch(handler, event)
			}
		}
	}
}

// Stop 优雅关闭 QQ 适配器。
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
	logger.Info("[qq.Adapter] Stopped")
	return nil
}

// fetchBotIdentity 调用 GetMe 以填充 botID 和 botName。
func (a *Adapter) fetchBotIdentity(ctx stdctx.Context) {
	a.mu.RLock()
	api := a.api
	a.mu.RUnlock()
	if api == nil {
		return
	}
	fetchCtx, cancel := stdctx.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := api.GetMe(fetchCtx)
	if err != nil {
		logger.WithError(err).Debug("[qq.Adapter] Could not fetch bot identity")
		return
	}
	id := result.Get("id").String()
	name := result.Get("username").String()
	if id == "" {
		return
	}
	a.mu.Lock()
	a.botID = id
	a.botName = name
	a.mu.Unlock()
	logger.Infof("[qq.Adapter] Bot identity: %s (%s)", name, id)
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

// HealthDetail 返回适配器的健康详情，包括连接类型、API 可用性和机器人身份状态。
func (a *Adapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": string(a.connType),
	}
	if a.api != nil {
		detail["api_available"] = true
	}
	a.mu.RLock()
	if a.botID != "" {
		detail["bot_identified"] = true
	}
	a.mu.RUnlock()
	if a.connType == connWS {
		if ws, ok := a.eventSource.(*WSConn); ok {
			detail["session_active"] = ws.sessionID != ""
		}
	}
	return detail
}

// ── 编译期接口断言 ────────────────────────────────────────────────────────────

var (
	_ platform.Adapter        = (*Adapter)(nil)
	_ platform.BotIdentity    = (*Adapter)(nil)
	_ platform.HealthDetailer = (*Adapter)(nil)
)
