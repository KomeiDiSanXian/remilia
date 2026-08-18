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
//   - /ai skill: 管理自定义技能（add/list/remove/enable/disable/promote/info）
//
// handleSubCommand 作为 @bot/私聊路径的子命令入口，
// 将自然语言命令映射到 execSubCommand 的具体实现。
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
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
		ctx.ReplyText("✅ 对话历史已清空，开始全新的对话吧！")
		return nil

	case "undo":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("没有可以撤销的对话")
			return nil
		}
		session.LockTurn()
		defer session.UnlockTurn()
		session.Lock()
		defer session.Unlock()
		if len(session.Messages) <= 1 {
			ctx.ReplyText("没有可以撤销的对话")
			return nil
		}
		lastUserIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleUser {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx <= 0 {
			ctx.ReplyText("没有可以撤销的对话")
			return nil
		}
		session.Messages = session.Messages[:lastUserIdx]
		p.sm.saveNoLock(session)
		ctx.ReplyText("↩️ 已撤销上一条对话")
		return nil

	case "retry":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("没有可以重试的对话")
			return nil
		}
		session.LockTurn()
		defer session.UnlockTurn()
		session.Lock()
		if len(session.Messages) <= 1 {
			session.Unlock()
			ctx.ReplyText("没有可以重试的对话")
			return nil
		}
		lastAssistantIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleAssistant {
				lastAssistantIdx = i
				break
			}
		}
		if lastAssistantIdx < 0 {
			session.Unlock()
			ctx.ReplyText("没有可以重试的对话")
			return nil
		}
		session.Messages = session.Messages[:lastAssistantIdx]
		p.sm.saveNoLock(session)
		session.Unlock()

		_ = ctx.TrySendTyping()
		result, err := p.processWithTools(ctx, session)
		if err != nil {
			ctx.ReplyText(formatAIError(err))
			return nil
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
			p.replyAndRecord(ctx, msg)
			return nil
		}
		return nil

	case "summary":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("还没有任何对话内容可以总结")
			return nil
		}
		msgsSnapshot := session.SnapshotMessages()
		if len(msgsSnapshot) <= 1 {
			ctx.ReplyText("还没有任何对话内容可以总结")
			return nil
		}

		// 单飞：同一会话已有总结任务在跑时不再启动新的 goroutine
		p.summaryMu.Lock()
		if p.summaries[sessionID] {
			p.summaryMu.Unlock()
			ctx.ReplyText("⏳ 正在生成对话总结，请稍候...")
			return nil
		}
		p.summaries[sessionID] = true
		p.summaryMu.Unlock()

		go func() {
			defer func() {
				p.summaryMu.Lock()
				delete(p.summaries, sessionID)
				p.summaryMu.Unlock()
			}()
			p.doSummary(ctx, msgsSnapshot)
		}()
		ctx.ReplyText("⏳ 正在生成对话总结，请稍候...")
		return nil

	case "status":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("当前没有活跃的对话")
			return nil
		}
		session.Lock()
		defer session.Unlock()
		if len(session.Messages) <= 1 {
			ctx.ReplyText("当前没有活跃的对话")
			return nil
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
		fmt.Fprintf(&b, "  - 提供商：`%s`\n", p.cfg.Provider)
		fmt.Fprintf(&b, "  - 模型：`%s`\n", p.cfg.Model)
		fmt.Fprintf(&b, "  - 消息数：`%d`（含 %d 条系统提示）\n", msgCount, sysCount)
		fmt.Fprintf(&b, "  - 对话时长：`%s`\n", formatDuration(duration))
		fmt.Fprintf(&b, "  - 会话 ID：`%s`\n", sessionID)
		ctx.ReplyText(b.String())
		return nil

	case "stats":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("当前没有活跃的对话")
			return nil
		}
		session.Lock()
		if len(session.Messages) <= 1 {
			session.Unlock()
			ctx.ReplyText("当前没有活跃的对话")
			return nil
		}
		callCount := session.CallCount
		toolCount := session.ToolCount
		session.Unlock()
		var b strings.Builder
		b.WriteString("📈 **使用统计**\n\n")
		fmt.Fprintf(&b, "  - LLM 调用次数：`%d`\n", callCount)
		fmt.Fprintf(&b, "  - 工具调用次数：`%d`\n", toolCount)
		ctx.ReplyText(b.String())
		return nil

	case "trace":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil {
			ctx.ReplyText("当前没有活跃的对话")
			return nil
		}
		entries := session.ToolTrace()
		if len(entries) == 0 {
			ctx.ReplyText("当前会话还没有工具调用记录。")
			return nil
		}
		var b strings.Builder
		b.WriteString("🔍 **工具调用追踪**（最近 " + fmt.Sprint(len(entries)) + " 条）\n\n")
		for _, e := range entries {
			mark := "✅"
			if e.Err != "" {
				mark = "❌"
			}
			fmt.Fprintf(&b, "%s `%s` 耗时 %s\n", mark, e.ToolName, formatDuration(e.Duration))
			if e.Args != "" {
				fmt.Fprintf(&b, "    参数：`%s`\n", e.Args)
			}
			if e.Err != "" {
				fmt.Fprintf(&b, "    错误：%s\n", e.Err)
			}
		}
		ctx.ReplyText(b.String())
		return nil

	case "tools", "help":
		var b strings.Builder
		b.WriteString("我可以使用以下工具：\n\n")
		tools := p.reg.List()
		userSkills := p.skillReg.ListByOwner(sender.ID)
		totalTools := len(tools) + len(userSkills)
		if totalTools == 0 {
			b.WriteString("（当前没有可用工具）")
		} else {
			for _, t := range tools {
				fmt.Fprintf(&b, "  - **%s**：%s\n", t.Name, t.Description)
			}
			for _, s := range userSkills {
				if s.Enabled {
					fmt.Fprintf(&b, "  - **%s**：%s *(自定义)*\n", s.Name, s.Description)
				}
			}
		}
		b.WriteString("\n在对话中直接告诉我你想使用哪个工具即可。\n")
		b.WriteString("\n**子命令：**")
		fmt.Fprintf(&b, "\n  `%s reset` — 清空对话历史", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s undo` — 撤销上一条对话", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s retry` — 重新生成上一条回复", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s summary` — 总结当前对话", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s status` — 查看会话状态", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s stats` — 查看使用统计", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s tools` — 列出可用工具", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s remind` — 设置定时提醒（如 `%s remind 5分钟 去喝水`）", p.cfg.TriggerCmd, p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s approve <ID>` — 批准工具执行（`%s deny <ID>` 拒绝）", p.cfg.TriggerCmd, p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s group` — 管理本群 AI 策略（提示词/工具白名单/审批/@触发）", p.cfg.TriggerCmd)
		fmt.Fprintf(&b, "\n  `%s skill` — 管理自定义技能", p.cfg.TriggerCmd)
		ctx.ReplyText(b.String())
		return nil

	case "skill":
		return p.handleSkillCommand(ctx)

	case "remind", "提醒":
		// 从消息内容中提取 "remind" 之后的参数（时长 + 内容）
		content := p.cleanMessage(ctx.GetMessageContent())
		content = strings.TrimSpace(strings.TrimLeft(content, "@"))
		for _, prefix := range []string{"remind", "提醒"} {
			content = strings.TrimPrefix(content, prefix)
		}
		content = strings.TrimSpace(content)
		return p.handleRemindCommand(ctx, content)

	case "approve", "允许", "批准":
		return p.handleApprovalCommand(ctx, true)

	case "deny", "拒绝", "驳回":
		return p.handleApprovalCommand(ctx, false)

	case "group", "群配置":
		return p.handleGroupCommand(ctx)
	}
	return nil
}

// handleSubCommand 处理 @bot/私聊路径的子命令，通过内容字符串匹配。
// 返回 true 表示已处理。
func (p *Plugin) handleSubCommand(ctx *eventctx.Context, content string) bool {
	cmd := strings.ToLower(strings.TrimSpace(content))
	var err error

	// 仅整词匹配 skill/技能 子命令前缀，避免误命中 "skillful"、"技能介绍" 等普通聊天
	if cmd == "skill" || strings.HasPrefix(cmd, "skill ") || cmd == "技能" || strings.HasPrefix(cmd, "技能 ") {
		err = p.handleSkillCommand(ctx)
		return err == nil
	}

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
	case "remind", "提醒":
		// 支持 "@机器人 提醒 5分钟 去喝水" 自然语言路径
		content := p.cleanMessage(ctx.GetMessageContent())
		content = strings.TrimSpace(strings.TrimLeft(content, "@"))
		for _, prefix := range []string{"remind", "提醒"} {
			content = strings.TrimPrefix(content, prefix)
		}
		content = strings.TrimSpace(content)
		err = p.handleRemindCommand(ctx, content)
	case "approve", "允许", "批准", "同意":
		// 支持 "@机器人 批准 A1" 自然语言路径
		err = p.handleApprovalCommand(ctx, true)
	case "deny", "拒绝", "驳回", "不同意":
		err = p.handleApprovalCommand(ctx, false)
	case "group", "群配置":
		err = p.handleGroupCommand(ctx)
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
// 使用 p.lifecycleCtx 作为父上下文，确保插件关闭时及时取消。
func (p *Plugin) doSummary(origCtx *eventctx.Context, msgs []Message) {
	filtered := make([]Message, 0, len(msgs)+1)
	for _, m := range msgs {
		if m.Role != RoleSystem {
			filtered = append(filtered, m)
		}
	}
	// 会话历史可能残留孤儿 tool 消息（上下文裁剪/中断导致），
	// 先修复工具调用序列再发送，避免后台总结也被 API 以 400 拒绝。
	filtered = repairToolCallSequence(filtered)
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

	summaryCtx, summaryCancel := context.WithTimeout(p.lifecycleCtx, p.cfg.APITimeout)
	defer summaryCancel()
	resp, err := p.prov.Chat(summaryCtx, req)
	if err != nil {
		// 插件关闭导致的取消不回复错误消息
		if p.lifecycleCtx.Err() != nil {
			return
		}
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		if e := newCtx.ReplyText("❌ 生成总结失败: " + formatAIError(err)); e != nil {
			logger.Errorf("doSummary reply error: %v", e)
		}
		return
	}

	if resp.Content != "" {
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		if p.cfg.Markdown {
			p.replyAndRecord(newCtx, platform.MarkdownMessage(resp.Content))
		} else {
			p.replyAndRecord(newCtx, platform.TextMessage("📋 **对话总结**\n\n"+resp.Content))
		}
	}
}

// --- Skill 管理命令 ---

// handleSkillCommand 处理 /ai skill 子命令的入口。
// 从消息内容中解析子子命令（add/list/remove/enable/disable/promote/info）并分发。
func (p *Plugin) handleSkillCommand(ctx *eventctx.Context) error {
	content := p.cleanMessage(ctx.GetMessageContent())
	content = strings.TrimSpace(strings.TrimLeft(content, "@"))
	content = strings.TrimPrefix(content, "skill")
	content = strings.TrimPrefix(content, "技能")
	content = strings.TrimSpace(content)

	parts := strings.SplitN(content, " ", 2)
	subCmd := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}

	sender := ctx.GetSenderInfo()
	ownerID := sender.ID

	switch subCmd {
	case "add":
		return p.handleSkillAdd(ctx, rest, ownerID)
	case "list":
		return p.handleSkillList(ctx, ownerID)
	case "remove", "delete", "rm":
		return p.handleSkillRemove(ctx, rest, ownerID)
	case "enable":
		return p.handleSkillToggle(ctx, rest, ownerID, true)
	case "disable":
		return p.handleSkillToggle(ctx, rest, ownerID, false)
	case "promote":
		return p.handleSkillPromote(ctx, rest, ownerID)
	case "info":
		return p.handleSkillInfo(ctx, rest, ownerID)
	default:
		ctx.ReplyText(
			"📋 **Skill 管理命令**\n\n" +
				fmt.Sprintf("  `%s skill add <名称>` — 注册新技能（发送 Markdown 内容或附件）\n", p.cfg.TriggerCmd) +
				fmt.Sprintf("  `%s skill list` — 列出我的技能\n", p.cfg.TriggerCmd) +
				fmt.Sprintf("  `%s skill remove <名称>` — 删除技能\n", p.cfg.TriggerCmd) +
				fmt.Sprintf("  `%s skill enable/disable <名称>` — 启用/禁用技能\n", p.cfg.TriggerCmd) +
				fmt.Sprintf("  `%s skill info <名称>` — 查看技能详情\n", p.cfg.TriggerCmd) +
				fmt.Sprintf("  `%s skill promote <名称>` — 提升为系统技能\n", p.cfg.TriggerCmd),
		)
		return nil
	}
}

// handleSkillAdd 处理 /ai skill add <name> [content]。
//
// 支持两种注册方式：
//  1. 一步到位：在命令后直接粘贴 Markdown 内容
//  2. 分两步：仅指定名称，系统等待下一条消息作为内容
//
// 也支持发送 .md 文件附件作为技能内容。
func (p *Plugin) handleSkillAdd(ctx *eventctx.Context, rest, ownerID string) error {
	// 使用 strings.Fields 提取首个空白分隔的 token 作为名称，
	// 支持换行分隔场景如 "/ai skill add my_skill\nmarkdown 正文"
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		ctx.ReplyText("❌ 请指定技能名称。用法：`" + p.cfg.TriggerCmd + " skill add <名称> <Markdown 内容>`\n" +
			"支持两种方式：\n" +
			"  1. `" + p.cfg.TriggerCmd + ` skill add my_skill 你是...` + "` — 一次性内联注册\n" +
			"  2. `" + p.cfg.TriggerCmd + " skill add my_skill` — 仅指定名称，然后发送 Markdown 内容或 .md 附件")
		return nil
	}
	name := fields[0]
	prompt := ""
	if len(rest) > len(name) {
		prompt = strings.TrimSpace(rest[len(name):])
	}

	// 尝试附件
	if prompt == "" {
		atts := platform.Attachments(ctx.GetPlatformEvent())
		for _, att := range atts {
			if strings.HasPrefix(att.MimeType, "text/") || strings.HasSuffix(att.URL, ".md") {
				if content := p.downloadTextAttachment(ctx.Context(), att); content != "" {
					prompt = content
					break
				}
			}
		}
	}

	if prompt == "" {
		// 两步注册：通过 FSM 等待用户下一条消息
		sessionID := makeSkillAddSessionID(ctx)
		if err := p.fsmEngine.StartSession(ctx, "skill_add", sessionID); err != nil {
			if errors.Is(err, fsm.ErrSessionExists) {
				ctx.ReplyText("❌ 你已有一个待完成的技能注册，请先发送内容或发送 cancel 取消。")
				return nil
			}
			ctx.ReplyText("❌ 无法创建技能注册会话：" + err.Error())
			return nil
		}
		// GetSession 现在返回副本（core 复查修复）；
		// 通过 UpdateSessionData 在会话锁内写入初始数据。
		p.fsmEngine.UpdateSessionData(sessionID, func(data map[string]any) {
			data["name"] = name
			data["ownerID"] = ownerID
		})
		ctx.ReplyText(fmt.Sprintf(
			"📝 请发送 Markdown 内容来定义技能 `%s`。\n"+
				"支持文本消息或 .md 文件附件。\n"+
				"发送 cancel 或 取消 可放弃注册。", name))
		// 两步注册已启动，等待用户下一条消息，本次不立即注册。
		return nil
	}

	return p.registerSkillAndReply(ctx, name, prompt, ownerID)
}

// registerSkillAndReply 注册技能并回复用户。
func (p *Plugin) registerSkillAndReply(ctx *eventctx.Context, name, prompt, ownerID string) error {
	desc := extractSkillDescription(prompt)
	skill := Skill{
		Name:        name,
		Description: desc,
		Prompt:      prompt,
		Enabled:     true,
	}
	if err := p.RegisterUserSkill(skill, ownerID); err != nil {
		ctx.ReplyText("❌ " + err.Error())
		return nil
	}
	ctx.ReplyText(fmt.Sprintf("✅ 技能 `%s%s` 已注册！现在可以在对话中指示 AI 调用它。\n> %s",
		UserSkillPrefix, name, desc))
	return nil
}

// handleSkillList 列出当前用户的所有技能及其状态和调用次数。
func (p *Plugin) handleSkillList(ctx *eventctx.Context, ownerID string) error {
	skills := p.skillReg.ListByOwner(ownerID)
	if len(skills) == 0 {
		ctx.ReplyText("📭 你还没有注册任何自定义技能。\n使用 `" + p.cfg.TriggerCmd + " skill add <名称> <Markdown 内容>` 开始创建。")
		return nil
	}

	var b strings.Builder
	b.WriteString("📋 **我的技能**\n\n")
	for _, s := range skills {
		status := "✅ 启用"
		if !s.Enabled {
			status = "⛔ 禁用"
		}
		fmt.Fprintf(&b, "  - **%s**：%s 调用 %d 次 — %s\n", s.Name, s.Description, s.UsageCount, status)
	}
	ctx.ReplyText(b.String())
	return nil
}

// handleSkillRemove 删除指定技能。支持带或不带 u_ 前缀的名称。
func (p *Plugin) handleSkillRemove(ctx *eventctx.Context, name, ownerID string) error {
	if name == "" {
		ctx.ReplyText("❌ 请指定要删除的技能名称。")
		return nil
	}

	fullName := name
	if !strings.HasPrefix(fullName, UserSkillPrefix) {
		fullName = UserSkillPrefix + name
	}

	if err := p.skillReg.Remove(fullName, ownerID); err != nil {
		if !strings.HasPrefix(name, UserSkillPrefix) {
			if err2 := p.skillReg.Remove(name, ownerID); err2 == nil {
				ctx.ReplyText(fmt.Sprintf("🗑️ 技能 `%s` 已删除。", name))
				return nil
			}
		}
		ctx.ReplyText("❌ " + err.Error())
		return nil
	}
	ctx.ReplyText(fmt.Sprintf("🗑️ 技能 `%s` 已删除。", fullName))
	return nil
}

// handleSkillToggle 启用或禁用指定技能。
func (p *Plugin) handleSkillToggle(ctx *eventctx.Context, name, ownerID string, enabled bool) error {
	if name == "" {
		ctx.ReplyText("❌ 请指定技能名称。")
		return nil
	}

	fullName := name
	if !strings.HasPrefix(fullName, UserSkillPrefix) {
		fullName = UserSkillPrefix + name
	}

	s, err := p.skillReg.SetEnabled(ownerID, fullName, enabled)
	if err != nil {
		ctx.ReplyText("❌ 未找到技能 `" + name + "`")
		return nil
	}

	action := "已启用"
	if !enabled {
		action = "已禁用"
	}
	ctx.ReplyText(fmt.Sprintf("✅ 技能 `%s` %s。", s.Name, action))
	return nil
}

// isAdmin 检查当前用户是否具有管理员/超级管理员权限。
// 优先使用 RBAC 权限管理器，不存在时返回 false（安全默认）。
func (p *Plugin) isAdmin(ctx *eventctx.Context) bool {
	pm := ctx.GetPermissionManager()
	if pm == nil {
		return false
	}
	roles := pm.GetUserRoles(ctx.GetUserID())
	for _, r := range roles {
		if r == "admin" || r == "superadmin" {
			return true
		}
	}
	return false
}

// handleSkillPromote 将用户技能提升为系统级技能。
// 提升后所有用户均可见可调用。需要管理员权限。
func (p *Plugin) handleSkillPromote(ctx *eventctx.Context, name, ownerID string) error {
	if name == "" {
		ctx.ReplyText("❌ 请指定要提升的技能名称。")
		return nil
	}

	if !p.isAdmin(ctx) {
		ctx.ReplyText("❌ 仅管理员可以提升技能为系统级。")
		return nil
	}

	fullName := name
	if !strings.HasPrefix(fullName, UserSkillPrefix) {
		fullName = UserSkillPrefix + name
	}

	if err := p.skillReg.Promote(fullName, ownerID); err != nil {
		ctx.ReplyText("❌ " + err.Error())
		return nil
	}

	// Promote 内部已将用户技能重命名为去前缀的系统级名称，
	// 必须用新名称查询并注册为工具（用户可能以带或不带 u_ 前缀的形式调用）。
	newName := strings.TrimPrefix(fullName, UserSkillPrefix)
	if s, ok := p.skillReg.GetSystem(newName); ok {
		p.registerSkillAsTool(s)
	}

	ctx.ReplyText(fmt.Sprintf("⬆️ 技能 `%s` 已提升为系统级，所有用户均可使用。", name))
	return nil
}

// handleSkillInfo 查看技能详情，包括所有者、描述、状态、调用次数和 Prompt 预览。
func (p *Plugin) handleSkillInfo(ctx *eventctx.Context, name, ownerID string) error {
	if name == "" {
		ctx.ReplyText("❌ 请指定技能名称。")
		return nil
	}

	s, ok := p.skillReg.GetByOwner(ownerID, name)
	if !ok {
		s, ok = p.skillReg.GetByOwner(ownerID, UserSkillPrefix+name)
	}
	if !ok {
		s, ok = p.skillReg.GetSystem(name)
	}
	if !ok {
		ctx.ReplyText("❌ 未找到技能 `" + name + "`")
		return nil
	}
	if s.OwnerID != OwnerSystem && s.OwnerID != ownerID {
		ctx.ReplyText("❌ 未找到技能 `" + name + "`")
		return nil
	}

	ownerLabel := "系统"
	if s.OwnerID != OwnerSystem {
		ownerLabel = "用户"
	}
	statusLabel := "✅ 启用"
	if !s.Enabled {
		statusLabel = "⛔ 禁用"
	}

	var b strings.Builder
	b.WriteString("📄 **技能详情**\n\n")
	fmt.Fprintf(&b, "  - **名称**：`%s`\n", s.Name)
	fmt.Fprintf(&b, "  - **类型**：%s\n", ownerLabel)
	fmt.Fprintf(&b, "  - **描述**：%s\n", s.Description)
	fmt.Fprintf(&b, "  - **状态**：%s\n", statusLabel)
	fmt.Fprintf(&b, "  - **调用次数**：%d\n", s.UsageCount)
	fmt.Fprintf(&b, "  - **Prompt 长度**：%d 字符\n\n", len(s.Prompt))

	preview := truncateRunes(s.Prompt, 500)
	b.WriteString("**Prompt 预览：**\n")
	b.WriteString("```\n" + preview + "\n```")

	ctx.ReplyText(b.String())
	return nil
}

// extractSkillDescription 从 Prompt 中提取第一行作为技能描述。
// 最多保留 200 个字符。
func extractSkillDescription(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	lines := strings.SplitN(prompt, "\n", 2)
	return truncateRunes(strings.TrimSpace(lines[0]), 200)
}

// truncateRunes 按 rune 截断字符串，避免劈开多字节 UTF-8 字符。
// 超过 max 个字符时以省略号结尾。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
