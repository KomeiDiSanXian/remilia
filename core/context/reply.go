package context

// reply.go — 常用回复的便利方法

import "github.com/KomeiDiSanXian/remilia/platform"

// ReplyText 发送纯文本回复。
//
// 等价于 ctx.Reply(platform.TextMessage(text))，但更简洁，
// 且调用方无需 import "platform"。
//
// 使用示例：
//
//	return ctx.ReplyText("Hello!")
func (ctx *Context) ReplyText(text string) error {
	_, err := ctx.Reply(platform.TextMessage(text))
	return err
}

// ReplyError 发送错误提示回复（带 ❌ 前缀）。
//
// 使用示例：
//
//	return ctx.ReplyError("权限不足")
func (ctx *Context) ReplyError(text string) error {
	_, err := ctx.Reply(platform.TextMessage("❌ " + text))
	return err
}

// ReplySuccess 发送成功提示回复（带 ✅ 前缀）。
//
// 使用示例：
//
//	return ctx.ReplySuccess("操作完成")
func (ctx *Context) ReplySuccess(text string) error {
	_, err := ctx.Reply(platform.TextMessage("✅ " + text))
	return err
}

// GetSenderID 返回消息发送者的平台 ID。
//
// 等价于 ctx.GetSenderInfo().ID，但更简短。
func (ctx *Context) GetSenderID() string {
	return ctx.GetSenderInfo().ID
}

// GetDisplayName 返回消息发送者的显示名称。
//
// 等价于 ctx.GetSenderInfo().DisplayName。
func (ctx *Context) GetDisplayName() string {
	return ctx.GetSenderInfo().DisplayName
}
