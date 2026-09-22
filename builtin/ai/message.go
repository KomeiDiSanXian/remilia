// Package ai message.go — 出站回复面。
//
// 本文件只保留与插件上下文耦合的出站回复：
//   - replyAndRecord：确保出站消息被 messagelog 的出站观察者记录，
//     使"回复机器人上一条消息"也能被回复上下文命中
//   - replyFormatted / formatReplyMessage：按 markdown 配置构造回复
//
// 入站消息归一化（合并转发识别/触发/图片提取、附件判定、命令样式识别）已归位
// 到 builtin/ai/runtime；回复上下文与群聊消息窗口的构建在 builtin/ai/promptctx。
package ai

import (
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// replyAndRecord 发送消息并确保出站记录。
//
// 发送仍走 ctx.Reply 的异步调度器（提交即返回，不阻塞 handler）。
// 出站消息的记录由 messagelog 的 OutboundObserver 在发送完成后同步完成
// （见 messagelog.MessageLogger 中间件 / Logger.OnOutbound）。
//
// 此薄封装仅在上下文缺少观察者时（如 doSummary 用 NewContextFromEvent
// 新建的上下文）用 p.history 补上，保证 AI 的对话回复总能被记录。
// 记录不受 include_reply_context 控制——记录是 messagelog 级行为，
// 该配置只控制回复上下文的注入（见 promptctx.PrependReplyContext）。
func (s *contextState) replyAndRecord(ctx *eventctx.Context, msg platform.OutboundMessage) *future.Future[platform.SendResult] {
	if _, ok := ctx.Ext().GetTyped[eventctx.OutboundObserverExt](); !ok && s.history != nil {
		ctx.Ext().SetTyped(eventctx.OutboundObserverExt{Observer: s.history})
	}
	return ctx.Reply(msg)
}

// replyFormatted 按 markdown 配置发送带格式的子命令回复。
//
// markdown=true 时使用 MarkdownMessage（平台不支持时由发送层自动降级为纯文本）；
// markdown=false 时始终发送纯文本。与主回复路径（handler/runner/sendtool）保持一致，
// 避免子命令文案（粗体/行内代码等）在支持 Markdown 的平台上一律以字面符号展示。
func (p *Plugin) replyFormatted(ctx *eventctx.Context, text string) *future.Future[platform.SendResult] {
	if p.cfg != nil && p.cfg.Markdown {
		return ctx.Reply(platform.MarkdownMessage(text))
	}
	return ctx.ReplyText(text)
}

// formatReplyMessage 按 markdown 配置构造带格式的回复消息（markdown=true 时
// 用 MarkdownMessage，否则纯文本），语义与 replyFormatted 一致；供需要先
// 构造消息再追加平台扩展（如 QQ 指令按钮，见 qqaction.go）的调用方使用。
func (p *Plugin) formatReplyMessage(text string) platform.OutboundMessage {
	if p.cfg != nil && p.cfg.Markdown {
		return platform.MarkdownMessage(text)
	}
	return platform.TextMessage(text)
}
