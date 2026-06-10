// Package ai subcommand.go — AI 插件子命令处理。
//
// 本文件处理所有子命令的逻辑实现，包括：
//   - /ai reset: 清空对话历史
//   - /ai undo: 撤销上一条对话
//   - /ai retry: 重新生成上一条回复
//   - /ai summary: 后台生成对话总结
//   - /ai status: 查看对话状态
//   - /ai stats: 查看使用统计
//   - /ai tools: 列出可用工具
//
// handleSubCommand 作为 @bot/私聊路径的子命令入口，
// 将自然语言命令映射到 execSubCommand 的具体实现。
package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// execSubCommand 根据子命令名称执行对应的操作。
// 用于 /ai 命令路径（通过 GetParsedCommand 获取子命令名）。
func (p *Plugin) execSubCommand(ctx *eventctx.Context, subCmd string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)

	switch subCmd {
	case "reset":
		p.sm.Delete(sessionID)
		return ctx.ReplyText("✅ 对话历史已清空，开始全新的对话吧！")

	case "undo":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("没有可以撤销的对话")
		}
		lastUserIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleUser {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx <= 0 {
			return ctx.ReplyText("没有可以撤销的对话")
		}
		session.Lock()
		session.Messages = session.Messages[:lastUserIdx]
		p.sm.saveNoLock(session)
		session.Unlock()
		return ctx.ReplyText("↩️ 已撤销上一条对话")

	case "retry":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("没有可以重试的对话")
		}
		lastAssistantIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleAssistant {
				lastAssistantIdx = i
				break
			}
		}
		if lastAssistantIdx < 0 {
			return ctx.ReplyText("没有可以重试的对话")
		}
		session.Lock()
		session.Messages = session.Messages[:lastAssistantIdx]
		p.sm.saveNoLock(session)
		session.Unlock()

		result, err := p.processWithTools(ctx, session)
		if err != nil {
			return ctx.ReplyText(formatAIError(err))
		}
		if result.Text != "" || len(result.Attachments) > 0 {
			msg := platform.OutboundMessage{}
			if p.cfg.Markdown {
				msg.Markdown = result.Text
			} else {
				msg.Text = result.Text
			}
			if len(result.Attachments) > 0 {
				msg.Attachments = result.Attachments
			}
			_, err = ctx.Reply(msg)
			return err
		}
		return nil

	case "summary":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("还没有任何对话内容可以总结")
		}
		msgsSnapshot := make([]Message, len(session.Messages))
		copy(msgsSnapshot, session.Messages)
		go p.doSummary(ctx, msgsSnapshot)
		return ctx.ReplyText("⏳ 正在生成对话总结，请稍候...")

	case "status":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("当前没有活跃的对话")
		}
		msgCount := len(session.Messages)
		sysCount := 0
		for _, m := range session.Messages {
			if m.Role == RoleSystem {
				sysCount++
			}
		}
		duration := time.Since(session.CreatedAt)
		var b strings.Builder
		b.WriteString("📊 **对话状态**\n\n")
		b.WriteString(fmt.Sprintf("  - 提供商：`%s`\n", p.cfg.Provider))
		b.WriteString(fmt.Sprintf("  - 模型：`%s`\n", p.cfg.Model))
		b.WriteString(fmt.Sprintf("  - 消息数：`%d`（含 %d 条系统提示）\n", msgCount, sysCount))
		b.WriteString(fmt.Sprintf("  - 对话时长：`%s`\n", formatDuration(duration)))
		b.WriteString(fmt.Sprintf("  - 会话 ID：`%s`\n", sessionID))
		return ctx.ReplyText(b.String())

	case "stats":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("当前没有活跃的对话")
		}
		var b strings.Builder
		b.WriteString("📈 **使用统计**\n\n")
		b.WriteString(fmt.Sprintf("  - LLM 调用次数：`%d`\n", session.CallCount))
		b.WriteString(fmt.Sprintf("  - 工具调用次数：`%d`\n", session.ToolCount))
		return ctx.ReplyText(b.String())

	case "tools", "help":
		var b strings.Builder
		b.WriteString("我可以使用以下工具：\n\n")
		tools := p.reg.List()
		if len(tools) == 0 {
			b.WriteString("（当前没有可用工具）")
		} else {
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - **%s**：%s\n", t.Name, t.Description))
			}
		}
		b.WriteString(fmt.Sprintf("\n在对话中直接告诉我你想使用哪个工具即可。\n"))
		b.WriteString(fmt.Sprintf("\n**子命令：**"))
		b.WriteString(fmt.Sprintf("\n  `%s reset` — 清空对话历史", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s undo` — 撤销上一条对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s retry` — 重新生成上一条回复", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s summary` — 总结当前对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s status` — 查看会话状态", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s stats` — 查看使用统计", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s tools` — 列出可用工具", p.cfg.TriggerCmd))
		return ctx.ReplyText(b.String())
	}
	return nil
}

// handleSubCommand 处理 @bot/私聊路径的子命令，通过内容字符串匹配。
// 返回 true 表示已处理。
func (p *Plugin) handleSubCommand(ctx *eventctx.Context, content string) bool {
	cmd := strings.ToLower(strings.TrimSpace(content))
	var err error
	switch cmd {
	case "reset", "重置":
		err = p.execSubCommand(ctx, "reset")
	case "undo":
		err = p.execSubCommand(ctx, "undo")
	case "retry", "重试":
		err = p.execSubCommand(ctx, "retry")
	case "summary", "总结":
		err = p.execSubCommand(ctx, "summary")
	case "status":
		err = p.execSubCommand(ctx, "status")
	case "stats":
		err = p.execSubCommand(ctx, "stats")
	case "tools", "工具", "help", "帮助":
		err = p.execSubCommand(ctx, "tools")
	default:
		return false
	}
	if err != nil {
		logger.Errorf("exec subcommand %q: %v", cmd, err)
	}
	return true
}

// formatDuration 将 time.Duration 格式化为人类可读的字符串。
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// doSummary 在后台调用 LLM 生成对话总结，通过原始 sender 发送结果。
// msgs 是调用方已复制的消息快照，不会与 session 管理器产生 data race。
func (p *Plugin) doSummary(origCtx *eventctx.Context, msgs []Message) {
	filtered := make([]Message, 0, len(msgs)+1)
	for _, m := range msgs {
		if m.Role != RoleSystem {
			filtered = append(filtered, m)
		}
	}
	filtered = append(filtered, Message{
		Role:    RoleUser,
		Content: "请用简短的几句话总结以上对话的要点。",
	})

	req := &ChatRequest{
		Model:       p.cfg.Model,
		Messages:    filtered,
		Temperature: p.cfg.Temperature,
		TopP:        p.cfg.TopP,
	}

	summaryCtx, summaryCancel := context.WithTimeout(context.Background(), p.cfg.APITimeout)
	defer summaryCancel()
	resp, err := p.prov.Chat(summaryCtx, req)
	if err != nil {
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		if e := newCtx.ReplyText("❌ 生成总结失败: " + formatAIError(err)); e != nil {
			logger.Errorf("doSummary reply error: %v", e)
		}
		return
	}

	if resp.Content != "" {
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		if p.cfg.Markdown {
			_, e := newCtx.Reply(platform.MarkdownMessage(resp.Content))
			if e != nil {
				logger.Errorf("doSummary reply error: %v", e)
			}
		} else {
			if e := newCtx.ReplyText("📋 **对话总结**\n\n" + resp.Content); e != nil {
				logger.Errorf("doSummary reply error: %v", e)
			}
		}
	}
}
