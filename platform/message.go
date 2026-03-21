package platform

import (
	"maps"
	"slices"
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

// Attachment 单个附件。
//
// URL 与 Data 互斥：
//   - URL 非空 → 使用远程 URL 发送（平台支持时直接引用）
//   - Data 非空 → 使用二进制直传（Telegram sendDocument、Discord 附件等）
//
// 两者均为空时，Sender 应将其忽略。
type Attachment struct {
	// Kind 附件媒体类型
	Kind AttachmentKind
	// URL 远程附件 URL（https://...）
	URL string
	// Data 本地二进制数据（直传，URL 为空时使用）
	Data []byte
	// MimeType MIME 类型，如 "image/png"（可选，辅助平台正确处理）
	MimeType string
	// Name 文件名（与 Data 或 FileURL 配合；平台不支持时可忽略）
	Name string
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
	// Color 边框/主题颜色，十六进制整数，如 0x5865F2（Discord 蓝）
	Color int
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
	// Style 按钮样式
	Style ButtonStyle
	// Disabled 按钮是否置灰不可点击（Discord/QQ 均支持）
	Disabled bool
	// Row 按钮所在行（0 起始）。
	// Discord 最多 5 行，每行 5 个。0 表示由平台自动排列。
	Row int
	// Emoji 按钮前展示的 emoji（Discord 原生支持，其他平台忽略）
	Emoji string
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
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
	newExtra := make(map[string]any, len(m.Extra)+1)
	maps.Copy(newExtra, m.Extra)
	newExtra[key] = value
	m.Extra = newExtra
	return m
}
