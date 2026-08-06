package platform

import (
	"maps"
	"slices"
	"strings"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// 附件（Attachment）
// ────────────────────────────────────────────────────────────────────────────

// AttachmentKind 附件媒体类型枚举。
type AttachmentKind string

const (
	// AttachmentKindImage 图片
	AttachmentKindImage AttachmentKind = "image"
	// AttachmentKindAudio 音频/语音
	AttachmentKindAudio AttachmentKind = "audio"
	// AttachmentKindVideo 视频
	AttachmentKindVideo AttachmentKind = "video"
	// AttachmentKindFile  通用文件
	AttachmentKindFile AttachmentKind = "file"
)

// Attachment 单个附件（出站发送与入站接收共用）。
//
// 出站语义（Sender 使用）：
//   - URL 与 Data 互斥：URL 非空 → 远程 URL 发送；Data 非空 → 二进制直传
//   - 两者均为空时，Sender 应将其忽略
//
// 入站语义（Event.Attachments 返回）：
//   - 平台填充能力不同，无法提供的字段返回零值
//   - Size/Width/Height 为接收时元信息，出站时忽略（零值无害）
//
// 平台专属扩展元数据通过 Extra 携带（key-value 形式）：
//
//	att.Extra = map[string]any{"wav_url": ..., "asr_text": ...}   // QQ 语音
//	att.Extra["resource_id"] = "r1"                               // 平台资源 ID
type Attachment struct {
	// Kind 附件媒体类型（出站必填；入站平台可推断时填充）
	Kind AttachmentKind
	// URL 附件远程 URL（出站发送 / 入站接收）
	URL string
	// Data 本地二进制数据（出站直传，URL 为空时使用；入站为空）
	Data []byte
	// MimeType MIME 类型，如 "image/png"（可选，辅助平台正确处理）
	MimeType string
	// Name 文件名（与 Data 或 URL 配合；平台不支持时可忽略）
	Name string
	// Size 文件大小（字节），入站平台不提供时为 0
	Size int
	// Width 图片/视频宽度（像素），非媒体类型或平台不提供时为 0
	Width int
	// Height 图片/视频高度（像素），非媒体类型或平台不提供时为 0
	Height int
	// Extra 平台专属扩展元数据（key-value 形式），无扩展数据时为 nil。
	//
	// 已知键（见各平台 extra.go 中的常量定义）：
	//   - "voice":  QQ 语音附件的 WAV 链接与 ASR 文本（*qq.VoiceAttachmentMeta）
	//   - "button": QQ 按钮权限控制（*qq.ButtonExtra）
	Extra map[string]any
}

// ────────────────────────────────────────────────────────────────────────────
// 富文本嵌入卡片（Embed）
// ────────────────────────────────────────────────────────────────────────────

// EmbedField Embed 内的单个字段行。
type EmbedField struct {
	// Name 字段标题
	Name string
	// Value 字段内容
	Value string
	// Inline 是否与相邻字段同行展示（Discord 支持，其他平台忽略）
	Inline bool
}

// Embed 富文本嵌入卡片（Discord 风格，其他平台尽力映射）。
//
// 各平台支持程度：
//   - Discord: 原生支持全部字段
//   - Telegram: 映射为格式化文本消息，图片单独发送
//   - QQ: 映射为 Markdown 或纯文本（仅 Title/Description/Fields）
//   - 不支持的平台可安全忽略此字段
type Embed struct {
	// Title 标题
	Title string
	// Description 正文描述（支持 Markdown，平台不支持时降级为纯文本）
	Description string
	// URL 标题跳转链接（可选）
	URL string
	// Color 边框/主题颜色，RGB 十六进制无符号整数，如 0x5865F2（Discord 蓝）
	Color uint32
	// Fields 字段列表
	Fields []EmbedField
	// ImageURL 正文大图 URL
	ImageURL string
	// ThumbnailURL 右上角缩略图 URL
	ThumbnailURL string
	// FooterText 页脚文本
	FooterText string
	// Timestamp 时间戳（显示在页脚，零值表示不展示）
	Timestamp time.Time
}

// ────────────────────────────────────────────────────────────────────────────
// 交互按钮（Button）
// ────────────────────────────────────────────────────────────────────────────

// ButtonStyle 按钮样式枚举（各平台尽力映射到最接近的原生样式）
type ButtonStyle string

const (
	// ButtonStylePrimary 主要操作按钮（蓝色/强调色）
	ButtonStylePrimary ButtonStyle = "primary"
	// ButtonStyleSecondary 次要操作按钮（灰色/默认色）
	ButtonStyleSecondary ButtonStyle = "secondary"
	// ButtonStyleDanger 危险操作按钮（红色/警告色）
	ButtonStyleDanger ButtonStyle = "danger"
	// ButtonStyleLink 链接按钮（点击后跳转 URL）
	ButtonStyleLink ButtonStyle = "link"
)

// ButtonRowAuto 是 Button.Row 的零值，表示交由平台自动排列布局。
//
// Row=ButtonRowAuto 时，每个按钮各自独占一行（安全默认值）。
// 需要将多个按钮排在同一行时，为它们指定相同的 Row 值（1 ～ 5）。
const ButtonRowAuto = 0

// Button 代表一个平台无关的交互按钮。
//
// 适用于 Discord 消息组件、Telegram 内联键盘、QQ 机器人键盘等。
// 各平台 Sender 负责将此结构映射到平台特定格式；
// 不支持按钮的平台可忽略此字段。
type Button struct {
	// ID 按钮回调标识符（如 Discord 的 custom_id、Telegram 的 callback_data）
	ID string
	// Label 按钮显示文字
	Label string
	// URL 链接目标（Style 为 ButtonStyleLink 时有效）
	URL string
	// Command 指令按钮文本（如 "/help"）。
	//
	// 非空时按钮为"指令按钮"：点击后把命令插入输入框（如 QQ 的
	// action.type=2，自动插入 "@bot <Command>"），不产生交互回调事件，
	// 由用户自行发送。平台不支持指令按钮时忽略此字段。
	Command string
	// Style 按钮样式
	Style ButtonStyle
	// Disabled 按钮是否置灰不可点击（Discord/QQ 均支持）
	Disabled bool
	// Row 按钮所在行。
	//
	//   - ButtonRowAuto（0，零值默认）：由平台自动排列，每个此值的按钮独占一行
	//   - 1 ～ 5：显式行号，相同 Row 值的按钮排列在同一行（1 = 第一行）
	//
	// Discord 最多 5 行，每行最多 5 个按钮；超出部分截断。
	Row int
	// Emoji 按钮前展示的 emoji（Discord 原生支持，其他平台忽略）
	Emoji string
	// Extra 平台专属按钮扩展字段（key-value 形式）。
	//
	// 用于携带通用字段无法表达的平台特定配置，各平台 Sender 读取。
	// 不支持此字段的平台可安全忽略。
	//
	// 已知键（见各平台 extra.go 中的常量定义）：
	//   - "button": QQ 按钮权限控制（*qq.ButtonExtra）
	//   - "inline": Telegram switch_inline_query 等扩展字段（*telegram.InlineButtonExtra）
	Extra map[string]any
}

// ────────────────────────────────────────────────────────────────────────────
// 消息段（Segment）—— 跨平台消息的唯一真相源
// ────────────────────────────────────────────────────────────────────────────

// SegmentType 统一消息段类型。
type SegmentType string

const (
	// SegmentText 纯文本段
	SegmentText SegmentType = "text"
	// SegmentAt  @ 单个用户段（一个 at 一个 Segment，保序）
	SegmentAt SegmentType = "at"
	// SegmentMentionAll @ 全体成员段
	SegmentMentionAll SegmentType = "mention_all"
	// SegmentImage 图片段
	SegmentImage SegmentType = "image"
	// SegmentAudio 音频/语音段
	SegmentAudio SegmentType = "audio"
	// SegmentVideo 视频段
	SegmentVideo SegmentType = "video"
	// SegmentFile 文件段
	SegmentFile SegmentType = "file"
	// SegmentFace 表情段（贴图/内置表情，身份保留在 FaceID）
	SegmentFace SegmentType = "face"
	// SegmentReply 引用/回复段
	SegmentReply SegmentType = "reply"
	// SegmentForward 合并转发段
	SegmentForward SegmentType = "forward"
	// SegmentButton 交互按钮段
	SegmentButton SegmentType = "button"
	// SegmentUnknown 平台特有/未识别段（Extra 保留原始数据）
	SegmentUnknown SegmentType = "unknown"
)

// SegmentExtraKey 是 Segment.Extra 的通用键。
const (
	// SegmentExtraIsSelf 平台 payload 自带自我标记时（qq is_you、satori is_self）
	// 解析器在 at 段上标注 true，作为 botID 判定的覆盖路径。
	SegmentExtraIsSelf = "is_self"
	// SegmentExtraFromPlatform 段来源平台标记（防御性，防跨平台误透传）。
	SegmentExtraFromPlatform = "from_platform"
	// SegmentExtraTitle forward 段摘要标题（跨平台降级用）。
	SegmentExtraTitle = "title"
	// SegmentExtraSummary forward 段摘要文本（跨平台降级用）。
	SegmentExtraSummary = "summary"
)

// Segment 一条原子消息段，保留原文顺序。
//
// 入站：各平台解析器输出；出站：OutboundMessage.Segments 复用同一类型
// （image/audio/video/file 段通过 Attachment.URL/Data 表达出站载荷）。
//
// 派生规则（唯一真相源 → 便捷视图）：
//   - SegmentsContent    段 → 纯文本（at/mention_all/face/reply/forward/button/unknown 剥离）
//   - SegmentsAttachments 段 → 附件列表（仅 image/audio/video/file）
//   - SegmentsMentions   段 → 被 @ 用户聚合视图（保序去重，含自身 IsSelf=true）
type Segment struct {
	// Type 段类型
	Type SegmentType
	// Text text 段内容 / at 段的显示文本（平台提供时）
	Text string
	// UserID at 段的目标用户 ID
	UserID string
	// ReplyToID reply 段的目标消息 ID
	ReplyToID string
	// Attachment image/audio/video/file 段载荷（复用统一附件类型）
	Attachment Attachment
	// FaceID face 段的表情 ID
	FaceID string
	// Extra 平台特有段数据（forward id、button 结构、ARK payload 等），
	// 通用键见 SegmentExtraKey 常量。
	Extra map[string]any
}

// ────────────────────────────────────────────────────────────────────────────
// 派生辅助（统一实现，保证 Content/Attachments 与段一致）
// ────────────────────────────────────────────────────────────────────────────

// SegmentsContent 将有序消息段拼接为纯文本。
//
// 规则：
//   - text 段：直接拼接 Text
//   - at / mention_all：**剥离**（不进入 Content）——统一后各平台 Content
//     命令友好（OnCommand 可匹配 "@机器人 /ping"），修复平台间行为分裂
//   - face / reply / forward / button / unknown：跳过
//   - image/audio/video/file：跳过（无文本）
//
// 本函数对段不做任何过滤（含 @ 机器人自身的段也在段列表中）。
func SegmentsContent(segs []Segment) string {
	var sb strings.Builder
	for _, s := range segs {
		if s.Type == SegmentText {
			sb.WriteString(s.Text)
		}
	}
	return sb.String()
}

// SegmentsAttachments 从有序消息段中提取附件列表（仅 image/audio/video/file，按出现顺序）。
func SegmentsAttachments(segs []Segment) []Attachment {
	var out []Attachment
	for _, s := range segs {
		switch s.Type {
		case SegmentImage, SegmentAudio, SegmentVideo, SegmentFile:
			out = append(out, s.Attachment)
		}
	}
	return out
}

// SegmentsMentions 从有序消息段中提取被 @ 用户聚合视图（保序去重）。
//
// botID 为机器人自身 ID（适配器 GetBotID），用于推导 IsSelf；
// 无法推导时（botID 为空）以 Segment.Extra[SegmentExtraIsSelf] 覆盖为准。
//
// 与现状 Mentions() 语义一致（satori/qq 均保留自身并标记 IsSelf=true）：
//   - **包含**被 @ 的机器人自身（IsSelf=true）——OnMentionedBot 依赖此语义
//   - 排除 SegmentMentionAll（@ 全体成员不是具体用户）
//   - 排除无法解析 UserID 的 at 段
func SegmentsMentions(segs []Segment, botID string) []UserInfo {
	var out []UserInfo
	seen := make(map[string]struct{}, 4)
	for _, s := range segs {
		if s.Type != SegmentAt || s.UserID == "" {
			continue
		}
		if _, ok := seen[s.UserID]; ok {
			continue
		}
		seen[s.UserID] = struct{}{}

		isSelf := botID != "" && s.UserID == botID
		if v, ok := s.Extra[SegmentExtraIsSelf]; ok {
			if b, ok := v.(bool); ok {
				isSelf = b
			}
		}
		out = append(out, UserInfo{
			ID:          s.UserID,
			DisplayName: s.Text,
			IsSelf:      isSelf,
			IsBot:       isSelf,
		})
	}
	return out
}

// SegmentsReplyToID 返回段中首个 reply 段的目标消息 ID（无 reply 段时返回空字符串）。
func SegmentsReplyToID(segs []Segment) string {
	for _, s := range segs {
		if s.Type == SegmentReply {
			return s.ReplyToID
		}
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// 出站消息（OutboundMessage）
// ────────────────────────────────────────────────────────────────────────────

// OutboundMessage 是平台无关的出站消息模型。
//
// 各平台 Sender 将此结构体转换为平台特定的发送格式。
// 未设置的字段会被忽略；平台不支持的字段会优雅降级。
//
// 字段优先级（平台支持时）：
//  1. Embeds（最丰富）
//  2. Attachments（富媒体）
//  3. Markdown（格式文本）
//  4. Text（纯文本，最广泛兼容）
type OutboundMessage struct {
	// Segments 有序消息段（主字段，新增）。
	// 非空时 Sender 按段顺序发送，忽略下方扁平字段（唯一真相源）。
	// 为空时走下方便捷字段路径（兼容旧调用方）。
	Segments []Segment

	// Text 纯文本内容
	Text string

	// Markdown Markdown 格式内容（平台不支持时降级为 Text）
	Markdown string

	// Attachments 附件列表（图片/音频/视频/文件，支持多附件）
	//
	// 不支持多附件的平台（如 QQ）只处理第一个匹配当前能力的附件。
	Attachments []Attachment

	// Embeds 富文本嵌入卡片列表（Discord 原生、其他平台降级处理）
	Embeds []Embed

	// Mentions 被 @ 用户的 ID 列表（平台内唯一标识符）
	//
	// QQ 平台：member_openid；Discord/Telegram：user_id。
	// 各平台 Sender 负责将其转换为平台特定的 @ 格式。
	Mentions []string

	// Buttons 交互按钮列表（Discord 组件、Telegram 内联键盘等）
	//
	// 不支持按钮的平台可安全忽略此字段。
	Buttons []Button

	// ReplyToID 回复的目标消息 ID（平台原生消息 ID）
	ReplyToID string

	// Extra 平台特定扩展字段（key-value 形式）
	//
	// 示例（QQ 平台传递 msg_seq）：
	//   msg.Extra = map[string]any{"msg_seq": 1}
	Extra map[string]any
}

// SegmentsToOutbound 将有序消息段转换为出站消息（段 → 便捷字段派生填充）。
//
// Segments 原样保留为主字段（Sender 段路径优先）；Text/Attachments/Mentions/ReplyToID
// 由派生函数填充，便于段路径之外的代码阅读。
func SegmentsToOutbound(segs []Segment) OutboundMessage {
	m := OutboundMessage{Segments: segs}
	m.Text = SegmentsContent(segs)
	m.Attachments = SegmentsAttachments(segs)
	for _, s := range segs {
		if s.Type == SegmentAt && s.UserID != "" {
			m.Mentions = append(m.Mentions, s.UserID)
		}
	}
	for _, s := range segs {
		if s.Type == SegmentReply && s.ReplyToID != "" {
			m.ReplyToID = s.ReplyToID
		}
	}
	return m
}

// OutboundSegments 将便捷字段逆向为有序段（尽力）。
//
// 已有 Segments 时直接返回原值；否则按 reply → at → 文本（Markdown 优先，
// 标注 Extra["markdown"]=true）→ 附件的顺序拼接。
// 注意：便捷字段无法表达交错位置（如「文本夹 at」），仅适用于旧路径消息。
func OutboundSegments(m OutboundMessage) []Segment {
	if len(m.Segments) > 0 {
		return m.Segments
	}
	var segs []Segment
	if m.ReplyToID != "" {
		segs = append(segs, Segment{Type: SegmentReply, ReplyToID: m.ReplyToID})
	}
	for _, uid := range m.Mentions {
		segs = append(segs, Segment{Type: SegmentAt, UserID: uid})
	}
	if m.Markdown != "" {
		segs = append(segs, Segment{
			Type:  SegmentText,
			Text:  m.Markdown,
			Extra: map[string]any{"markdown": true},
		})
	} else if m.Text != "" {
		segs = append(segs, Segment{Type: SegmentText, Text: m.Text})
	}
	for _, att := range m.Attachments {
		var t SegmentType
		switch att.Kind {
		case AttachmentKindImage:
			t = SegmentImage
		case AttachmentKindAudio:
			t = SegmentAudio
		case AttachmentKindVideo:
			t = SegmentVideo
		case AttachmentKindFile:
			t = SegmentFile
		default:
			continue
		}
		segs = append(segs, Segment{Type: t, Attachment: att})
	}
	return segs
}

// ────────────────────────────────────────────────────────────────────────────
// 入站消息（Message）
// ────────────────────────────────────────────────────────────────────────────

// Message 是入站消息的静态快照（平台无关）。
//
// 字段语义与 [Event] 接口一致，用于历史消息、消息检索等
// 需要"拿到一条完整消息数据"的场景（事件流使用动态 [Event] 接口）。
//
// 与 [OutboundMessage] 的区别：Message 是接收视角（含 Sender/Chat/
// Timestamp/附件元信息），OutboundMessage 是发送视角（含 Markdown/
// Buttons/二进制数据）。跨平台转发时用 [MessageFromEvent] 或
// 平台历史消息接口获得 Message，再构造 OutboundMessage。
type Message struct {
	// ID 平台消息 ID（可用于撤回/编辑）。
	ID string
	// Platform 消息来源平台 ID（= EventIdentity.Platform()，MessageFromEvent 填充；
	// 转发判定用，见 MessageToOutbound）。
	Platform string
	// Sender 发送者信息。
	Sender UserInfo
	// Chat 消息所在会话。
	Chat ChatInfo
	// Segments 有序消息段（唯一真相源）。
	Segments []Segment
	// Content 消息文本内容（纯文本，不含平台特定格式；= SegmentsContent(Segments())）。
	Content string
	// Timestamp 消息发送时间（平台不提供时为零值）。
	Timestamp time.Time
	// Attachments 消息携带的附件列表（= SegmentsAttachments(Segments())）。
	Attachments []Attachment
	// Mentions 消息中 @ 的用户列表。
	Mentions []UserInfo
	// ReplyToID 被回复消息的平台原生 ID（非回复时为空）。
	ReplyToID string
	// Extra 平台特有扩展字段（key-value 形式）。
	Extra map[string]any
}

// MessageFromEvent 将动态事件转换为消息静态快照。
//
// 从 [Event] 接口的基础方法 + [ReplyEvent]/[MentionsEvent] 可选接口
// 提取全部字段；事件未实现可选接口时对应字段为空。
//
// 使用示例（把收到的消息转发为历史消息存档）：
//
//	msg := platform.MessageFromEvent(ev)
//	store.Append(ev.Chat().ID, msg)
func MessageFromEvent(e Event) Message {
	m := Message{
		Platform:    e.Platform(),
		Sender:      e.Sender(),
		Chat:        e.Chat(),
		Segments:    e.Segments(),
		Content:     Content(e),
		Timestamp:   e.Timestamp(),
		Attachments: Attachments(e),
		Mentions:    GetMentions(e),
		ReplyToID:   GetReplyToID(e),
	}
	if id := e.ID(); id != "" {
		m.ID = id
	}
	return m
}

// ────────────────────────────────────────────────────────────────────────────
// 转发辅助（MessageToOutbound）
// ────────────────────────────────────────────────────────────────────────────

// ForwardOption 是 MessageToOutbound 的可选配置。
type ForwardOption func(*forwardConfig)

type forwardConfig struct {
	targetPlatform string
	degrade        DegradePolicy
}

// WithTargetPlatform 声明转发目标平台 ID。
//
// 目标平台与消息来源平台（Message.Platform）一致时，reply/face/forward/button/unknown
// 等平台原生段按「同平台透传」处理（原始数据可还原）；
// 缺省或跨平台时按内置处置表保守降级（reply/unknown 剥离、face 降 text、forward 摘要）。
func WithTargetPlatform(platformID string) ForwardOption {
	return func(c *forwardConfig) { c.targetPlatform = platformID }
}

// WithDegrade 覆盖默认降级策略（默认使用内置处置表）。
//
// 策略接收待处置段，返回降级产物（0 个 = 剥离，1+ 个 = 替换为该组段）。
func WithDegrade(p DegradePolicy) ForwardOption {
	return func(c *forwardConfig) { c.degrade = p }
}

// DegradePolicy 是跨平台转发时对段的处置策略。
type DegradePolicy func(s Segment, samePlatform bool) []Segment

// MessageToOutbound 将消息快照转换为出站消息（跨平台转发）。
//
// 直接按段 → 段映射，保留文本夹 at 的交错位置；不经扁平字段中转。
// 转发语义（转发语义决议）：
//   - WithTargetPlatform(m.Platform) → 同平台透传（reply/face/forward/button/unknown 还原）
//   - 缺省/跨平台 → 保守降级（reply/button/unknown 剥离，face 降 text，forward 摘要）
func MessageToOutbound(m Message, opts ...ForwardOption) OutboundMessage {
	cfg := forwardConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	same := cfg.targetPlatform != "" && cfg.targetPlatform == m.Platform

	segs := make([]Segment, 0, len(m.Segments))
	for _, s := range m.Segments {
		if cfg.degrade != nil {
			segs = append(segs, cfg.degrade(s, same)...)
			continue
		}
		segs = append(segs, degradeSegment(s, same)...)
	}
	return SegmentsToOutbound(segs)
}

// degradeSegment 内置处置表（转发处置表）。
func degradeSegment(s Segment, samePlatform bool) []Segment {
	if samePlatform {
		return []Segment{s}
	}
	switch s.Type {
	case SegmentText, SegmentAt, SegmentMentionAll,
		SegmentImage, SegmentAudio, SegmentVideo, SegmentFile:
		return []Segment{s}
	case SegmentReply, SegmentButton, SegmentUnknown:
		return nil
	case SegmentFace:
		return []Segment{{Type: SegmentText, Text: s.FaceID}}
	case SegmentForward:
		if summary := extraString(s.Extra, SegmentExtraSummary); summary != "" {
			return []Segment{{Type: SegmentText, Text: summary}}
		}
		if title := extraString(s.Extra, SegmentExtraTitle); title != "" {
			return []Segment{{Type: SegmentText, Text: title}}
		}
		return nil
	default:
		return []Segment{s}
	}
}

// extraString 安全读取 Segment.Extra 中的字符串值。
func extraString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// ────────────────────────────────────────────────────────────────────────────
// 工厂函数
// ────────────────────────────────────────────────────────────────────────────

// TextMessage 快速创建纯文本消息
func TextMessage(text string) OutboundMessage {
	return OutboundMessage{Text: text}
}

// MarkdownMessage 快速创建 Markdown 消息
func MarkdownMessage(md string) OutboundMessage {
	return OutboundMessage{Markdown: md}
}

// ImageMessage 快速创建图片消息（远程 URL）
func ImageMessage(url string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{Kind: AttachmentKindImage, URL: url}}}
}

// ImageDataMessage 快速创建图片消息（本地二进制直传）
//
// 适用于在内存中生成图片（如二维码、文字图片、验证码）后直接发送，
// 无需先上传到文件服务器。
//
// mimeType 为图片 MIME 类型（如 "image/png"、"image/jpeg"），
// name 为可选文件名（某些平台要求）。
//
// 示例：
//
//	pngBytes, _ := qrcode.Encode(content, qrcode.Medium, 256)
//	msg := platform.ImageDataMessage(pngBytes, "qrcode.png", "image/png")
func ImageDataMessage(data []byte, name, mimeType string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{
		Kind:     AttachmentKindImage,
		Data:     data,
		Name:     name,
		MimeType: mimeType,
	}}}
}

// AudioMessage 快速创建音频消息（远程 URL）
func AudioMessage(url string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{Kind: AttachmentKindAudio, URL: url}}}
}

// VideoMessage 快速创建视频消息（远程 URL）
func VideoMessage(url string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{Kind: AttachmentKindVideo, URL: url}}}
}

// FileMessage 快速创建文件消息（远程 URL）
func FileMessage(url, name string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{Kind: AttachmentKindFile, URL: url, Name: name}}}
}

// FileDataMessage 快速创建文件消息（本地二进制直传）
func FileDataMessage(data []byte, name, mimeType string) OutboundMessage {
	return OutboundMessage{Attachments: []Attachment{{
		Kind:     AttachmentKindFile,
		Data:     data,
		Name:     name,
		MimeType: mimeType,
	}}}
}

// AttachmentFromURL 用远程 URL 构造附件。
//
// URL 与 Data 互斥；需要二进制直传时请使用 [AttachmentFromData]。
//
// 使用示例：
//
//	att := platform.AttachmentFromURL(platform.AttachmentKindImage, "https://example.com/img.png")
func AttachmentFromURL(kind AttachmentKind, url string) Attachment {
	return Attachment{Kind: kind, URL: url}
}

// AttachmentFromData 用本地二进制数据构造附件。
//
// URL 与 Data 互斥；需要远程 URL 时请使用 [AttachmentFromURL]。
//
// 使用示例：
//
//	att := platform.AttachmentFromData(platform.AttachmentKindFile, pdfBytes)
//	att.Name = "report.pdf"
//	att.MimeType = "application/pdf"
func AttachmentFromData(kind AttachmentKind, data []byte) Attachment {
	return Attachment{Kind: kind, Data: data}
}

// ────────────────────────────────────────────────────────────────────────────
// 链式 Builder 方法（返回新消息，不修改原消息）
// ────────────────────────────────────────────────────────────────────────────

// WithReply 设置回复目标消息 ID
func (m OutboundMessage) WithReply(messageID string) OutboundMessage {
	m.ReplyToID = messageID
	return m
}

// WithMentions 追加 @ 用户 ID 列表
func (m OutboundMessage) WithMentions(userIDs ...string) OutboundMessage {
	m.Mentions = append(slices.Clone(m.Mentions), userIDs...)
	return m
}

// WithButtons 追加交互按钮
func (m OutboundMessage) WithButtons(buttons ...Button) OutboundMessage {
	m.Buttons = append(slices.Clone(m.Buttons), buttons...)
	return m
}

// WithAttachments 追加附件
func (m OutboundMessage) WithAttachments(attachments ...Attachment) OutboundMessage {
	m.Attachments = append(slices.Clone(m.Attachments), attachments...)
	return m
}

// WithEmbeds 追加富文本卡片
func (m OutboundMessage) WithEmbeds(embeds ...Embed) OutboundMessage {
	m.Embeds = append(slices.Clone(m.Embeds), embeds...)
	return m
}

// WithExtra 添加平台扩展字段（返回新消息，不修改原消息）
//
// 每次调用均创建独立的 Extra map，避免多个派生消息共享同一底层 map 导致的数据污染。
// 当原消息 Extra 为空时，直接创建单元素 map，跳过无用的 maps.Copy。
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
	if len(m.Extra) == 0 {
		m.Extra = map[string]any{key: value}
		return m
	}
	newExtra := make(map[string]any, len(m.Extra)+1)
	maps.Copy(newExtra, m.Extra)
	newExtra[key] = value
	m.Extra = newExtra
	return m
}

// TruncateText 截断文本至指定字符数（按 Unicode rune 计算，非字节）。
//
// 若文本长度不超过 maxRunes，直接返回原字符串（无内存分配）。
// 超出时返回截断后的文本加 "…" 后缀，**返回值总长度不超过 maxRunes**
// （省略号计入预算内）。
//
// 该函数的典型用法是把文本压到平台长度上限以内，例如
// Capabilities.MaxTextLength 或 Telegram 的 200 字符 callback 提示；
// 若省略号不计入预算，结果会恰好超出上限一个字符，请求仍被平台拒绝。
//
// 修复框架问题 #27：xhstext 发现生成文本可能超过平台消息长度限制，
// 统一提供此工具函数避免各插件自行实现截断逻辑。
//
// 使用示例：
//
//	output := platform.TruncateText(longText, 500)
func TruncateText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(text)
	if len(r) <= maxRunes {
		return text
	}
	// maxRunes == 1 时 r[:0] 为空，结果恰为 "…"，无需特判。
	return string(r[:maxRunes-1]) + "…"
}

// IsEmpty 报告消息是否没有任何可发送的内容。
//
// 当 Text、Markdown、Attachments、Embeds、Buttons、Mentions 均为空/nil 时返回 true。
// Extra 等纯元数据字段不计入"内容"判断，因为单独存在时平台无法发出有意义的消息。
//
// 典型用法（Sender 实现中防止发送空消息）：
//
//	if req.Message.IsEmpty() {
//	    return errutil.ErrEmptyMessage
//	}
func (m OutboundMessage) IsEmpty() bool {
	if len(m.Segments) > 0 {
		return false
	}
	return m.Text == "" &&
		m.Markdown == "" &&
		len(m.Attachments) == 0 &&
		len(m.Embeds) == 0 &&
		len(m.Buttons) == 0 && // 纯按钮消息（Discord 组件面板等）是合法的
		len(m.Mentions) == 0 // 纯 @ 消息在部分平台/频道场景合法
}
