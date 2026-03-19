package platform

import (
	stdctx "context"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// chatInfoContextKey 是向 Go context 注入 ChatInfo 的 key（平台无关）
type chatInfoContextKey struct{}

// WithChatInfo 将 ChatInfo 注入到 Go context，供下游发送器路由使用。
//
// 框架在调用 platform.Sender.Send 时会自动注入，
// 平台发送器实现可通过 ChatInfoFromContext 读取。
func WithChatInfo(ctx stdctx.Context, chat ChatInfo) stdctx.Context {
	return stdctx.WithValue(ctx, chatInfoContextKey{}, chat)
}

// ChatInfoFromContext 从 Go context 中读取 ChatInfo。
//
// 若未注入，ok 返回 false。平台发送器应优先检查此值以决定路由方式（群聊/私聊）。
func ChatInfoFromContext(ctx stdctx.Context) (ChatInfo, bool) {
	chat, ok := ctx.Value(chatInfoContextKey{}).(ChatInfo)
	return chat, ok
}

// Sender 是平台无关的消息发送接口。
//
// 各平台适配器实现此接口，将 OutboundMessage 转换并发送到目标会话。
type Sender interface {
	// Send 发送消息到指定会话
	//
	// chatID 为目标会话 ID（私聊为用户 openID，群聊为群 ID）
	Send(ctx stdctx.Context, chatID string, msg OutboundMessage) error
}

// NoopSender 空实现，用于测试或不需要发送能力的场景
type NoopSender struct{}

// Send 什么也不做，始终返回 nil
func (n *NoopSender) Send(_ stdctx.Context, _ string, _ OutboundMessage) error {
	return nil
}

// PlatformAdapter 是平台适配器的核心接口。
//
// 每个平台（QQ、Discord、Telegram 等）实现此接口，
// 框架核心通过此接口接收事件和发送消息，不依赖任何平台 SDK。
//
// 生命周期：
//
//	Start() ──→ [事件循环，持续调用 handler] ──→ Stop()
type PlatformAdapter interface {
	// Platform 返回平台标识符（小写，如 "qq"、"discord"、"telegram"）
	Platform() string

	// StartPlatform 启动适配器事件循环（阻塞，直到 ctx 取消或出错）
	//
	// 每收到一个事件，调用 handler(event)。
	// handler 应快速返回（框架内部会在 goroutine 中处理）。
	StartPlatform(ctx stdctx.Context, handler func(Event)) error

	// Stop 优雅停止适配器
	Stop(ctx stdctx.Context) error

	// Sender 返回该平台的消息发送接口
	Sender() Sender
}

// Registry 多平台适配器注册表。
//
// 支持同时运行多个平台适配器，框架通过 Registry 管理它们的生命周期。
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]PlatformAdapter
}

// NewRegistry 创建空的适配器注册表
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]PlatformAdapter),
	}
}

// Register 注册一个平台适配器
//
// 若同一平台已注册，会覆盖旧适配器。
func (r *Registry) Register(adapter PlatformAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Platform()] = adapter
}

// Get 获取指定平台的适配器
func (r *Registry) Get(platform string) (PlatformAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	return a, ok
}

// All 返回所有已注册适配器的快照（切片顺序不保证）
func (r *Registry) All() []PlatformAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PlatformAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// StartAll 并发启动所有已注册平台适配器
//
// 每个适配器在独立 goroutine 中运行，ctx 取消时所有适配器退出。
// handler 会收到来自所有平台的事件。
func (r *Registry) StartAll(ctx stdctx.Context, handler func(Event)) error {
	adapters := r.All()
	if len(adapters) == 0 {
		return fmt.Errorf("platform registry: no adapters registered")
	}

	var wg sync.WaitGroup
	for _, a := range adapters {
		wg.Go(func() {
			if err := a.StartPlatform(ctx, handler); err != nil {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Error("[Registry] Platform adapter exited with error")
			}
		})
	}

	// 等待 ctx 取消
	<-ctx.Done()
	// 等待所有平台适配器 goroutine 完全退出后再返回，防止 goroutine 泄漏。
	// 各适配器的 StartPlatform 应感知 ctx.Done() 并自行退出。
	wg.Wait()
	return nil
}

// StopAll 依次停止所有已注册平台适配器，收集并合并错误。
//
// 注意：当通过 [Bot.UsePlatformRegistry] 或 [BotBuilder.WithPlatformRegistry] 将
// Registry 注入到 Bot 时，Bot 的 lifecycle manager 会直接管理各适配器的生命周期，
// 不会调用此方法。StopAll 仅供直接持有 Registry 并自行管理适配器生命周期的场景使用
// （例如自定义框架集成或独立测试）。
func (r *Registry) StopAll(ctx stdctx.Context) error {
	adapters := r.All()
	var errs []error
	for _, a := range adapters {
		if err := a.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("platform %s stop: %w", a.Platform(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("platform registry stop errors: %v", errs)
	}
	return nil
}
