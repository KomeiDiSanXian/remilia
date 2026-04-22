package platform

// send.go — 消息发送核心类型与接口
//
// 包含：
//   - [SendResult]  — 发送结果
//   - [SendRequest] — 发送请求信封
//   - [Sender]      — 核心发送接口
//   - [NoopSender]  — 空实现（测试用）
//   - 消息操作可选接口：[MessageEditor]、[MessageDeleter]、
//     [ReactionSender]（含 [EmojiKind]、[Emoji]）、[TypingNotifier]
//   - 对应辅助函数：[GetEditor]、[GetDeleter]、[GetReactionSender]、[GetTypingNotifier]

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
)

// ─────────────────────────────────────────────────────��──────────────────────
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

// NoopSender 空实现，用于测试或不需要发送能力的场景
type NoopSender struct{}

// Send 什么也不做，始终返回零值 SendResult 和 nil
func (n *NoopSender) Send(_ stdctx.Context, _ SendRequest) (SendResult, error) {
	return SendResult{}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 消息操作可选接口（MessageEditor / MessageDeleter / ReactionSender / TypingNotifier）
// ────────────────────────────────────────────────────────────────────────────

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

// ReactionSender 可选接口，支持表情回应操作的平台实现此接口。
//
// 使用前用类型断言检查支持：
//
//	if rs, ok := platform.GetReactionSender(adapter); ok {
//	    rs.AddReaction(ctx, chatID, messageID, platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"})
//	}
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

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数：安全获取可选接口
// ────────────────────────────────────────────────────────────────────────────

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
