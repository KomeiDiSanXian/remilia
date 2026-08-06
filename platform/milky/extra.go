package milky

import "github.com/KomeiDiSanXian/remilia/platform"

// ────────────────────────────────────────────────────────────────────────────
// 消息场景常量（导出供应用层使用）
// ────────────────────────────────────────────────────────────────────────────

// Milky 消息场景常量，用于 [MessageExtra.Scene] 字段。
const (
	// SceneGroup 群消息场景
	SceneGroup = sceneGroup
	// SceneFriend 好友消息场景
	SceneFriend = sceneFriend
	// SceneTemp 临时会话消息场景
	SceneTemp = sceneTemp
)

// ────────────────────────────────────────────────────────────────────────────
// 平台特定的令牌键（存储在 platform.ChatInfo.Tokens 中）
// ────────────────────────────────────────────────────────────────────────────

// 这些常量供适配器内部使用，应用层代码通常无需直接访问。
const (
	// TokenMessageScene 在 ChatInfo.Tokens 中存储 Milky 消息场景（"friend"、"group"、"temp"），
	// 以便发送方无需重新解析 ID 即可正确路由。
	TokenMessageScene = "milky_message_scene"

	// TokenFriendUID 存储好友请求 API 所需的好友 UID（字符串）。
	TokenFriendUID = "milky_friend_uid"

	// TokenNotificationSeq 存储群通知序列号。
	TokenNotificationSeq = "milky_notification_seq"

	// TokenNotificationType 存储群通知类型字符串。
	TokenNotificationType = "milky_notification_type"

	// TokenInvitationSeq 存储群邀请序列号。
	TokenInvitationSeq = "milky_invitation_seq"
)

// ────────────────────────────────────────────────────────────────────────────
// Milky 特有发送消息段类型
// ────────────────────────────────────────────────────────────────────────────

// OutgoingSegment 是 Milky 平台特有发送消息段的标记接口。
//
// 通过 [MessageExtra.Segments] 传入，由 [ApplyExtra] 注入后
// Milky Sender 会在构建出站消息时追加到消息段列表末尾；
// 也可直接传入 [Adapter.SendPrivateMessage] / [Adapter.SendGroupMessage]。
//
// 已实现类型：
// [TextSegment]、[MentionSegment]、[MentionAllSegment]、[ReplySegment]、
// [ImageSegment]、[RecordSegment]、[VideoSegment]、
// [FaceSegment]、[LightAppSegment]、[ForwardSegment]。
type OutgoingSegment interface {
	milkyOutgoingSegment() // 标记方法（包内可见）
}

// TextSegment 表示一个纯文本消息段。
type TextSegment struct {
	Text string
}

func (*TextSegment) milkyOutgoingSegment() {}

// MentionSegment 表示一个 @单人提及消息段。
//
// 若要 @全体成员，请使用 [MentionAllSegment]。
type MentionSegment struct {
	UserID int64
}

func (*MentionSegment) milkyOutgoingSegment() {}

// ReplySegment 表示一个引用回复消息段（通常放在消息最前面）。
type ReplySegment struct {
	MessageSeq int64
}

func (*ReplySegment) milkyOutgoingSegment() {}

// ImageSegment 表示一个图片消息段。
//
// URI 支持 file://、http(s):// 或 base64:// 前缀。
// SubType 为图片子类型（"normal" 或 "sticker"），留空时使用 "normal"。
type ImageSegment struct {
	URI     string
	SubType string // "normal" 或 "sticker"；默认 "normal"
	Summary string // 图片摘要文本（可选）
}

func (*ImageSegment) milkyOutgoingSegment() {}

// RecordSegment 表示一个语音/录音消息段。
//
// URI 支持 file:// 、http(s):// 或 base64:// 前缀。
type RecordSegment struct {
	URI      string
	ThumbURI string // 封面图 URI（可选）
}

func (*RecordSegment) milkyOutgoingSegment() {}

// VideoSegment 表示一个视频消息段。
//
// URI 支持 file:// 、http(s):// 或 base64:// 前缀。
type VideoSegment struct {
	URI      string
	ThumbURI string // 缩略图 URI（可选）
}

func (*VideoSegment) milkyOutgoingSegment() {}

// FaceSegment 表示一个 QQ 表情消息段。
//
// 使用示例：
//
//	msg := milky.ApplyExtra(
//	    platform.TextMessage(""),
//	    milky.MessageExtra{
//	        Segments: []milky.OutgoingSegment{
//	            &milky.FaceSegment{FaceID: "21", IsLarge: false},
//	        },
//	    },
//	)
type FaceSegment struct {
	FaceID  string
	IsLarge bool
}

func (*FaceSegment) milkyOutgoingSegment() {}

// MentionAllSegment 表示 @全体成员 消息段。
//
// 使用示例：
//
//	msg := milky.ApplyExtra(
//	    platform.TextMessage("注意"),
//	    milky.MessageExtra{
//	        Segments: []milky.OutgoingSegment{&milky.MentionAllSegment{}},
//	    },
//	)
type MentionAllSegment struct{}

func (*MentionAllSegment) milkyOutgoingSegment() {}

// LightAppSegment 表示一个小程序（light_app）消息段。
//
// JSONPayload 为小程序 JSON 数据字符串。
type LightAppSegment struct {
	JSONPayload string
}

func (*LightAppSegment) milkyOutgoingSegment() {}

// ForwardEntry 是合并转发消息中的单条消息条目。
//
// 通过 Text 设置简单文本内容，或通过 Segments 设置复杂消息段（两者互斥，
// 若 Segments 非空则忽略 Text）。
type ForwardEntry struct {
	UserID     int64
	SenderName string
	Text       string            // 简单文本（与 Segments 互斥）
	Segments   []OutgoingSegment // 复杂消息段（优先于 Text）
}

// ForwardSegment 表示一个合并转发消息段。
//
// 使用示例：
//
//	fwd := &milky.ForwardSegment{
//	    Messages: []milky.ForwardEntry{
//	        {UserID: 10001, SenderName: "Alice", Text: "hello"},
//	        {UserID: 10002, SenderName: "Bob",   Text: "world"},
//	    },
//	    Title:   "聊天记录",
//	    Summary: "查看 2 条消息",
//	}
type ForwardSegment struct {
	Messages []ForwardEntry
	Title    string   // 合并转发标题（可选）
	Preview  []string // 预览文本，1～4 条（可选）
	Summary  string   // 摘要（可选）
	Prompt   string   // 预览外显文本，仅移动端有效（可选）
}

func (*ForwardSegment) milkyOutgoingSegment() {}

// ────────────────────────────────────────────────────────────────────────────
// MessageExtra — Milky 特有的消息发送选项
// ────────────────────────────────────────────────────────────────────────────

// MessageExtra 保存发送出站消息时的 Milky 特有选项。
//
// 通过 [ApplyExtra] 注入；Milky Sender 通过 [extractExtra] 取回。
//
// 示例：
//
//	msg := milky.ApplyExtra(
//	    platform.TextMessage("hello"),
//	    milky.MessageExtra{Scene: milky.SceneTemp},
//	)
type MessageExtra struct {
	// Scene 覆盖从 chat ID 推导出的消息场景。
	// 用于向临时会话发送消息。留空则自动检测。
	Scene string

	// Segments 附加的 Milky 特有消息段（在标准内容之后追加）。
	// 支持所有 [OutgoingSegment] 实现类型，包括：
	// [TextSegment]、[MentionSegment]、[MentionAllSegment]、[ReplySegment]、
	// [ImageSegment]、[RecordSegment]、[VideoSegment]、
	// [FaceSegment]、[LightAppSegment]、[ForwardSegment]。
	Segments []OutgoingSegment
}

const milkyExtraKey = "__milky_message_extra__"

// ApplyExtra 将 Milky 特有选项注入到 OutboundMessage 中。
//
// 返回一个新消息，原消息不会被修改。
func ApplyExtra(msg platform.OutboundMessage, extra MessageExtra) platform.OutboundMessage {
	return msg.WithExtra(milkyExtraKey, extra)
}

// extractExtra 从 OutboundMessage 中取回 Milky 特有选项。
func extractExtra(msg platform.OutboundMessage) MessageExtra {
	if msg.Extra == nil {
		return MessageExtra{}
	}
	v, ok := msg.Extra[milkyExtraKey]
	if !ok {
		return MessageExtra{}
	}
	e, _ := v.(MessageExtra)
	return e
}

// ────────────────────────────────────────────────────────────────────────────
// 附件元数据类型（入站消息）
// ────────────────────────────────────────────────────────────────────────────

// ImageSegmentMeta 是图片类型 Attachment 的 Extra 载荷。
//
// 通过类型断言访问：
//
//	if meta, ok := att.Extra.(*milky.ImageSegmentMeta); ok {
//	    id := meta.ResourceID
//	}
// ────────────────────────────────────────────────────────────────────────────
// 附件扩展键（Attachment.Extra 的键名常量）
// ────────────────────────────────────────────────────────────────────────────

// 各段类型的元数据在 Attachment.Extra 中使用的键名。
const (
	ExtraKeyImage      = "image"
	ExtraKeyRecord     = "record"
	ExtraKeyVideo      = "video"
	ExtraKeyFile       = "file"
	ExtraKeyFace       = "face"
	ExtraKeyMarketFace = "market_face"
	ExtraKeyLightApp   = "light_app"
	ExtraKeyXML        = "xml"
)

type ImageSegmentMeta struct {
	ResourceID string
	SubType    string // "normal" 或 "sticker"
}

// RecordSegmentMeta 是语音/录音类型 Attachment 的 Extra 载荷。
type RecordSegmentMeta struct {
	ResourceID string
	Duration   int // 秒
}

// VideoSegmentMeta 是视频类型 Attachment 的 Extra 载荷。
type VideoSegmentMeta struct {
	ResourceID string
	Duration   int // 秒
}

// FileSegmentMeta 是文件类型 Attachment 的 Extra 载荷。
type FileSegmentMeta struct {
	FileID   string
	FileName string
	FileSize int64
	FileHash string // TriSHA1 哈希值，仅私聊文件存在
}

// FaceSegmentMeta 是 QQ 表情类型 Attachment 的 Extra 载荷。
type FaceSegmentMeta struct {
	FaceID  string
	IsLarge bool
}

// MarketFaceSegmentMeta 是市场表情类型 Attachment 的 Extra 载荷。
type MarketFaceSegmentMeta struct {
	EmojiPackageID int
	EmojiID        string
	Key            string
	Summary        string
	URL            string
}

// LightAppSegmentMeta 是小程序类型 Attachment 的 Extra 载荷。
type LightAppSegmentMeta struct {
	AppName     string
	JSONPayload string
}

// XMLSegmentMeta 是 XML 消息类型 Attachment 的 Extra 载荷。
type XMLSegmentMeta struct {
	ServiceID  int
	XMLPayload string
}
