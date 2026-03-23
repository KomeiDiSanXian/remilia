package platform

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ────────────────────────────────────────────────────────────────────────────
// SendResult
// ────────────────────────────────────────────────────────────────────────────

// SendResult 消息发送成功后的响应摘要。
//
// MessageID 与 Timestamp 是各平台响应中最常用的热点字段，直接暴露为强类型，
// 可以直接用于撤回（MessageDeleter.Delete）、编辑（MessageEditor.Edit）及日志追踪。
//
// 平台专属的额外字段（如 QQ 富媒体的 file_info / ttl）通过 Raw 携带，
// 调用方用类型断言按需访问：
//
//	if r, ok := result.Raw.(*qq.QQSendResult); ok {
//	    fileInfo := r.FileInfo // 富媒体 file_info token
//	}
//
// 已知 Raw 类型：
//   - *qq.QQSendResult：QQ 平台响应（含 FileInfo、FileUUID、TTL 等富媒体字段）
type SendResult struct {
	// MessageID 平台返回的已发送消息唯一 ID。
	// 用于后续撤回/编辑等操作。富媒体上传且 srv_send_msg=false 时为空字符串。
	MessageID string

	// Timestamp 平台确认的消息发送时间（零值表示平台未返回）。
	Timestamp time.Time

	// Platform 来源平台标识符（如 "qq"、"discord"、"telegram"）。
	Platform string

	// Raw 平台专属响应的完整原始数据，由各平台适配器负责填充。
	// 调用方通过类型断言获取平台特定字段；不需要平台特定字段时可忽略。
	Raw any
}

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

// Validate 校验 SendRequest 是否合法，供 Sender 实现复用，避免重复检查。
//
// 当 Target.ID 为空时返回 [errutil.ErrNoChatInfo]；
// 当 Message 没有任何可发送内容时返回 [errutil.ErrEmptyMessage]；
// 当附件同时设置 URL 和 Data（互斥），或两者均为空时返回 [errutil.ErrInvalidMessage]。
//
// 使用示例（Sender 实现）：
//
//	func (s *mySender) Send(ctx context.Context, req platform.SendRequest) error {
//	    if err := req.Validate(); err != nil {
//	        return err
//	    }
//	    // ... 平台特定发送逻辑
//	}
func (r SendRequest) Validate() error {
	if r.Target.ID == "" {
		return errutil.ErrNoChatInfo
	}
	if r.Message.IsEmpty() {
		return errutil.ErrEmptyMessage
	}
	for i, att := range r.Message.Attachments {
		if att.URL != "" && len(att.Data) > 0 {
			return fmt.Errorf("%w: attachment[%d] has both URL and Data set (mutually exclusive)", errutil.ErrInvalidMessage, i)
		}
		if att.URL == "" && len(att.Data) == 0 {
			return fmt.Errorf("%w: attachment[%d] must have either URL or Data", errutil.ErrInvalidMessage, i)
		}
	}
	return nil
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
	// Send 发送消息，返回平台响应摘要与错误。
	//
	// 成功时 SendResult.MessageID 包含平台分配的消息 ID（可用于撤回/编辑）；
	// 平台未返回 ID 时 MessageID 为空字符串（不影响发送本身的成功状态）。
	// 路由信息由 req 显式传入，ctx 仅用于取消/超时/tracing。
	//
	// 若 req.Target.ID 为空，实现者应返回 errutil.ErrNoChatInfo。
	Send(ctx stdctx.Context, req SendRequest) (SendResult, error)
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
//	    rs.AddReaction(ctx, chatID, messageID, platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"})
//	}

// EmojiKind 平台无关的 emoji 种类枚举。
type EmojiKind string

const (
	// EmojiKindUnicode 标准 Unicode 表情（如 "👍"），Value 字段填 Unicode 字符
	EmojiKindUnicode EmojiKind = "unicode"
	// EmojiKindCustom 平台自定义表情（如 Discord 的 name:id 格式），需 ID + Value
	EmojiKindCustom EmojiKind = "custom"
	// EmojiKindSystem 平台内置系统表情（如 QQ 内置表情，需 ID 字段）
	EmojiKindSystem EmojiKind = "system"
)

// Emoji 平台无关的表情标识，用于 ReactionSender 接口。
//
// 各平台 Sender 根据 Kind 将其映射到平台特定格式：
//   - Discord:  unicode → Value 直接传入；custom → "Value:ID"
//   - Telegram: unicode → Value；custom → 自定义 emoji ID
//   - QQ:       system  → (emojiType=1, emojiID=ID)；unicode → (emojiType=2, emojiID=Value)
//
// 使用示例：
//
//	// Unicode 点赞
//	platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"}
//	// Discord 自定义 emoji
//	platform.Emoji{Kind: platform.EmojiKindCustom, ID: "123456789", Value: "myEmoji"}
//	// QQ 内置系统表情（表情 ID=405）
//	platform.Emoji{Kind: platform.EmojiKindSystem, ID: "405"}
type Emoji struct {
	// Kind 表情种类
	Kind EmojiKind
	// ID 平台内部 emoji ID。
	// 标准 Unicode 表情此字段为空，直接使用 Value。
	ID string
	// Value emoji 字面量或名称。
	// Unicode 表情填字符本身（如 "👍"）；自定义表情填显示名称（如 "myEmoji"）。
	Value string
}

type ReactionSender interface {
	// AddReaction 为指定消息添加表情回应。
	// chatID 为目标会话 ID，messageID 为平台原生消息 ID，emoji 为平台无关表情标识。
	AddReaction(ctx stdctx.Context, chatID, messageID string, emoji Emoji) error
	// RemoveReaction 移除指定消息上的表情回应。
	RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji Emoji) error
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

// Send 什么也不做，始终返回零值 SendResult 和 nil
func (n *NoopSender) Send(_ stdctx.Context, _ SendRequest) (SendResult, error) {
	return SendResult{}, nil
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
//	    rs.AddReaction(ctx, chatID, messageID, platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"})
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

	// ── 量化限制（0 = 无已知限制或平台未公开）────────────────────────────

	// MaxTextLength 单条文本消息最大字符数。
	// 例：Discord=2000，Telegram=4096，QQ=0（未公开）。
	MaxTextLength int
	// MaxAttachmentMB 单个附件最大大小（MB）。
	// 例：Discord=8，Telegram=50，QQ=0（未公开）。
	MaxAttachmentMB int
	// MaxButtonsPerRow 每行最多按钮数（0=无已知限制）。
	// 例：Discord/QQ=5。
	MaxButtonsPerRow int
	// MaxButtonRows 最多按钮行数（0=无已知限制）。
	// 例：Discord/QQ=5。
	MaxButtonRows int
	// MaxEmbedFields 单个 Embed 最多字段数（0=无已知限制）。
	// 例：Discord=25。
	MaxEmbedFields int
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
// BotIdentity（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// BotIdentity 是机器人自身身份信息的可选接口。
//
// 支持获取机器人自身 ID/名称的平台适配器应实现此接口，
// 便于 Handler 做"防止自回复"判断、日志标注等操作。
//
// 使用示例：
//
//	// 防止自回复
//	if botID := platform.GetBotID(adapter); botID != "" {
//	    if event.Sender().ID == botID {
//	        return // 忽略自身发出的消息
//	    }
//	}
//
//	// 直接类型断言（需要同时访问多个字段时更高效）
//	if bi, ok := adapter.(platform.BotIdentity); ok {
//	    log.Printf("bot %s (%s) online", bi.BotName(), bi.BotID())
//	}
type BotIdentity interface {
	// BotID 返回机器人在当前平台的唯一标识符。
	//
	// 与 event.Sender().ID 对比可判断事件是否由机器人自身触发。
	// 平台未提供或尚未连接时返回空字符串。
	BotID() string

	// BotName 返回机器人的显示名称（昵称/用户名）。
	//
	// 平台未提供时返回空字符串。
	BotName() string
}

// GetBotID 安全获取适配器的机器人唯一 ID。
//
// 若适配器未实现 [BotIdentity] 或平台尚未返回 ID，返回空字符串。
//
// 使用示例：
//
//	if platform.GetBotID(adapter) == event.Sender().ID {
//	    return // 忽略自身发出的消息
//	}
func GetBotID(a Adapter) string {
	if bi, ok := a.(BotIdentity); ok {
		return bi.BotID()
	}
	return ""
}

// GetBotName 安全获取适配器的机器人显示名称。
//
// 若适配器未实现 [BotIdentity]，返回空字符串。
func GetBotName(a Adapter) string {
	if bi, ok := a.(BotIdentity); ok {
		return bi.BotName()
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// AdapterObserver
// ────────────────────────────────────────────────────────────────────────────

// AdapterObserver 接收 Registry 适配器生命周期事件，用于可观测性集成。
//
// 所有方法均在调用方 goroutine 中**同步**执行，实现应保证非阻塞（如仅递增计数器）。
// 如需进行耗时操作（日志写入、网络调用），请在实现内部使用异步队列。
//
// 使用示例（注册 metrics 观察者）：
//
//	reg := platform.NewRegistry().WithObserver(mc.PlatformObserver())
//	reg.StartAll(ctx, handler)
type AdapterObserver interface {
	// OnAdapterStarted 适配器 goroutine 启动时调用（Start 开始阻塞前）。
	OnAdapterStarted(platform string)
	// OnAdapterStopped 适配器 goroutine 退出时调用（无论是否有错误）。
	OnAdapterStopped(platform string)
	// OnAdapterError 适配器以非 context 取消/超时错误退出时调用。
	// errMsg 为 error.Error() 文本，避免直接传递 error 接口引起 alloc。
	OnAdapterError(platform, errMsg string)
	// OnAdapterDisconnect RecoverableAdapter 意外断连时调用。
	OnAdapterDisconnect(platform string, err error)
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
	observer AdapterObserver // optional, nil = no-op

	// disconnectUnregs 保存每个平台 RecoverableAdapter 的断连回调注销函数。
	// StartAll 每次启动前先调用旧的注销函数，再注册新的，防止多次调用时回调累积。
	// StopAll 完成后统一清理，释放对 Registry 的引用，避免 GC 泄漏。
	disconnectUnregs map[string]func()
}

// NewRegistry 创建空的适配器注册表
func NewRegistry() *Registry {
	return &Registry{
		adapters:         make(map[string]Adapter),
		disconnectUnregs: make(map[string]func()),
	}
}

// WithObserver 注册适配器生命周期观察者，返回 *Registry 支持链式调用。
//
// 必须在 StartAll 之前调用；并发调用是线程安全的（使用写锁）。
// 传入 nil 表示清除当前观察者。
func (r *Registry) WithObserver(o AdapterObserver) *Registry {
	r.mu.Lock()
	r.observer = o
	r.mu.Unlock()
	return r
}

// notifyObserver 内部帮助函数，持有读锁调用 observer。
func (r *Registry) notifyObserver(fn func(AdapterObserver)) {
	r.mu.RLock()
	o := r.observer
	r.mu.RUnlock()
	if o != nil {
		fn(o)
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
// 无论注册表是否为空，始终返回非 nil 切片，保持与 Go 惯例的一致性。
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
		// 若适配器支持断连通知，注册框架侧的告警 hook。
		// 先注销旧的注册（防止多次调用 StartAll 时回调累积），再注册新的。
		if ra, ok := a.(RecoverableAdapter); ok {
			r.mu.Lock()
			if old, exists := r.disconnectUnregs[a.Platform()]; exists && old != nil {
				old() // 注销旧回调，释放对 Registry 的引用
			}
			r.mu.Unlock()

			unregister := ra.OnDisconnect(func(err error) {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Warn("[Registry] Platform adapter disconnected, waiting for recovery")
				r.notifyObserver(func(o AdapterObserver) { o.OnAdapterDisconnect(a.Platform(), err) })
			})

			r.mu.Lock()
			r.disconnectUnregs[a.Platform()] = unregister
			r.mu.Unlock()
		}
		wg.Go(func() {
			r.notifyObserver(func(o AdapterObserver) { o.OnAdapterStarted(a.Platform()) })
			err := a.Start(ctx, handler)
			r.notifyObserver(func(o AdapterObserver) { o.OnAdapterStopped(a.Platform()) })
			if err != nil {
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Error("[Registry] Platform adapter exited with error")
				// 仅对非 ctx 取消/超时的 fatal error 通知 observer（日常 ctx 退出不算错误）
				if !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
					r.notifyObserver(func(o AdapterObserver) { o.OnAdapterError(a.Platform(), err.Error()) })
				}
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
// 停止完成后统一清理断连回调注销函数，释放对 Registry 的内部引用，避免 GC 泄漏。
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

	// 所有适配器已停止，注销断连回调，释放闭包对 Registry 的引用
	r.mu.Lock()
	for k, unreg := range r.disconnectUnregs {
		if unreg != nil {
			unreg()
		}
		delete(r.disconnectUnregs, k)
	}
	r.mu.Unlock()

	return errors.Join(errs...)
}
