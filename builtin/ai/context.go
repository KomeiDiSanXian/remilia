// Package ai context.go — 回复上下文与群聊消息窗口。
//
// 本文件实现两个依赖 messagelog 消息历史的结构级上下文能力：
//   - 回复上下文：用户"回复某条消息再 @ 机器人"时，把被回复消息的内容前置到用户消息
//   - 群聊消息窗口：把同群最近的若干条入站消息注入系统提示，让 AI 感知多人对话
//
// 同时提供 replyAndRecord 薄封装：确保出站消息被 messagelog 的出站观察者记录，
// 从而使"回复机器人上一条消息"也能被回复上下文命中。
package ai

import (
	"fmt"
	"strings"

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
// 该配置只控制回复上下文的注入（见 prependReplyContext）。
func (p *Plugin) replyAndRecord(ctx *eventctx.Context, msg platform.OutboundMessage) *future.Future[platform.SendResult] {
	if _, ok := eventctx.ExtGet[eventctx.OutboundObserverExt](ctx.Ext()); !ok && p.history != nil {
		eventctx.ExtSet(ctx.Ext(), eventctx.OutboundObserverExt{Observer: p.history})
	}
	return ctx.Reply(msg)
}

// prependReplyContext 若本条消息是回复，将所回复消息的内容前置到用户消息。
// 命中出站消息（机器人自己的回复）时以"机器人"标注发送者。
// 未命中或关闭时不改变 content。
func (p *Plugin) prependReplyContext(ctx *eventctx.Context, content string) string {
	if p.history == nil {
		return content
	}
	replyID := platform.GetReplyToID(ctx.GetPlatformEvent())
	if replyID == "" {
		return content
	}

	entry, ok := p.history.QueryByEventID(ctx.GetChatInfo().ID, replyID)
	if !ok || entry.Content == "" {
		return content
	}

	name := entry.UserName
	if entry.IsOutbound {
		name = "机器人"
	}
	if name == "" {
		name = "对方"
	}

	msg := strings.TrimSpace(stripMentionMarkup(entry.Content))
	msg = truncateRunes(msg, 200)
	if msg == "" {
		return content
	}

	prefix := fmt.Sprintf("[你正在回复 %s 的消息]\n%s", name, msg)
	if content == "" {
		return prefix
	}
	return prefix + "\n\n" + content
}

// buildGroupContext 组装同群最近 N 条入站消息（昵称: 内容，旧到新）。
// 跳过空内容与合成事件（AI 工具调用的内部命令），剥 @ 标记并单行截断。
func (p *Plugin) buildGroupContext(ctx *eventctx.Context) string {
	if p.history == nil || p.cfg.ContextGroupMessages <= 0 {
		return ""
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		return ""
	}

	entries := p.history.QueryGroup(chat.ID, p.cfg.ContextGroupMessages)
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	for _, e := range entries {
		if e.Content == "" || e.Platform == "synthetic" {
			continue
		}
		name := e.UserName
		if name == "" {
			name = e.UserID
		}
		if name == "" {
			name = "未知"
		}
		text := strings.TrimSpace(stripMentionMarkup(e.Content))
		text = truncateRunes(text, 120)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", name, text)
	}

	return strings.TrimRight(b.String(), "\n")
}
