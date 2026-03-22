package qq

import "github.com/KomeiDiSanXian/remilia/platform"

// ────────────────────────────────────────────────────────────────────────────
// Passive Reply Token Keys
// ────────────────────────────────────────────────────────────────────────────

// QQ 被动回复授权 token 的键名，存储于 platform.ChatInfo.Tokens。
//
// 示例：
//
//	chat.Tokens[qq.TokenMsgID]   // msg_id 被动回复授权（C2C / 群消息）
//	chat.Tokens[qq.TokenEventID] // event_id 被动回复授权（INTERACTION_CREATE 等）
const (
	// TokenMsgID 消息 ID 被动回复 token（QQ v2 API msg_id 字段）。
	// 来源：C2C_MESSAGE_CREATE、GROUP_AT_MESSAGE_CREATE、频道消息事件的 payload.ID。
	TokenMsgID = "msg_id"

	// TokenEventID 事件 ID 被动回复 token（QQ v2 API event_id 字段）。
	// 来源：INTERACTION_CREATE、GROUP_ADD_ROBOT、GROUP_MSG_RECEIVE、
	//        FRIEND_ADD、C2C_MSG_RECEIVE 等事件的 payload.ID。
	TokenEventID = "event_id"
)

// ────────────────────────────────────────────────────────────────────────────
// VoiceAttachmentMeta
// ────────────────────────────────────────────────────────────────────────────

// VoiceAttachmentMeta 携带 QQ 平台语音附件的专属元数据。
//
// 当 platform.InboundAttachment.Extra 为 *VoiceAttachmentMeta 时，
// 表示该附件是 QQ 语音消息。使用方式：
//
//	for _, att := range event.Attachments() {
//	    if meta, ok := att.Extra.(*qq.VoiceAttachmentMeta); ok {
//	        // 访问语音 WAV 链接和 ASR 识别文本
//	        wavURL := meta.WavURL
//	        text   := meta.AsrText
//	    }
//	}
type VoiceAttachmentMeta struct {
	// WavURL 语音文件的 WAV 格式播放链接（有时效，勿长期持有）
	WavURL string
	// AsrText 语音内容的 ASR（自动语音识别）参考文本
	AsrText string
}

// ────────────────────────────────────────────────────────────────────────────
// MessageExtra
// ────────────────────────────────────────────────────────────────────────────

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
	// IsWakeup 互动召回消息（2026/01/10 新增）。
	// 与 msg_id/event_id 互斥，用于在用户主动对话后的召回窗口内下发一条消息。
	// 使用时不要同时设置 EventID，否则行为未定义。
	IsWakeup bool
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
