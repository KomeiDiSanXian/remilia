package platform

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// chatInfoContextKey 是向 Go context 注入 ChatInfo 的 key（平台无关）
type chatInfoContextKey struct{}

// eventIDContextKey 是向 Go context 注入触发事件 ID 的 key（用于被动回复）
type eventIDContextKey struct{}

// WithChatInfo 将 ChatInfo 注入到 Go context，供下游发送器路由使用。
func WithChatInfo(ctx stdctx.Context, chat ChatInfo) stdctx.Context {
	return stdctx.WithValue(ctx, chatInfoContextKey{}, chat)
}

// ChatInfoFromContext 从 Go context 中读取 ChatInfo。
func ChatInfoFromContext(ctx stdctx.Context) (ChatInfo, bool) {
	chat, ok := ctx.Value(chatInfoContextKey{}).(ChatInfo)
	return chat, ok
}

// WithEventID 将触发事件 ID 注入到 Go context。
//
// 框架在调用 ctx.Reply() 时自动注入，供 QQ 等需要被动回复关联的平台使用。
// 平台发送器可通过 [EventIDFromContext] 读取并自动填充回复关联字段。
func WithEventID(ctx stdctx.Context, eventID string) stdctx.Context {
	if eventID == "" {
		return ctx
	}
	return stdctx.WithValue(ctx, eventIDContextKey{}, eventID)
}

// EventIDFromContext 从 Go context 中读取触发事件 ID。
//
// 若未注入或为空，ok 返回 false。
func EventIDFromContext(ctx stdctx.Context) (string, bool) {
	id, ok := ctx.Value(eventIDContextKey{}).(string)
	return id, ok && id != ""
}

// ────────────────────────────────────────────────────────────────────────────
// Sender
// ────────────────────────────────────────────────────────────────────────────

// Sender 是平台无关的消息发送接口。
//
// 目标会话信息（ChatInfo，包含 ID、IsGroup 等路由字段）由调用方
// 通过 platform.WithChatInfo 注入到 ctx 中，Send 实现通过
// platform.ChatInfoFromContext 读取，不再依赖额外的 chatID 参数。
//
// 使用示例（handler 内）：
//
//	ctx.Reply(platform.TextMessage("pong"))  // 框架自动注入 ChatInfo
//
// 直接调用（已知目标会话）：
//
//	sendCtx := platform.WithChatInfo(context.Background(), platform.ChatInfo{
//	    ID:      "group-001",
//	    IsGroup: true,
//	})
//	sender.Send(sendCtx, platform.TextMessage("公告"))
type Sender interface {
	// Send 发送消息。目标会话信息从 ctx 中的 ChatInfo 读取。
	//
	// 若 ctx 未携带 ChatInfo，实现者应返回 errutil.ErrNoChatInfo。
	Send(ctx stdctx.Context, msg OutboundMessage) error
}

// MessageEditor 可选接口，支持消息编辑的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if editor, ok := sender.(platform.MessageEditor); ok {
//	    editor.Edit(ctx, messageID, newMsg)
//	}
type MessageEditor interface {
	// Edit 编辑已发送的消息。
	// chatID 为目标会话 ID，messageID 为平台原生消息 ID。
	Edit(ctx stdctx.Context, messageID string, msg OutboundMessage) error
}

// MessageDeleter 可选接口，支持消息删除的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if deleter, ok := sender.(platform.MessageDeleter); ok {
//	    deleter.Delete(ctx, messageID)
//	}
type MessageDeleter interface {
	// Delete 删除/撤回已发送的消息。
	// messageID 为平台原生消息 ID。
	Delete(ctx stdctx.Context, messageID string) error
}

// NoopSender 空实现，用于测试或不需要发送能力的场景
type NoopSender struct{}

// Send 什么也不做，始终返回 nil
func (n *NoopSender) Send(_ stdctx.Context, _ OutboundMessage) error {
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Capabilities
// ────────────────────────────────────────────────────────────────────────────

// Capabilities 声明平台支持的特性集合。
//
// 平台适配器通过 Capabilities() 返回此结构，允许 Handler 在运行时
// 做跨平台特性检测，实现"渐进增强"策略（优先使用丰富特性，降级到纯文本）。
//
// 示例：
//
//	caps := ctx.GetPlatformCapabilities()
//	if caps.Embeds {
//	    msg = platform.TextMessage("").WithEmbeds(myEmbed)
//	} else {
//	    msg = platform.MarkdownMessage(myEmbed.Title + "\n" + myEmbed.Description)
//	}
type Capabilities struct {
	// Markdown 是否支持 Markdown 格式消息
	Markdown bool
	// Buttons 是否支持交互按钮（内联键盘等）
	Buttons bool
	// MultiAttachment 是否支持在一条消息中发送多个附件
	MultiAttachment bool
	// MessageEdit 是否支持编辑已发送消息（实现 MessageEditor）
	MessageEdit bool
	// MessageDelete 是否支持删除/撤回消息（实现 MessageDeleter）
	MessageDelete bool
	// Embeds 是否支持富文本嵌入卡片
	Embeds bool
	// FileUpload 是否支持二进制文件直传（非 URL，Attachment.Data）
	FileUpload bool
	// GuildSupport 是否有服务器/频道层级（ChatInfo.ParentID 有效）
	GuildSupport bool
}

// ────────────────────────────────────────────────────────────────────────────
// Adapter
// ────────────────────────────────────────────────────────────────────────────

// Adapter 是平台适配器的核心接口。
//
// 每个平台（QQ、Discord、Telegram 等）实现此接口，
// 框架核心通过此接口接收事件和发送消息，不依赖任何平台 SDK。
//
// 生命周期：
//
//	Start() ──→ [事件循环，持续调用 handler] ──→ Stop()
type Adapter interface {
	// Platform 返回平台标识符（小写，如 "qq"、"discord"、"telegram"）
	Platform() string

	// Start 启动适配器事件循环（阻塞，直到 ctx 取消或出错）
	//
	// 每收到一个事件，调用 handler(event)。
	// handler 应快速返回（框架内部会在 goroutine 中处理）。
	Start(ctx stdctx.Context, handler func(Event)) error

	// Stop 优雅停止适配器
	Stop(ctx stdctx.Context) error

	// Sender 返回该平台的消息发送接口
	Sender() Sender

	// Capabilities 返回该平台支持的特性集合。
	// 用于 Handler 做跨平台特性检测，实现渐进增强策略。
	Capabilities() Capabilities

	// IsRunning 返回适配器当前是否处于运行状态。
	//
	// 在 Start() 成功启动后返回 true，Stop() 完成后返回 false。
	// 用于健康检查和监控，实现应保证并发安全。
	IsRunning() bool
}

// ────────────────────────────────────────────────────────────────────────────
// Registry
// ────────────────────────────────────────────────────────────────────────────

// Registry 多平台适配器注册表。
//
// 支持同时运行多个平台适配器，框架通过 Registry 管理它们的生命周期。
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry 创建空的适配器注册表
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]Adapter),
	}
}

// Register 注册一个平台适配器
//
// 若同一平台已注册，会覆盖旧适配器。
func (r *Registry) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Platform()] = adapter
}

// Get 获取指定平台的适配器
func (r *Registry) Get(platform string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	return a, ok
}

// Remove 注销指定平台的适配器，返回 true 表示成功移除，false 表示不存在。
//
// 注意：仅从注册表中移除，不调用 Stop()；若适配器正在运行，
// 调用方应先调用 adapter.Stop() 再调用 Remove。
func (r *Registry) Remove(platform string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[platform]; !ok {
		return false
	}
	delete(r.adapters, platform)
	return true
}

// All 返回所有已注册适配器的快照（切片顺序不保证）
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// StartAll 并发启动所有已注册平台适配器
//
// 每个适配器在独立 goroutine 中运行，ctx 取消时所有适配器退出。
// handler 会收到来自所有平台的事件。
// 若有适配器以非 context 取消的错误退出，StartAll 将返回该错误。
func (r *Registry) StartAll(ctx stdctx.Context, handler func(Event)) error {
	adapters := r.All()
	if len(adapters) == 0 {
		return fmt.Errorf("platform registry: no adapters registered")
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for _, a := range adapters {
		wg.Go(func() { // 这部分依赖 Go 1.22+ 语义（闭包捕获）
			if err := a.Start(ctx, handler); err != nil {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Error("[Registry] Platform adapter exited with error")
				mu.Lock()
				errs = append(errs, fmt.Errorf("platform %s: %w", a.Platform(), err))
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	// 返回第一个非 context 取消/超时的错误
	for _, err := range errs {
		if !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
			return err
		}
	}
	return nil
}

// StopAll 并发停止所有已注册平台适配器，合并全部错误后返回。
//
// 所有适配器同时发起停止，总耗时取决于最慢的那一个（而非各平台停止时间之和）。
func (r *Registry) StopAll(ctx stdctx.Context) error {
	adapters := r.All()
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, a := range adapters {
		wg.Go(func() {
			if err := a.Stop(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("platform %s stop: %w", a.Platform(), err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
