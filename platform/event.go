// Package platform 定义平台无关的消息与事件抽象层。
//
// 设计目标：
//   - 将框架核心（engine、context）与具体平台（QQ官方、Discord、Telegram 等）解耦
//   - 现有 QQ 适配器通过 platform/qq 包实现本接口，向后兼容
//   - 新平台只需实现 Adapter + Event 接口，无需改动核心引擎
//
// 层次结构：
//
//	┌──────────────┐
//	│   Bot/Engine │  使用 platform.Event / platform.Adapter
//	├──────────────┤
//	│  platform/   │  接口定义（本包）
//	├──────────────┤
//	│  platform/qq │  QQ 官方实现
//	│  platform/.. │  其他平台实现
//	└──────────────┘
package platform

import "time"

// EventKind 平台无关的事件类别枚举。
//
// 每个平台的具体事件类型（如 dto.EventType）映射到此枚举，
// 供 Engine 的 Matcher 做通用路由（无需感知平台细节）。
type EventKind string

const (
	// EventKindUnknown 未知/未映射事件
	EventKindUnknown EventKind = "UNKNOWN"
	// EventKindPrivateMessage 私聊消息（QQ C2C、Telegram 私聊、Discord DM 等）
	EventKindPrivateMessage EventKind = "PRIVATE_MESSAGE"
	// EventKindGroupMessage 群组消息（QQ 群、Discord 频道等）
	EventKindGroupMessage EventKind = "GROUP_MESSAGE"
	// EventKindGuildMessage 频道/服务器消息（QQ频道、Discord 服务器等）
	EventKindGuildMessage EventKind = "GUILD_MESSAGE"
	// EventKindNotice 通知类事件（通用，平台无法精确归类时使用）
	EventKindNotice EventKind = "NOTICE"
	// EventKindRequest 请求类事件（加好友请求、加群请求等）
	EventKindRequest EventKind = "REQUEST"
	// EventKindSystem 系统事件（Ready、Resumed 等）
	EventKindSystem EventKind = "SYSTEM"
	// EventKindInteraction 交互事件（按钮回调、斜杠命令、下拉菜单等）
	//
	// Discord Interaction、QQ 机器人 v2 按钮回调、Telegram 内联键盘回调。
	EventKindInteraction EventKind = "INTERACTION"
	// EventKindReaction 消息表情回应（添加或移除）
	//
	// Discord 表情回应、Telegram 表情回应、QQ 表情回应。
	EventKindReaction EventKind = "REACTION"
	// EventKindMemberJoin 成员加入群组/服务器事件
	EventKindMemberJoin EventKind = "MEMBER_JOIN"
	// EventKindMemberLeave 成员离开/被踢出群组/服务器事件
	EventKindMemberLeave EventKind = "MEMBER_LEAVE"
	// EventKindMessageUpdate 消息被编辑
	//
	// Discord 消息编辑、Telegram 消息编辑。
	EventKindMessageUpdate EventKind = "MESSAGE_UPDATE"
	// EventKindMessageDelete 消息被撤回/删除
	EventKindMessageDelete EventKind = "MESSAGE_DELETE"
)

// UserInfo 代表消息发送者/用户的基本信息。
//
// 各平台填充能力不同，未知字段返回空字符串或 false。
type UserInfo struct {
	// ID 平台内唯一用户标识（QQ openID、Telegram userID 等）
	ID string
	// DisplayName 用户显示名（昵称/用户名）
	DisplayName string
	// IsBot 是否为机器人账号
	IsBot bool
}

// ChatInfo 代表消息所在会话的基本信息。
type ChatInfo struct {
	// ID 会话/群组/频道唯一标识
	// 私聊：用户 ID；群组：群 ID；频道/话题：channel_id
	ID string
	// ParentID 父容器唯一标识（服务器/频道层级时使用）。
	//
	// 频道消息：guild_id / 服务器 ID
	// Discord: guild_id；QQ 频道: guild_id
	// 私聊和普通群组为空字符串。
	ParentID string
	// Name 会话名称（可选，部分平台不提供）
	Name string
	// IsGroup 是否为群组/频道消息（false = 私聊）
	IsGroup bool
	// IsDM 是否为私信（Direct Message）会话。
	//
	// 与 IsGroup=true、ParentID 非空同时成立时，
	// 表示这是一条频道私信（如 QQ DIRECT_MESSAGE_CREATE），
	// 发送回复时应使用 DM 专属接口而非普通频道消息接口。
	IsDM bool
}

// InboundAttachment 入站消息中携带的附件（平台无关抽象）。
//
// 各平台填充能力不同，无法提供的字段返回零值。
type InboundAttachment struct {
	// URL 附件远程 URL（平台托管；部分平台的 URL 有时效，勿长期持有）
	URL string
	// MimeType MIME 类型，如 "image/png"（平台不提供时为空字符串）
	MimeType string
	// Name 文件名（平台不提供时为空字符串）
	Name string
	// Size 文件大小（字节），平台不提供时为 0
	Size int
	// Width 图片/视频宽度（像素），非媒体类型或平台不提供时为 0
	Width int
	// Height 图片/视频高度（像素），非媒体类型或平台不提供时为 0
	Height int
	// VoiceWavURL 语音附件的 WAV 格式播放链接（仅 QQ 平台语音消息携带）。
	// 非语音类型或平台不提供时为空字符串。
	VoiceWavURL string
	// AsrText 语音附件的 ASR（自动语音识别）参考文本（仅 QQ 平台语音消息携带）。
	// 非语音类型或平台不提供时为空字符串。
	AsrText string
}

// Event 是平台无关的事件抽象接口（最小必要集合）。
//
// 各平台适配器将原始 payload 包装为 Event 实现，
// 框架核心只依赖此接口，不直接引用任何平台特定结构体。
//
// 平台特定或可选功能通过独立接口扩展：
//   - [RawEvent]：访问平台原始类型字符串和 payload
//   - [EditableEvent]：判断消息是否为编辑版本
//   - [ReplyEvent]：获取被回复消息的 ID（回复链/消息线程）
//
// 使用类型断言检测可选能力：
//
//	if re, ok := event.(platform.ReplyEvent); ok {
//	    replyID := re.ReplyToID()
//	}
//
// 或使用包级帮助函数（优先推荐）：
//
//	rawType := platform.RawType(event)   // 若不支持则返回 ""
//	replyID := platform.GetReplyToID(event) // 若不支持则返回 ""
type Event interface {
	// Platform 返回平台标识符（如 "qq"、"discord"、"telegram"）
	Platform() string

	// Kind 返回平台无关的事件类别
	Kind() EventKind

	// ID 返回平台级别的唯一事件标识符。
	//
	// 用途：去重、追踪、死信队列等需要唯一标识的场景。
	// 平台不提供时返回空字符串；调用方应对空字符串做兼容处理。
	ID() string

	// Sender 返回消息发送者信息
	Sender() UserInfo

	// Chat 返回消息所在会话信息
	Chat() ChatInfo

	// Content 返回消息文本内容（纯文本，不含平台特定格式）
	Content() string

	// Timestamp 返回事件时间戳（尽力而为，平台不提供时返回零值）
	Timestamp() time.Time

	// Attachments 返回消息中携带的附件列表。
	//
	// 平台不支持附件或消息无附件时返回 nil。
	Attachments() []InboundAttachment
}

// ────────────────────────────────────────────────────────────────────────────
// 可选扩展接口
// ────────────────────────────────────────────────────────────────────────────

// RawEvent 是平台特定数据的可选访问接口。
//
// 适配器在需要暴露底层 payload 或平台原生类型字符串时实现此接口。
// 框架核心代码通过 [RawType] / [RawPayload] 帮助函数安全访问，无需直接断言。
type RawEvent interface {
	// RawType 返回平台原始事件类型字符串（如 QQ 的 "C2C_MESSAGE_CREATE"）
	RawType() string

	// RawPayload 返回原始平台 payload（类型断言后可访问平台特定字段）
	//
	// 示例（QQ 平台）:
	//   if payload, ok := e.RawPayload().(*dto.Payload); ok { ... }
	RawPayload() any
}

// EditableEvent 是消息编辑感知的可选接口（D3）。
//
// 支持消息编辑事件的平台（Discord、Telegram）实现此接口。
// 使用示例：
//
//	if ee, ok := event.(platform.EditableEvent); ok && ee.IsEdited() {
//	    log.Printf("消息已编辑，原始时间戳: %v", ee.OriginalTimestamp())
//	}
type EditableEvent interface {
	// IsEdited 返回此事件是否为消息编辑事件
	IsEdited() bool
	// OriginalTimestamp 返回原始消息的发送时间戳（零值表示不可用）
	OriginalTimestamp() time.Time
}

// ReplyEvent 是消息回复链感知的可选接口（N7）。
//
// 支持消息回复（线程）的平台实现此接口。
// 框架通过 [GetReplyToID] 帮助函数安全访问，无需直接断言。
//
// 使用示例：
//
//	if id := platform.GetReplyToID(event); id != "" {
//	    // 此消息是对 id 的回复
//	}
type ReplyEvent interface {
	// ReplyToID 返回被回复消息的平台原生 ID。
	// 若此消息不是回复，或平台不提供此信息，返回空字符串。
	ReplyToID() string
}

// MentionsEvent 是 @ 用户列表感知的可选接口。
//
// 消息中携带 @ 用户列表（QQ group_at_message、Discord mentions、
// Telegram entities 中的 mention）时，适配器实现此接口。
// 框架通过 [GetMentions] 帮助函数安全访问，无需直接断言。
//
// 使用示例：
//
//	if mentions := platform.GetMentions(event); len(mentions) > 0 {
//	    for _, u := range mentions {
//	        log.Printf("@ 了用户 %s (%s)", u.DisplayName, u.ID)
//	    }
//	}
type MentionsEvent interface {
	// Mentions 返回消息中 @ 的用户列表（不含机器人自身）。
	// 无 @ 用户时返回 nil。
	Mentions() []UserInfo
}

// ────────────────────────────────────────────────────────────────────────────
// 可选接口帮助函数
// ────────────────────────────────────────────────────────────────────────────

// RawType 安全获取平台原始事件类型字符串。
//
// 若事件未实现 [RawEvent]，返回空字符串。
func RawType(e Event) string {
	if re, ok := e.(RawEvent); ok {
		return re.RawType()
	}
	return ""
}

// RawPayload 安全获取平台原始 payload。
//
// 若事件未实现 [RawEvent]，返回 nil。
func RawPayload(e Event) any {
	if re, ok := e.(RawEvent); ok {
		return re.RawPayload()
	}
	return nil
}

// GetReplyToID 安全获取被回复消息的 ID。
//
// 若事件未实现 [ReplyEvent] 或消息不是回复，返回空字符串。
func GetReplyToID(e Event) string {
	if re, ok := e.(ReplyEvent); ok {
		return re.ReplyToID()
	}
	return ""
}

// IsEdited 安全判断消息是否为编辑版本。
//
// 若事件未实现 [EditableEvent]，返回 false。
func IsEdited(e Event) bool {
	if ee, ok := e.(EditableEvent); ok {
		return ee.IsEdited()
	}
	return false
}

// GetMentions 安全获取消息中 @ 的用户列表。
//
// 若事件未实现 [MentionsEvent] 或消息没有 @ 用户，返回 nil。
func GetMentions(e Event) []UserInfo {
	if me, ok := e.(MentionsEvent); ok {
		return me.Mentions()
	}
	return nil
}
