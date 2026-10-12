// Package ai groupcmd.go — /ai group 管理命令实现。
//
// 管理当前群/全局的 AI 策略（per-group 工具策略与提示词）：
//
//	/ai group status                          — 查看本群生效配置
//	/ai group set prompt <text>               — 设置群提示词
//	/ai group set tools all|none|t1,t2        — 设置群工具白名单
//	/ai group set approval off|restricted|always — 设置群审批模式
//	/ai group set mention on|off              — 设置群 @ 触发要求
//	/ai group reset [prompt|tools|approval|mention|all] — 重置群配置
//	/ai group global status|reset             — 管理全局默认（需 superadmin）
//
// 权限：群级配置要求群管理员（平台群主/管理员 或 RBAC admin/superadmin）；
// 全局配置要求 superadmin 角色。
package ai

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// handleGroupCommand 处理 /ai group 子命令。
func (p *Plugin) handleGroupCommand(ctx *eventctx.Context) error {
	content := runtime.CleanMessage(ctx.GetMessageContent(), p.triggerCmd)
	content = strings.TrimSpace(strings.TrimLeft(content, "@"))
	// 剥掉 "group"/"群配置" 前缀
	for _, prefix := range []string{"group", "群配置"} {
		content = strings.TrimPrefix(content, prefix)
	}
	content = strings.TrimSpace(content)

	// /ai group global ... / "全局" ... 分支（全局配置管理）。
	// "全局" 前缀必须一并剥离，否则 "全局 status" 进入 handleGroupGlobal 后
	// rest 仍带 "全局"，落 default 报用法错误，中文别名形同虚设。
	if rest, ok := strings.CutPrefix(content, "global"); ok {
		return p.handleGroupGlobal(ctx, strings.TrimSpace(rest))
	}
	if rest, ok := strings.CutPrefix(content, "全局"); ok {
		return p.handleGroupGlobal(ctx, strings.TrimSpace(rest))
	}

	parts := strings.Fields(content)
	if len(parts) == 0 {
		p.replyFormatted(ctx, p.groupCommandHelp(p.cfg.TriggerCmd))
		return nil
	}

	switch parts[0] {
	case "status", "查看":
		return p.handleGroupStatus(ctx)
	case "set", "设置":
		return p.handleGroupSet(ctx, parts[1:])
	case "reset", "重置", "clear":
		return p.handleGroupReset(ctx, parts[1:])
	default:
		p.replyFormatted(ctx, p.groupCommandHelp(p.cfg.TriggerCmd))
		return nil
	}
}

// handleGroupStatus 显示当前群（或调用者所在会话）的生效配置。
func (p *Plugin) handleGroupStatus(ctx *eventctx.Context) error {
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		ctx.ReplyText("❌ /ai group 仅限群聊场景使用")
		return nil
	}
	gp := p.groupPolicies.Effective(chat.ID)
	groupID := chat.ID

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 **本群 AI 策略** (`%s`)\n\n", groupID))

	prompt := gp.EffectiveSystemPrompt()
	if prompt == "" {
		prompt = "（使用全局/默认提示词）"
	}
	fmt.Fprintf(&b, "  - **提示词**: %s\n", p.ellipsize(prompt, 60))

	toolPolicy := "all"
	if gp.ToolPolicy != nil {
		toolPolicy = *gp.ToolPolicy
	}
	fmt.Fprintf(&b, "  - **工具白名单**: `%s`\n", toolPolicy)

	approval := p.cfg.ToolApproval
	if a := gp.EffectiveApproval(); a != "" {
		approval = a
	}
	if approval == "" {
		approval = "off"
	}
	fmt.Fprintf(&b, "  - **审批模式**: `%s`\n", approval)

	if require, ok := gp.EffectiveRequireMention(); ok {
		mode := "自主发言（不强制 @）"
		if require {
			mode = "必须 @ 触发"
		}
		fmt.Fprintf(&b, "  - **@ 触发**: %s\n", mode)
	} else {
		globalMode := "自主发言"
		if p.cfg.AtBot && !p.cfg.GroupAutonomous && !p.cfg.Fallback {
			globalMode = "必须 @ 触发"
		}
		fmt.Fprintf(&b, "  - **@ 触发**: %s（全局默认）\n", globalMode)
	}
	// 接话策略展示：群覆盖 > 全局配置。
	replyPolicy := p.cfg.GroupReplyPolicy
	if r := gp.EffectiveReplyPolicy(); r != "" {
		replyPolicy = r
	}
	replyChance := p.cfg.GroupReplyChance
	if c, ok := gp.EffectiveReplyChance(); ok {
		replyChance = c
	}
	switch replyPolicy {
	case config.GroupReplySmart:
		fmt.Fprintf(&b, "  - **接话策略**: `smart`（智能接话：强信号必回，闲聊按 %.0f%% 概率）\n", replyChance*100)
	case config.GroupReplyJudge:
		fmt.Fprintf(&b, "  - **接话策略**: `judge`（LLM 决定是否接话，闲聊判定失败按 %.0f%% 概率兜底）\n", replyChance*100)
	default:
		fmt.Fprintf(&b, "  - **接话策略**: `all`（每条都回）\n")
	}

	p.replyFormatted(ctx, b.String())
	return nil
}

// handleGroupSet 处理 /ai group set <field> <value>。
func (p *Plugin) handleGroupSet(ctx *eventctx.Context, args []string) error {
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		ctx.ReplyText("❌ /ai group 仅限群聊场景使用")
		return nil
	}
	if !p.isGroupAdmin(ctx) {
		ctx.ReplyText("❌ 需要群管理员权限才能修改本群 AI 策略")
		return nil
	}
	if len(args) < 2 {
		p.replyFormatted(ctx, p.groupCommandHelp(p.cfg.TriggerCmd))
		return nil
	}

	field := strings.ToLower(args[0])
	value := strings.TrimSpace(strings.Join(args[1:], " "))
	policy := &groupPolicy{}

	switch field {
	case "prompt", "提示词":
		policy.SystemPrompt = &value
	case "tools", "工具":
		norm := strings.ToLower(strings.ReplaceAll(value, "，", ","))
		switch norm {
		case "all", "全部":
			policy.ToolPolicy = new("all")
		case "none", "无", "off":
			policy.ToolPolicy = new("none")
		default:
			// 逗号分隔工具名白名单：大小写不敏感地解析到真实工具名
			// （FilterTools 按归一化小写比较），并把解析不到的名字回显给
			// 管理员——此前拼错名字会让整组工具静默清零，回复还只有一句
			// "✅ 已更新"，无从发现。
			known := make(map[string]string) // 小写名 → 注册表真实名
			if p.reg != nil {
				for _, t := range p.reg.List() {
					known[strings.ToLower(t.Name)] = t.Name
				}
			}
			if len(known) == 0 {
				// 注册表不可用（nil 或空）：无法校验存在性，按原样存入小写名。
				policy.ToolPolicy = new(norm)
			} else {
				var matched, unknown []string
				for name := range strings.SplitSeq(norm, ",") {
					name = strings.TrimSpace(name)
					if name == "" {
						continue
					}
					if real, ok := known[name]; ok {
						matched = append(matched, real)
					} else {
						unknown = append(unknown, name)
					}
				}
				if len(matched) == 0 {
					ctx.ReplyText(fmt.Sprintf("❌ 工具白名单中没有任何已注册工具（输入：%s），配置未生效", value))
					return nil
				}
				policy.ToolPolicy = new(strings.Join(matched, ","))
				if len(unknown) > 0 {
					p.replyFormatted(ctx, fmt.Sprintf("⚠️ 以下工具未注册，已忽略：%s（生效：%s）",
						strings.Join(unknown, ", "), strings.Join(matched, ", ")))
				}
			}
		}
	case "approval", "审批":
		norm := strings.ToLower(value)
		if !slices.Contains(validApprovalModes, norm) {
			ctx.ReplyText("❌ 审批模式必须为 off / restricted / always")
			return nil
		}
		policy.Approval = &norm
	case "mention", "触发", "@触发":
		norm := strings.ToLower(value)
		switch norm {
		case "on", "true", "1", "必须", "开启":
			policy.RequireMention = new(true)
		case "off", "false", "0", "自主", "关闭":
			policy.RequireMention = new(false)
		default:
			ctx.ReplyText("❌ mention 必须为 on / off")
			return nil
		}
	case "reply", "回复", "接话":
		// 取值：all / smart / judge / <概率>（如 0.5）/ <模式> <概率>。
		// 概率仅弱信号层使用：smart 是抽签阈值，judge 是 LLM 判定失败时的回退阈值。
		mode, chance, parseErr := parseGroupReplyArgs(args[1:])
		if parseErr != nil {
			ctx.ReplyText(parseErr.Error())
			return nil
		}
		policy.ReplyPolicy = mode
		policy.ReplyChance = chance
	default:
		p.replyFormatted(ctx, p.groupCommandHelp(p.cfg.TriggerCmd))
		return nil
	}

	p.groupPolicies.SetGroup(chat.ID, policy)
	p.replyFormatted(ctx, fmt.Sprintf("✅ 已更新本群 `%s` 配置", field))
	return nil
}

// handleGroupReset 处理 /ai group reset [field|all]。
func (p *Plugin) handleGroupReset(ctx *eventctx.Context, args []string) error {
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		ctx.ReplyText("❌ /ai group 仅限群聊场景使用")
		return nil
	}
	if !p.isGroupAdmin(ctx) {
		ctx.ReplyText("❌ 需要群管理员权限才能修改本群 AI 策略")
		return nil
	}

	field := "all"
	if len(args) > 0 {
		field = strings.ToLower(args[0])
	}
	switch field {
	case "all", "全部":
		p.groupPolicies.ResetGroup(chat.ID)
		ctx.ReplyText("✅ 已重置本群全部 AI 策略（回退到全局/默认）")
	case "prompt", "提示词":
		p.groupPolicies.ResetField(chat.ID, "prompt")
		ctx.ReplyText("✅ 已重置本群提示词")
	case "tools", "工具":
		p.groupPolicies.ResetField(chat.ID, "tools")
		ctx.ReplyText("✅ 已重置本群工具白名单")
	case "approval", "审批":
		p.groupPolicies.ResetField(chat.ID, "approval")
		ctx.ReplyText("✅ 已重置本群审批模式")
	case "mention", "触发":
		p.groupPolicies.ResetField(chat.ID, "mention")
		ctx.ReplyText("✅ 已重置本群 @ 触发要求")
	case "reply", "回复", "接话":
		p.groupPolicies.ResetField(chat.ID, "reply")
		ctx.ReplyText("✅ 已重置本群接话策略与概率")
	default:
		ctx.ReplyText("❌ 未知字段，可用：prompt / tools / approval / mention / all")
	}
	return nil
}

// handleGroupGlobal 处理 /ai group global <status|reset>（需 superadmin）。
func (p *Plugin) handleGroupGlobal(ctx *eventctx.Context, rest string) error {
	if !p.isSuperAdmin(ctx) {
		ctx.ReplyText("❌ 全局策略管理需要 superadmin 角色")
		return nil
	}
	parts := strings.Fields(strings.TrimSpace(strings.TrimLeft(rest, "@")))
	sub := "status"
	if len(parts) > 0 {
		sub = strings.ToLower(parts[0])
	}
	switch sub {
	case "status", "查看":
		gp := p.groupPolicies.Get(globalGroupID)
		if gp == nil || gp.Empty() {
			ctx.ReplyText("📋 当前没有全局 AI 策略配置（全部群使用插件默认值）")
			return nil
		}
		var b strings.Builder
		b.WriteString("📋 **全局 AI 策略**\n\n")
		if prompt := gp.EffectiveSystemPrompt(); prompt != "" {
			fmt.Fprintf(&b, "  - **提示词**: %s\n", p.ellipsize(prompt, 60))
		}
		if gp.ToolPolicy != nil {
			fmt.Fprintf(&b, "  - **工具白名单**: `%s`\n", *gp.ToolPolicy)
		}
		if gp.Approval != nil {
			fmt.Fprintf(&b, "  - **审批模式**: `%s`\n", *gp.Approval)
		}
		if require, ok := gp.EffectiveRequireMention(); ok {
			mode := "自主发言"
			if require {
				mode = "必须 @ 触发"
			}
			fmt.Fprintf(&b, "  - **@ 触发**: %s\n", mode)
		}
		p.replyFormatted(ctx, b.String())
	case "reset", "重置", "clear":
		p.groupPolicies.ResetGroup(globalGroupID)
		ctx.ReplyText("✅ 已重置全局 AI 策略")
	default:
		p.replyFormatted(ctx, "❌ 用法：`"+p.cfg.TriggerCmd+" group global status|reset`")
	}
	return nil
}

// parseGroupReplyArgs 解析 /ai group set reply 的参数：
//
//	all            — 每条都回
//	smart | judge  — 智能接话（概率沿用全局）
//	judge 0.5      — 指定模式与概率
//	0.5            — 简写：smart + 概率 0.5
//
// 返回 (策略, 概率(可 nil), 错误)。
func parseGroupReplyArgs(args []string) (*string, *float64, error) {
	mode := strings.ToLower(strings.TrimSpace(args[0]))
	var chance *float64
	if len(args) > 1 {
		v, err := strconv.ParseFloat(strings.TrimSpace(args[1]), 64)
		if err != nil || v < 0 || v > 1 {
			return nil, nil, errInvalidReplyArgs
		}
		chance = new(v)
	} else if p, err := strconv.ParseFloat(mode, 64); err == nil {
		// 纯概率简写：默认套在 smart 上。
		if p < 0 || p > 1 {
			return nil, nil, errInvalidReplyArgs
		}
		mode = config.GroupReplySmart
		chance = new(p)
	}
	if !config.ValidGroupReplyPolicy(mode) {
		return nil, nil, errInvalidReplyArgs
	}
	return new(mode), chance, nil
}

// errInvalidReplyArgs reply 参数非法的回显。
var errInvalidReplyArgs = errors.New("❌ reply 取值：all / smart / judge，可带 0~1 概率（如 `reply smart 0.5`）")

// isSuperAdmin 判断调用者是否为 superadmin 角色。
func (p *Plugin) isSuperAdmin(ctx *eventctx.Context) bool {
	pm := ctx.GetPermissionManager()
	if pm == nil {
		return false
	}
	return slices.Contains(pm.GetUserRoles(ctx.GetUserID()), "superadmin")
}

// groupCommandHelp 返回 /ai group 帮助文本。
func (a *adminState) groupCommandHelp(triggerCmd string) string {
	if triggerCmd == "" {
		triggerCmd = "/ai"
	}
	return fmt.Sprintf(`📋 **群 AI 策略管理**

  `+"`%s group status`"+`                          — 查看本群生效配置
  `+"`%s group set prompt <文本>`"+`              — 设置群提示词
  `+"`%s group set tools all|none|工具名,工具名`"+` — 设置群工具白名单
  `+"`%s group set approval off|restricted|always`"+` — 设置群审批模式
  `+"`%s group set mention on|off`"+`            — 设置群 @ 触发要求
  `+"`%s group set reply all|smart`"+`            — 设置接话策略（smart=智能接话）
  `+"`%s group reset [prompt|tools|approval|mention|reply|all]`"+` — 重置群配置
  `+"`%s group global status|reset`"+`           — 管理全局默认（需 superadmin）

权限：群配置需群管理员；全局配置需 superadmin`, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd, triggerCmd)
}

// ellipsize 截断长文本用于展示。
func (a *adminState) ellipsize(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
