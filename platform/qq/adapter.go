package qq

import (
	stdctx "context"
	"runtime"
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
// 交给框架提供的 handler 处理。
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
	// workers 是事件处理 goroutine 数量（0 表示使用 runtime.NumCPU()）
	workers int

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

// WithWorkers 设置事件处理 worker goroutine 数量。
//
// 0 或负值表示使用 runtime.NumCPU()（默认行为）。
// 链式调用：qq.NewAdapter(wh, api).WithWorkers(4)
func (a *Adapter) WithWorkers(n int) *Adapter {
	a.workers = n
	return a
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
// 使用有界 worker pool 处理事件，避免高频事件下无限创建 goroutine。
// worker 数量默认为 runtime.NumCPU()，可通过 WithWorkers 调整。
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
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("[qq.Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// ── 编译期接口断言 ────────────────────────────────────────────────────────────

var (
	_ platform.Adapter     = (*Adapter)(nil)
	_ platform.BotIdentity = (*Adapter)(nil)
)
