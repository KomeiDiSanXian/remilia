package context

// reply.go — 常用回复的便利方法

import (
	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ReplyText 发送纯文本回复。
//
// 等价于 ctx.Reply(platform.TextMessage(text))，但更简洁，
// 且调用方无需 import "platform"。
//
// 使用示例：
//
//	ctx.ReplyText("Hello!"); return nil  // 忽略返回值，异步发送
func (ctx *Context) ReplyText(text string) *future.Future[platform.SendResult] {
	return ctx.Reply(platform.TextMessage(text))
}

// ReplyError 发送错误提示回复（带 ❌ 前缀）。
//
// 使用示例：
//
//	ctx.ReplyError("权限不足"); return nil
func (ctx *Context) ReplyError(text string) *future.Future[platform.SendResult] {
	return ctx.Reply(platform.TextMessage("❌ " + text))
}

// ReplySuccess 发送成功提示回复（带 ✅ 前缀）。
//
// 使用示例：
//
//	ctx.ReplySuccess("操作完成"); return nil
func (ctx *Context) ReplySuccess(text string) *future.Future[platform.SendResult] {
	return ctx.Reply(platform.TextMessage("✅ " + text))
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
