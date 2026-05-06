package qq

import (
	stdctx "context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// Webhook 是 QQ webhook 事件源所需的最简接口。
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// Adapter 是 QQ 的 platform.Adapter 实现。
//
// 它从 Webhook 中读取 *dto.Payload，经过 NewEvent() 转换为 platform.Event 后
// 直接交给框架提供的 handler 处理。handler 的并发控制由 Engine 的 ExecPool 负责。
//
// 多平台注册表用法示例（在已有 webhook.Conn 上注册）：
//
//	// webhookConn 需实现 EventStream() <-chan *dto.Payload
//	// 例如来自 openapi/protocol/webhook 的 *webhook.Conn
//	qqAdapter := qq.NewAdapter(webhookConn, openAPIClient)
//	registry := platform.NewRegistry()
//	registry.Register(qqAdapter)
//
// 单平台（自包含）场景可直接使用 WebhookServerAdapter：
//
//	webhookServer := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(webhookServer).Build()
type Adapter struct {
	webhook Webhook
	sender  platform.Sender
	api     openapi.OpenAPI

	ctx      stdctx.Context
	cancel   stdctx.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool

	// BotIdentity
	botID   string
	botName string
}

// NewAdapter 创建 QQ 平台适配器。
//
// webhook 是事件源（需实现 EventStream()）。
// api 是用于发送消息的 QQ OpenAPI 客户端，传 nil 可禁用发送能力。
func NewAdapter(webhook Webhook, api openapi.OpenAPI) *Adapter {
	return &Adapter{
		webhook: webhook,
		sender:  NewSender(api),
		api:     api,
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
// 从 Webhook 读取事件，转换后直接调用 handler。handler 的并发控制
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
	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		a.mu.Unlock()
		return nil
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
				safeInvoke(handler, event)
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
	if a.api == nil {
		return
	}
	fetchCtx, cancel := stdctx.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := a.api.GetMe(fetchCtx)
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

func safeInvoke(handler func(platform.Event), event platform.Event) {
	platform.SafeDispatch(handler, event)
}

// ── 编译期接口断言 ────────────────────────────────────────────────────────────

var (
	_ platform.Adapter     = (*Adapter)(nil)
	_ platform.BotIdentity = (*Adapter)(nil)
)
