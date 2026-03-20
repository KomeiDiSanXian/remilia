package qq

import "github.com/KomeiDiSanXian/remilia/platform"

// MessageExtra 是 QQ 平台专属的消息扩展参数。
//
// 使用 ApplyExtra 将其注入到 platform.OutboundMessage，
// QQ Sender 在发送时会类型安全地提取这些字段。
//
// 示例：
//
//	msg := platform.TextMessage("pong").WithReply(event.ID())
//	msg = qq.ApplyExtra(msg, qq.MessageExtra{MsgSeq: 1, EventID: rawEventID})
//	ctx.Reply(msg)
type MessageExtra struct {
	// MsgSeq 消息序列号，用于防重放（QQ v2 API 要求）
	MsgSeq uint64
	// EventID 触发事件 ID，用于被动回复时关联来源事件
	EventID string
}

// qqExtraKey 是注入到 OutboundMessage.Extra 的键（包级私有常量）。
// 使用带前缀的字符串保证不与用户自定义 key 冲突。
const qqExtraKey = "__qq_message_extra__"

// ApplyExtra 将 QQ 专属扩展参数注入到 OutboundMessage，返回新消息（不修改原消息）。
func ApplyExtra(msg platform.OutboundMessage, extra MessageExtra) platform.OutboundMessage {
	return msg.WithExtra(qqExtraKey, extra)
}

// extractExtra 从 OutboundMessage 中提取 QQ 专属参数（内部使用）。
// 若未注入或类型不匹配，返回零值 MessageExtra。
func extractExtra(msg platform.OutboundMessage) MessageExtra {
	if msg.Extra == nil {
		return MessageExtra{}
	}
	v, ok := msg.Extra[qqExtraKey]
	if !ok {
		return MessageExtra{}
	}
	e, _ := v.(MessageExtra)
	return e
}
