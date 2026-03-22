package platform

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ────────────────────────────────────────────────────────────────────────────
// SendRequest
// ────────────────────────────────────────────────────────────────────────────

// SendRequest 发送请求信封，将路由信息与消息内容显式捆绑。
//
// 替代原先通过 context.WithValue 隐式注入 ChatInfo / EventID 的方式，
// 使 Sender 接口的契约完全可见，并在编译期保证类型安全。
//
// 被动回复授权 token（如 QQ 的 msg_id / event_id）由 Target（ChatInfo）的
// Tokens 字段携带，平台事件解析时自动填充。
type SendRequest struct {
	// Target 目标会话信息（ID、IsGroup、Tokens 等路由与回复字段）。
	// Target.ID 为空时，Sender 实现应返回 errutil.ErrNoChatInfo。
	Target ChatInfo

	// Message 要发送的消息内容。
	Message OutboundMessage
}

// ────────────────────────────────────────────────────────────────────────────
// Sender
// ────────────────────────────────────────────────────────────────────────────

// Sender 是平台无关的消息发送接口。
//
// 路由信息（目标会话 ChatInfo，含被动回复 token）由 SendRequest.Target 显式传入，
// ctx 仅用于超时控制、取消传播和 OpenTelemetry tracing。
//
// 使用示例（handler 内，框架自动构造 SendRequest）：
//
//	ctx.Reply(platform.TextMessage("pong"))
//
// 直接调用（已知目标会话）：
//
//	sender.Send(context.Background(), platform.SendRequest{
//	    Target:  platform.ChatInfo{ID: "group-001", IsGroup: true},
//	    Message: platform.TextMessage("公告"),
//	})
type Sender interface {
	// Send 发送消息。路由信息由 req 显式传入，ctx 仅用于取消/超时/tracing。
	//
	// 若 req.Target.ID 为空，实现者应返回 errutil.ErrNoChatInfo。
	Send(ctx stdctx.Context, req SendRequest) error
}

// MessageEditor 可选接口，支持消息编辑的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if editor, ok := sender.(platform.MessageEditor); ok {
//	    editor.Edit(ctx, chatID, messageID, newMsg)
//	}
type MessageEditor interface {
	// Edit 编辑已发送的消息。
	// chatID 为目标会话 ID（频道/群/私聊），messageID 为平台原生消息 ID。
	Edit(ctx stdctx.Context, chatID, messageID string, msg OutboundMessage) error
}

// MessageDeleter 可选接口，支持消息删除的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if deleter, ok := sender.(platform.MessageDeleter); ok {
//	    deleter.Delete(ctx, chatID, messageID)
//	}
type MessageDeleter interface {
	// Delete 删除/撤回已发送的消息。
	// chatID 为目标会话 ID，messageID 为平台原生消息 ID。
	Delete(ctx stdctx.Context, chatID, messageID string) error
}

// ReactionSender 可选接口，支持表情回应操作的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if rs, ok := platform.GetReactionSender(adapter); ok {
//	    rs.AddReaction(ctx, chatID, messageID, "👍")
//	}
type ReactionSender interface {
	// AddReaction 为指定消息添加表情回应。
	// chatID 为目标会话 ID，messageID 为平台原生消息 ID，emoji 为表情标识符或 Unicode。
	AddReaction(ctx stdctx.Context, chatID, messageID, emoji string) error
	// RemoveReaction 移除指定消息上的表情回应。
	RemoveReaction(ctx stdctx.Context, chatID, messageID, emoji string) error
}

// TypingNotifier 可选接口，支持"正在输入"状态的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if tn, ok := platform.GetTypingNotifier(adapter); ok {
//	    tn.SendTyping(ctx, chatID)
//	}
type TypingNotifier interface {
	// SendTyping 向指定会话发送"正在输入"状态指示。
	SendTyping(ctx stdctx.Context, chatID string) error
}

// NoopSender 空实现，用于测试或不需要发送能力的场景
type NoopSender struct{}

// Send 什么也不做，始终返回 nil
func (n *NoopSender) Send(_ stdctx.Context, _ SendRequest) error {
	return nil
}

// GetEditor 安全获取适配器 Sender 的消息编辑接口。
//
// 若 Sender 未实现 [MessageEditor]，返回 (nil, false)。
// 使用示例：
//
//	if editor, ok := platform.GetEditor(adapter); ok {
//	    editor.Edit(ctx, chatID, messageID, newContent)
//	}
func GetEditor(a Adapter) (MessageEditor, bool) {
	e, ok := a.Sender().(MessageEditor)
	return e, ok
}

// GetDeleter 安全获取适配器 Sender 的消息删除接口。
//
// 若 Sender 未实现 [MessageDeleter]，返回 (nil, false)。
// 使用示例：
//
//	if deleter, ok := platform.GetDeleter(adapter); ok {
//	    deleter.Delete(ctx, chatID, messageID)
//	}
func GetDeleter(a Adapter) (MessageDeleter, bool) {
	d, ok := a.Sender().(MessageDeleter)
	return d, ok
}

// GetReactionSender 安全获取适配器 Sender 的表情回应接口。
//
// 若 Sender 未实现 [ReactionSender]，返回 (nil, false)。
// 使用示例：
//
//	if rs, ok := platform.GetReactionSender(adapter); ok {
//	    rs.AddReaction(ctx, chatID, messageID, "👍")
//	}
func GetReactionSender(a Adapter) (ReactionSender, bool) {
	rs, ok := a.Sender().(ReactionSender)
	return rs, ok
}

// GetTypingNotifier 安全获取适配器 Sender 的"正在输入"接口。
//
// 若 Sender 未实现 [TypingNotifier]，返回 (nil, false)。
// 使用示例：
//
//	if tn, ok := platform.GetTypingNotifier(adapter); ok {
//	    tn.SendTyping(ctx, chatID)
//	}
func GetTypingNotifier(a Adapter) (TypingNotifier, bool) {
	tn, ok := a.Sender().(TypingNotifier)
	return tn, ok
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
	// Reactions 是否支持表情回应（Discord/Telegram/QQ 均支持）
	Reactions bool
	// ThreadReply 是否支持消息回复链/引用回复
	ThreadReply bool
	// TypingIndicator 是否支持"正在输入"状态
	TypingIndicator bool
	// MentionAll 是否支持 @全体成员
	MentionAll bool
	// VoiceChannel 是否支持语音频道（Discord Stage/VC）
	VoiceChannel bool
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

// RecoverableAdapter 可选接口：支持感知断连事件的适配器实现此接口。
//
// 适配器在 Start() 内部自动重连时，每次意外断连应调用已注册的 fn，
// 允许框架或应用层触发告警、更新监控指标等副作用。
//
// 使用示例：
//
//	if ra, ok := adapter.(platform.RecoverableAdapter); ok {
//	    unregister := ra.OnDisconnect(func(err error) {
//	        metrics.RecordDisconnect(adapter.Platform())
//	        logger.Warnf("adapter %s disconnected: %v", adapter.Platform(), err)
//	    })
//	    defer unregister() // 不再需要时注销回调
//	}
type RecoverableAdapter interface {
	Adapter
	// OnDisconnect 注册断连回调，返回注销函数。
	//
	// fn 在适配器每次意外断连时被调用，err 为断连原因。
	// 多次调用将追加（而非覆盖）回调，互不影响。
	// 调用返回的 unregister 函数可注销该特定回调；传入 nil 时为空操作。
	OnDisconnect(fn func(err error)) (unregister func())
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
//
// 注册表为空时返回 nil，避免无谓的切片分配。
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.adapters) == 0 {
		return nil
	}
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// Len 返回已注册适配器数量，无需分配切片。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adapters)
}

// Replace 原子替换指定平台的适配器，返回被替换的旧适配器。
//
// 无论原有适配器是否存在，新适配器都会被注册。
// 若该平台此前无适配器，returned old 为 nil，replaced 为 false。
//
// 典型用法（热替换运行中的适配器）：
//
//	old, ok := registry.Replace(newAdapter)
//	if ok {
//	    _ = old.Stop(ctx)
//	}
func (r *Registry) Replace(adapter Adapter) (old Adapter, replaced bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, replaced = r.adapters[adapter.Platform()]
	r.adapters[adapter.Platform()] = adapter
	return old, replaced
}

// SenderFor 返回指定平台的消息发送器。
//
// 若平台未注册，返回 (nil, false)。
func (r *Registry) SenderFor(platform string) (Sender, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	if !ok {
		return nil, false
	}
	return a.Sender(), true
}

// CapabilitiesFor 返回指定平台的能力声明。
//
// 若平台未注册，返回零值 Capabilities 和 false。
func (r *Registry) CapabilitiesFor(platform string) (Capabilities, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[platform]
	if !ok {
		return Capabilities{}, false
	}
	return a.Capabilities(), true
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
		// F-8: 若适配器支持断连通知，注册框架侧的告警 hook
		if ra, ok := a.(RecoverableAdapter); ok {
			_ = ra.OnDisconnect(func(err error) {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Warn("[Registry] Platform adapter disconnected, waiting for recovery")
			})
		}
		wg.Go(func() { // wg.Go 是 Go 1.25 新增方法；循环变量按值捕获依赖 Go 1.22+ 语义。
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

	// 收集全部非 context 取消/超时的错误，合并后返回
	var fatalErrs []error
	for _, err := range errs {
		if !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
			fatalErrs = append(fatalErrs, err)
		}
	}
	return errors.Join(fatalErrs...)
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
