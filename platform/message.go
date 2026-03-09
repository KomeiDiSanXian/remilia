package platform

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

// WithReply 为消息设置回复目标，返回新消息（不修改原消息）
func (m OutboundMessage) WithReply(messageID string) OutboundMessage {
	m.ReplyToID = messageID
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
