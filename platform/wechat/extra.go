package wechat

import "github.com/KomeiDiSanXian/remilia/platform"

// extra.go — 平台专属令牌键、附件/按钮扩展键，以及发送选项。
//
// 框架与插件统一通过这里的常量读写平台特有数据，避免各处散落字符串字面量。

// Token keys stored in platform.ChatInfo.Tokens for sender routing.
const (
	// TokenReplyReqID 是长连接模式下回复所需的 req_id（回调帧头里的 req_id）。
	//
	// 企业微信要求同一次回调的所有回复（含流式刷新）都复用这个 req_id，
	// 由适配器在解析事件时写入。
	TokenReplyReqID = "reply_req_id"

	// TokenResponseURL 是 URL 回调模式下的一次性回复地址。
	//
	// 只能使用一次且 1 小时后过期；没有它就只能等用户再次发消息。
	TokenResponseURL = "response_url"

	// TokenMsgID 是回调的 msgid，用于排重与追踪（企业微信会因网络原因重投）。
	TokenMsgID = "msgid"
)

// Extra keys on platform.Attachment.Extra.
const (
	// ExtraKeyAesKey 是媒体直链的解密密钥（每条链接唯一）。
	//
	// 企业微信的图片/文件/视频直链内容是 AES-256-CBC 加密的，
	// 拿到 URL 但没有这个 key 只能得到密文。用 [DecryptMedia] 解密。
	ExtraKeyAesKey = "aeskey"

	// ExtraKeyFileName 是文件消息的原始文件名。
	ExtraKeyFileName = "filename"
)

// Extra key on platform.Button.Extra，值为 *ButtonExtra。
const extraKeyButton = "wechat_button"

// ButtonExtra 是按钮的企业微信扩展。
type ButtonExtra struct {
	// Style 按钮样式，取值 1~4（1 主色 / 2 次色 / 3 警示 / 4 成功），
	// 0 使用模板默认。模板卡片的样式语义与 ButtonStyle 不完全对应。
	Style int
}

// MessageExtra 是企业微信的消息发送选项。
//
// 用 ApplyExtra 注入到 OutboundMessage：
//
//	msg := wechat.ApplyExtra(platform.TextMessage("hi"), wechat.MessageExtra{
//	    Title: "构建通知",
//	})
type MessageExtra struct {
	// Title 卡片主标题（仅当消息带 Buttons、被转成模板卡片时使用）。
	Title string
	// Description 卡片主标题下的说明文字。
	Description string
	// TaskID 模板卡片任务 ID，留空自动生成。
	//
	// 企业微信要求同一次卡片交互更新时沿用回调里的 task_id，插件自行回填
	// 才能在不改变卡片结构的前提下原地更新。
	TaskID string
}

// ApplyExtra 把企业微信发送选项注入 OutboundMessage（返回新消息）。
func ApplyExtra(msg platform.OutboundMessage, extra MessageExtra) platform.OutboundMessage {
	return msg.WithExtra(extraKeyMessage, extra)
}

// extraKeyMessage 是 MessageExtra 在 OutboundMessage.Extra 中的键。
const extraKeyMessage = "__wechat_message_extra__"

// extractExtra 取出 MessageExtra，未注入时返回零值。
func extractExtra(msg platform.OutboundMessage) MessageExtra {
	if msg.Extra == nil {
		return MessageExtra{}
	}
	if v, ok := msg.Extra[extraKeyMessage]; ok {
		if e, ok := v.(MessageExtra); ok {
			return e
		}
	}
	return MessageExtra{}
}

// extractButtonExtra 取出按钮扩展，未设置时返回零值。
func extractButtonExtra(btn platform.Button) ButtonExtra {
	if btn.Extra == nil {
		return ButtonExtra{}
	}
	if v, ok := btn.Extra[extraKeyButton]; ok {
		if e, ok := v.(ButtonExtra); ok {
			return e
		}
	}
	return ButtonExtra{}
}
