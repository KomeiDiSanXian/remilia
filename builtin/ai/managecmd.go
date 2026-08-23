// Package ai managecmd.go — 记忆 / 待办 / 计划的管理子命令。
//
// 这三个能力原本只有模型侧工具（memory_* / todo_* / create_plan），
// 用户缺少直接查看与管理的入口。本文件补齐：
//   - /ai memory：查看我的记忆（群聊同时显示本群记忆）、删除、清空（group 需群管理员）
//   - /ai todo：会话待办清单的增删改查与清理
//   - /ai plan：查看/取消当前任务计划
//
// 与 remind 子命令同构：子命令名之后的参数从消息内容解析（兼容
// "/ai memory clear group" 与 "@bot memory clear group" 两种路径）。
package ai

import (
	"fmt"
	"strconv"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// subCommandRest 从消息内容中提取子命令之后的参数。
// 如 "/ai memory clear group" → "clear group"（前缀来自调用方）。
func (p *Plugin) subCommandRest(ctx *eventctx.Context, prefixes ...string) string {
	content := p.cleanMessage(ctx.GetMessageContent())
	content = strings.TrimSpace(strings.TrimLeft(content, "@"))
	for _, prefix := range prefixes {
		content = strings.TrimPrefix(content, prefix)
	}
	return strings.TrimSpace(content)
}

// --- /ai memory ---

// handleMemoryCommand 处理 /ai memory 子命令（list / clear [user|group]）。
func (p *Plugin) handleMemoryCommand(ctx *eventctx.Context, rest string) error {
	if p.memory == nil {
		p.replyFormatted(ctx, "❌ 长期记忆未启用（memory_enabled=false）")
		return nil
	}
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	subCmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	switch subCmd {
	case "", "list", "ls", "查看":
		return p.handleMemoryList(ctx)
	case "clear", "清空":
		return p.handleMemoryClear(ctx, arg)
	case "remove", "rm", "del", "删除":
		return p.handleMemoryRemove(ctx, arg)
	default:
		p.replyFormatted(ctx, memoryHelpText(p.cfg.TriggerCmd))
		return nil
	}
}

// handleMemoryList 列出当前用户（及所在群）的长期记忆。
func (p *Plugin) handleMemoryList(ctx *eventctx.Context) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	var b strings.Builder
	b.WriteString("🧠 **长期记忆**\n\n")

	userFacts := p.memory.Facts(userScope(sender.ID))
	if len(userFacts) == 0 {
		b.WriteString("  - 我的记忆：暂无（AI 会在对话中自动记录偏好与事实）\n")
	} else {
		fmt.Fprintf(&b, "  - 我的记忆（%s）：\n", memoryCountLabel(len(userFacts), p.cfg.MemoryMaxFacts))
		for i, f := range userFacts {
			fmt.Fprintf(&b, "    %d. %s\n", i+1, f.Text)
		}
	}

	if chat.IsGroup {
		groupFacts := p.memory.Facts(groupScope(chat.ID))
		if len(groupFacts) == 0 {
			b.WriteString("  - 本群记忆：暂无\n")
		} else {
			fmt.Fprintf(&b, "  - 本群记忆（%s）：\n", memoryCountLabel(len(groupFacts), p.cfg.MemoryMaxFacts))
			for i, f := range groupFacts {
				fmt.Fprintf(&b, "    %d. %s\n", i+1, f.Text)
			}
		}
	}

	b.WriteString("\n也可以直接用语言让我记住/忘记某件事。")
	p.replyFormatted(ctx, b.String())
	return nil
}

// handleMemoryClear 清空指定作用域的记忆（默认 user；group 需 superadmin）。
func (p *Plugin) handleMemoryClear(ctx *eventctx.Context, scope string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "user", "我的":
		n := p.memory.Clear(userScope(sender.ID))
		if n == 0 {
			p.replyFormatted(ctx, "📭 我的记忆中没有任何事实需要清空")
		} else {
			p.replyFormatted(ctx, fmt.Sprintf("🧹 已清空我的记忆（%d 条）", n))
		}
		return nil
	case "group", "本群":
		if !chat.IsGroup {
			p.replyFormatted(ctx, "❌ 当前不是群聊，没有本群记忆")
			return nil
		}
		if !p.isGroupAdmin(ctx) {
			p.replyFormatted(ctx, "❌ 清空本群公共记忆需要群管理员（群主/管理员）角色")
			return nil
		}
		n := p.memory.Clear(groupScope(chat.ID))
		if n == 0 {
			p.replyFormatted(ctx, "📭 本群记忆中没有任何事实需要清空")
		} else {
			p.replyFormatted(ctx, fmt.Sprintf("🧹 已清空本群记忆（%d 条）", n))
		}
		return nil
	default:
		p.replyFormatted(ctx, "❌ 未知作用域，用法：`"+p.cfg.TriggerCmd+" memory clear [user|group]`")
		return nil
	}
}

// handleMemoryRemove 删除某条记忆：支持按列表序号（如 2）或文本片段
// （如 冰美式）。可加作用域前缀：memory remove group <序号|文本>。
func (p *Plugin) handleMemoryRemove(ctx *eventctx.Context, rest string) error {
	scope := "user"
	target := strings.TrimSpace(rest)
	if fields := strings.Fields(target); len(fields) > 1 {
		switch strings.ToLower(fields[0]) {
		case "user", "我的":
			scope = "user"
			target = strings.TrimSpace(strings.TrimPrefix(target, fields[0]))
		case "group", "本群":
			scope = "group"
			target = strings.TrimSpace(strings.TrimPrefix(target, fields[0]))
		}
	}
	if target == "" {
		p.replyFormatted(ctx, "❌ 请指定要删除的记忆，用法：`"+p.cfg.TriggerCmd+
			" memory remove <序号|文本>`（序号见 `"+p.cfg.TriggerCmd+" memory`；群记忆加 group：`"+
			p.cfg.TriggerCmd+" memory remove group <序号|文本>`）")
		return nil
	}

	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	scopeKey := userScope(sender.ID)
	label := "我的记忆"
	if scope == "group" {
		if !chat.IsGroup {
			p.replyFormatted(ctx, "❌ 当前不是群聊，没有本群记忆")
			return nil
		}
		scopeKey = groupScope(chat.ID)
		label = "本群记忆"
	}

	// 按列表序号删除（序号与 /ai memory 展示顺序一致）。
	if n, err := strconv.Atoi(target); err == nil {
		facts := p.memory.Facts(scopeKey)
		if n < 1 || n > len(facts) {
			p.replyFormatted(ctx, fmt.Sprintf("❌ 序号越界（当前%s共 %d 条）", label, len(facts)))
			return nil
		}
		text := facts[n-1].Text
		if removed := p.memory.RemoveWhere(scopeKey, func(f MemoryFact) bool { return f.Text == text }); removed > 0 {
			p.replyFormatted(ctx, fmt.Sprintf("🧹 已从%s删除：%s", label, text))
		} else {
			p.replyFormatted(ctx, "❌ 删除失败，请重试")
		}
		return nil
	}

	// 按文本片段删除（忽略大小写，删除所有包含该片段的记忆）。
	frag := strings.ToLower(target)
	removed := p.memory.RemoveWhere(scopeKey, func(f MemoryFact) bool {
		return strings.Contains(strings.ToLower(f.Text), frag)
	})
	if removed == 0 {
		p.replyFormatted(ctx, "📭 未在"+label+"中找到包含「"+target+"」的记忆")
		return nil
	}
	p.replyFormatted(ctx, fmt.Sprintf("🧹 已从%s删除 %d 条包含「%s」的记忆", label, removed, target))
	return nil
}

// memoryCountLabel 生成记忆条数标签（配置了上限时带上上限）。
func memoryCountLabel(n, max int) string {
	if max > 0 {
		return fmt.Sprintf("%d/%d", n, max)
	}
	return fmt.Sprintf("%d 条", n)
}

func memoryHelpText(triggerCmd string) string {
	return fmt.Sprintf(`🧠 **长期记忆管理**

  `+"`%s memory`"+`                  — 查看我的记忆（群聊同时显示本群记忆）
  `+"`%s memory remove <序号|文本>`"+` — 删除某条/包含文本的记忆（群记忆：`+"`%s memory remove group ...`"+`）
  `+"`%s memory clear`"+`           — 清空我的记忆
  `+"`%s memory clear group`"+`     — 清空本群公共记忆（需群管理员）

AI 会在对话中自动记录偏好与事实，也可以直接用语言让我记住/忘记某件事。`, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd)
}

// --- /ai todo ---

// handleTodoCommand 处理 /ai todo 子命令（list / add / done / remove / clear）。
func (p *Plugin) handleTodoCommand(ctx *eventctx.Context, rest string) error {
	if p.todos == nil {
		p.replyFormatted(ctx, "❌ 待办功能不可用")
		return nil
	}
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	subCmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch subCmd {
	case "", "list", "ls", "查看":
		filter := strings.ToLower(arg)
		if filter != "pending" && filter != "done" {
			filter = ""
		}
		return p.handleTodoList(ctx, filter)
	case "add", "新增", "添加":
		if arg == "" {
			p.replyFormatted(ctx, todoHelpText(p.cfg.TriggerCmd))
			return nil
		}
		id := p.todos.add(ctx.GetChatInfo().ID, arg)
		p.replyFormatted(ctx, fmt.Sprintf("✅ 已添加待办 `%s`：%s", id, arg))
		return nil
	case "done", "完成":
		return p.handleTodoDone(ctx, arg)
	case "remove", "rm", "del", "删除":
		return p.handleTodoRemove(ctx, arg)
	case "clear", "清空":
		return p.handleTodoClear(ctx, strings.ToLower(arg))
	default:
		p.replyFormatted(ctx, todoHelpText(p.cfg.TriggerCmd))
		return nil
	}
}

// handleTodoList 列出当前会话的待办（filter 可选 pending/done）。
func (p *Plugin) handleTodoList(ctx *eventctx.Context, filter string) error {
	items := p.todos.list(ctx.GetChatInfo().ID)
	var pending, done []todoItem
	for _, it := range items {
		if it.Done {
			done = append(done, it)
		} else {
			pending = append(pending, it)
		}
	}
	switch filter {
	case "pending":
		items = pending
	case "done":
		items = done
	}
	if len(items) == 0 {
		if filter == "" {
			p.replyFormatted(ctx, "📭 当前会话没有待办事项")
		} else {
			p.replyFormatted(ctx, "📭 没有"+filter+"状态的待办事项")
		}
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 **待办事项**（%d 条）\n\n", len(items))
	for _, it := range items {
		mark := "⬜"
		if it.Done {
			mark = "✅"
		}
		fmt.Fprintf(&b, "  - %s `%s` %s\n", mark, it.ID, it.Text)
	}
	p.replyFormatted(ctx, b.String())
	return nil
}

// handleTodoDone 标记一条待办完成（ID 来自 /ai todo list）。
func (p *Plugin) handleTodoDone(ctx *eventctx.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		p.replyFormatted(ctx, "❌ 请指定待办 ID，用法：`"+p.cfg.TriggerCmd+" todo done <ID>`")
		return nil
	}
	if p.todos.setDone(ctx.GetChatInfo().ID, id, true) {
		p.replyFormatted(ctx, fmt.Sprintf("✅ 待办 `%s` 已完成", id))
	} else {
		p.replyFormatted(ctx, "❌ 未找到待办 `"+id+"`（ID 见 `"+p.cfg.TriggerCmd+" todo list`）")
	}
	return nil
}

// handleTodoRemove 删除一条待办（ID 来自 /ai todo list）。
func (p *Plugin) handleTodoRemove(ctx *eventctx.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		p.replyFormatted(ctx, "❌ 请指定待办 ID，用法：`"+p.cfg.TriggerCmd+" todo remove <ID>`")
		return nil
	}
	if p.todos.remove(ctx.GetChatInfo().ID, id) {
		p.replyFormatted(ctx, fmt.Sprintf("🗑️ 已删除待办 `%s`", id))
	} else {
		p.replyFormatted(ctx, "❌ 未找到待办 `"+id+"`（ID 见 `"+p.cfg.TriggerCmd+" todo list`）")
	}
	return nil
}

// handleTodoClear 清除已完成（默认）或全部待办。
func (p *Plugin) handleTodoClear(ctx *eventctx.Context, arg string) error {
	chatID := ctx.GetChatInfo().ID
	switch arg {
	case "", "done":
		n := p.todos.clearDone(chatID)
		if n == 0 {
			p.replyFormatted(ctx, "📭 没有已完成的待办需要清除")
		} else {
			p.replyFormatted(ctx, fmt.Sprintf("🧹 已清除 %d 条已完成的待办", n))
		}
	case "all":
		n := p.todos.clearAll(chatID)
		if n == 0 {
			p.replyFormatted(ctx, "📭 当前会话没有待办需要清空")
		} else {
			p.replyFormatted(ctx, fmt.Sprintf("🧹 已清空全部待办（%d 条）", n))
		}
	default:
		p.replyFormatted(ctx, "❌ 未知参数，用法：`"+p.cfg.TriggerCmd+" todo clear [done|all]`")
	}
	return nil
}

func todoHelpText(triggerCmd string) string {
	return fmt.Sprintf(`📋 **待办管理**

  `+"`%s todo`"+`                      — 列出待办
  `+"`%s todo add <内容>`"+`           — 新增待办
  `+"`%s todo done <ID>`"+`            — 标记完成（如 T1）
  `+"`%s todo remove <ID>`"+`          — 删除待办
  `+"`%s todo clear`"+`                — 清除已完成的待办
  `+"`%s todo clear all`"+`            — 清空全部待办

待办按会话隔离，重启后失效。`, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd)
}

// --- /ai plan ---

// handlePlanCommand 处理 /ai plan 子命令（status / cancel）。
func (p *Plugin) handlePlanCommand(ctx *eventctx.Context, rest string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	session := p.sm.GetOrCreate(makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID), sender.ID, chat.ID)
	if session == nil {
		p.replyFormatted(ctx, "📭 当前没有进行中的计划")
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "", "status", "查看":
		plan := session.planSnapshot()
		if plan == nil || !plan.Active {
			p.replyFormatted(ctx, "📭 当前没有进行中的计划")
			return nil
		}
		text := strings.ReplaceAll(formatPlan(plan), "\n", "\n  ")
		p.replyFormatted(ctx, "🗺️ **当前计划**\n\n  "+text)
		return nil
	case "cancel", "取消":
		if session.cancelPlan() {
			p.replyFormatted(ctx, "🛑 已取消当前计划（剩余步骤不再自动推进）")
		} else {
			p.replyFormatted(ctx, "📭 当前没有进行中的计划可取消")
		}
		return nil
	default:
		p.replyFormatted(ctx, planHelpText(p.cfg.TriggerCmd))
		return nil
	}
}

func planHelpText(triggerCmd string) string {
	return fmt.Sprintf(`🗺️ **任务计划**

  `+"`%s plan`"+`            — 查看当前计划进度
  `+"`%s plan cancel`"+`     — 取消当前计划

复杂任务由 AI 自动创建计划并逐步推进（create_plan），此命令用于查看进度与取消。`, triggerCmd, triggerCmd)
}
