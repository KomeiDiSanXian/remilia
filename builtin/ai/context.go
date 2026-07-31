// Package ai context.go — 回复上下文与群聊消息窗口。
//
// 本文件实现两个依赖 messagelog 消息历史的结构级上下文能力：
//   - 回复上下文：用户"回复某条消息再 @ 机器人"时，把被回复消息的内容前置到用户消息
//   - 群聊消息窗口：把同群最近的若干条入站消息注入系统提示，让 AI 感知多人对话
//
// 同时提供 replyAndRecord：在保持异步发送的前提下，记录机器人自己的对话回复
// （通过 ctx.Reply 返回的 Future 在发送完成后取得平台 MessageID 写入 messagelog），
// 从而使"回复机器人上一条消息"也能被回复上下文命中。
package ai

import (
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// replyAndRecord 发送消息并异步记录出站消息（供回复上下文查询）。
//
// 发送仍走 ctx.Reply 的异步调度器（提交即返回，不阻塞 handler）；
// 记录通过后台 goroutine 等待 Future 完成、取得平台 MessageID 后写入 messagelog。
// 仅在 include_reply_context 开启且 history 可用时记录；平台不返回 MessageID 时静默跳过。
func (p *Plugin) replyAndRecord(ctx *eventctx.Context, msg platform.OutboundMessage) *future.Future[platform.SendResult] {
	f := ctx.Reply(msg)

	if p.cfg.IncludeReplyContext && p.history != nil {
		chatID := ctx.GetChatInfo().ID
		text := msg.Text
		if text == "" {
			text = msg.Markdown
		}
		if text != "" && chatID != "" {
			go func() {
				select {
				case <-f.Done():
					res, _ := f.Result()
					if res.MessageID != "" {
						p.history.RecordOutbound(chatID, res.MessageID, text, time.Now())
					}
				case <-p.lifecycleCtx.Done():
				}
			}()
		}
	}

	return f
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
