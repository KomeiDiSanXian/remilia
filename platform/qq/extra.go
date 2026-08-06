package qq

import (
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

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
// 附件/按钮扩展键
// ────────────────────────────────────────────────────────────────────────────

// Attachment/Button 的 Extra map 中使用的键名（包级常量）。
const (
	// ExtraKeyVoice QQ 语音附件元数据键，值为 *VoiceAttachmentMeta。
	ExtraKeyVoice = "voice"
	// ExtraKeyButton QQ 按钮扩展元数据键，值为 *ButtonExtra。
	ExtraKeyButton = "button"
	// ExtraKeyInline Telegram 按钮扩展元数据键，值为 *telegram.InlineButtonExtra。
	ExtraKeyInline = "inline"
	// ExtraKeyArkData 结构化卡片（message_type=3）的 ark_data 原始 JSON 键。
	// 挂在 Segment.Extra 上（SegmentUnknown 段），值为 string。
	ExtraKeyArkData = "ark_data"
)

// ────────────────────────────────────────────────────────────────────────────
// VoiceAttachmentMeta
// ────────────────────────────────────────────────────────────────────────────

// VoiceAttachmentMeta 携带 QQ 平台语音附件的专属元数据。
//
// 存储于 platform.Attachment.Extra[qq.ExtraKeyVoice]。
// 使用方式：
//
//	for _, att := range platform.Attachments(event) {
//	    if meta, ok := att.Extra[qq.ExtraKeyVoice].(*qq.VoiceAttachmentMeta); ok {
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

// ButtonExtra 是 QQ 平台专属的按钮扩展字段。
// 存储于 platform.Button.Extra[qq.ExtraKeyButton]，用于控制 QQ 按钮的独有特性。
//
// 示例：
//
//	btn := platform.Button{
//	    ID:    "cmd_ping",
//	    Label: "Ping",
//	    Extra: map[string]any{
//	        qq.ExtraKeyButton: &qq.ButtonExtra{
//	            Enter:  true,           // 指令按钮：点击后自动发送
//	            Reply:  true,           // 指令按钮：带引用回复
//	            Anchor: 0,              // 1=唤起选图器
//	        },
//	    },
//	}
type ButtonExtra struct {
	// Enter 指令按钮可用，点击按钮后直接自动发送 data，仅单聊可用，默认 false。
	// 仅 type=2（指令按钮）有效。支持版本 8983。
	Enter bool
	// Reply 指令按钮可用，指令是否带引用回复本消息，默认 false。
	// 仅 type=2（指令按钮）有效。支持版本 8983。
	Reply bool
	// Anchor 本字段仅在指令按钮下有效，设置为 1 时点击按钮自动唤起手Q选图器。
	// 仅支持手机端版本 8983+ 的单聊场景，桌面端不支持。
	Anchor int
}

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
	// Ark ARK 模板消息（QQ 平台专属卡片消息）。
	// 设置后优先于 Text/Markdown 等普通消息类型。
	Ark *Ark
	// Card 卡片消息（msg_type=8），适用于群聊图文卡片。
	// 优先级高于 Ark。
	Card *dto.Card
	// InputNotify 输入中状态（msg_type=6），仅 C2C 单聊。
	InputNotify *dto.InputNotify
	// MarkdownTemplateID 自定义 Markdown 模板 ID（已废弃但仍可用）。
	MarkdownTemplateID string
	// MarkdownParams Markdown 模板参数，与 MarkdownTemplateID 配合使用。
	MarkdownParams []dto.MarkdownParam
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
