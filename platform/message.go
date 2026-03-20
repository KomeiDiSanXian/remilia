package platform

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
// 适用于 Discord 消息组件、Telegram 内联键盘等。
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
}

// OutboundMessage 是平台无关的出站消息模型。
//
// 各平台 Sender 将此结构体转换为平台特定的发送格式。
// 未设置的字段会被忽略；平台不支持的字段（如 Markdown）会优雅降级为文本。
type OutboundMessage struct {
	// Text 纯文本内容
	Text string

	// Markdown Markdown 格式内容（平台不支持时降级为 Text）
	Markdown string

	// ImageURL 图片 URL（富媒体消息）
	ImageURL string

	// AudioURL 音频/语音消息 URL
	AudioURL string

	// VideoURL 视频消息 URL
	VideoURL string

	// FileURL 文件消息 URL
	FileURL string

	// FileName 文件名（与 FileURL 配合使用；平台不支持时可忽略）
	FileName string

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

// TextMessage 快速创建纯文本消息的便捷构造函数
func TextMessage(text string) OutboundMessage {
	return OutboundMessage{Text: text}
}

// MarkdownMessage 快速创建 Markdown 消息的便捷构造函数
func MarkdownMessage(md string) OutboundMessage {
	return OutboundMessage{Markdown: md}
}

// ImageMessage 快速创建图片消息的便捷构造函数
func ImageMessage(url string) OutboundMessage {
	return OutboundMessage{ImageURL: url}
}

// AudioMessage 快速创建音频/语音消息的便捷构造函数
func AudioMessage(url string) OutboundMessage {
	return OutboundMessage{AudioURL: url}
}

// VideoMessage 快速创建视频消息的便捷构造函数
func VideoMessage(url string) OutboundMessage {
	return OutboundMessage{VideoURL: url}
}

// FileMessage 快速创建文件消息的便捷构造函数
func FileMessage(url, name string) OutboundMessage {
	return OutboundMessage{FileURL: url, FileName: name}
}

// WithReply 为消息设置回复目标，返回新消息（不修改原消息）
func (m OutboundMessage) WithReply(messageID string) OutboundMessage {
	m.ReplyToID = messageID
	return m
}

// WithMentions 为消息追加 @ 用户，返回新消息（不修改原消息）
func (m OutboundMessage) WithMentions(userIDs ...string) OutboundMessage {
	n := make([]string, len(m.Mentions), len(m.Mentions)+len(userIDs))
	copy(n, m.Mentions)
	m.Mentions = append(n, userIDs...)
	return m
}

// WithButtons 为消息追加交互按钮，返回新消息（不修改原消息）
func (m OutboundMessage) WithButtons(buttons ...Button) OutboundMessage {
	n := make([]Button, len(m.Buttons), len(m.Buttons)+len(buttons))
	copy(n, m.Buttons)
	m.Buttons = append(n, buttons...)
	return m
}

// WithExtra 为消息添加扩展字段，返回新消息（不修改原消息）
func (m OutboundMessage) WithExtra(key string, value any) OutboundMessage {
	if m.Extra == nil {
		m.Extra = make(map[string]any)
	}
	m.Extra[key] = value
	return m
}
