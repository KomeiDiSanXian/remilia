package dto

import "strings"

// MessageBuilder 链式消息构建器
//
// 使用示例:
//
//	msg := dto.NewBuilder().Text("Hello ").At(openID).Text("!").Build()
//	msg := dto.NewBuilder().Markdown("**Bold**").Build()
//	msg := dto.NewBuilder().ReplyTo(msgID).Text("已收到").Build()
type MessageBuilder struct {
	content   strings.Builder
	msgType   MessageType
	markdown  *Markdown
	ark       *Ark
	media     *MediaResponse
	replyToID EventID
	eventID   EventID
	messageID EventID
	msgSeq    uint64
}

// NewBuilder 创建消息构建器
func NewBuilder() *MessageBuilder {
	return &MessageBuilder{msgType: TextMessage}
}

// Text 追加纯文本内容
func (b *MessageBuilder) Text(s string) *MessageBuilder {
	b.content.WriteString(s)
	b.msgType = TextMessage
	return b
}

// At 追加 @用户 标记（内联 XML 标签）
func (b *MessageBuilder) At(openID string) *MessageBuilder {
	b.content.WriteString(At(openID))
	return b
}

// AtAll 追加 @全体成员 标记
func (b *MessageBuilder) AtAll() *MessageBuilder {
	b.content.WriteString(AtAll())
	return b
}

// Markdown 设置 Markdown 内容（会覆盖之前的 Text 内容）
func (b *MessageBuilder) Markdown(content string) *MessageBuilder {
	b.markdown = &Markdown{Content: content}
	b.msgType = MarkdownMessage
	return b
}

// MarkdownTemplate 使用模板 ID 设置 Markdown
func (b *MessageBuilder) MarkdownTemplate(templateID string, params []MarkdownParam) *MessageBuilder {
	b.markdown = &Markdown{
		CustomTemplateID: templateID,
		Params:           params,
	}
	b.msgType = MarkdownMessage
	return b
}

// Ark 设置 Ark 卡片
func (b *MessageBuilder) Ark(templateID int, kv []map[string]any) *MessageBuilder {
	b.ark = &Ark{TemplateID: templateID, KV: kv}
	b.msgType = ArkMessage
	return b
}

// Media 设置富媒体内容
func (b *MessageBuilder) Media(fileUUID, fileInfo string) *MessageBuilder {
	b.media = &MediaResponse{FileUUID: fileUUID, FileInfo: fileInfo}
	b.msgType = MediaMessage
	return b
}

// ReplyTo 设置引用回复的消息 ID
func (b *MessageBuilder) ReplyTo(msgID EventID) *MessageBuilder {
	b.replyToID = msgID
	b.messageID = msgID // msg_id 字段用于引用回复
	return b
}

// WithEventID 设置触发事件 ID（被动回复时必须）
func (b *MessageBuilder) WithEventID(eventID EventID) *MessageBuilder {
	b.eventID = eventID
	return b
}

// WithSeq 设置消息序列号（防重放）
func (b *MessageBuilder) WithSeq(seq uint64) *MessageBuilder {
	b.msgSeq = seq
	return b
}

// Build 构建 *Message，返回可直接发送的消息
func (b *MessageBuilder) Build() *Message {
	msg := &Message{
		Type:       b.msgType,
		EventID:    b.eventID,
		MessageID:  b.messageID,
		MessageSeq: b.msgSeq,
	}
	switch b.msgType {
	case TextMessage:
		msg.Content = b.content.String()
	case MarkdownMessage:
		msg.Markdown = b.markdown
	case ArkMessage:
		msg.Ark = b.ark
	case MediaMessage:
		msg.Media = b.media
	}
	return msg
}

// TextMsg 快捷函数：创建纯文本消息
func TextMsg(text string) *Message {
	return NewBuilder().Text(text).Build()
}

// MarkdownMsg 快捷函数：创建 Markdown 消息
func MarkdownMsg(content string) *Message {
	return NewBuilder().Markdown(content).Build()
}

// ArkCard 构建 Ark 卡片消息的辅助构建器
type ArkCard struct {
	templateID int
	kv         []map[string]any
}

// NewArkCard 创建 Ark 卡片构建器
func NewArkCard(templateID int) *ArkCard {
	return &ArkCard{templateID: templateID, kv: []map[string]any{}}
}

// KV 追加键值对
func (a *ArkCard) KV(key string, value any) *ArkCard {
	a.kv = append(a.kv, map[string]any{"key": key, "value": value})
	return a
}

// Build 构建最终的 *Message
func (a *ArkCard) Build() *Message {
	return NewBuilder().Ark(a.templateID, a.kv).Build()
}
