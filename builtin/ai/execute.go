// Package ai execute.go — 工具与技能（Skill）的执行逻辑。
//
// 本文件包含工具调用的执行链路：
//   - executeToolResult: 工具调度总入口，先检查 Skill 再检查真实命令，最后回退到 Execute 回调
//   - captureSender（execution.CaptureSender）: 拦截 Sender 用于捕获 handler 输出
//   - executeSkill: Skill 内部工具调用循环（非流式）
//   - buildSkillTools: 构建 Skill 可见的工具列表（自有工具 + 其他 Skill）
//   - executeSkillTool: 执行 Skill 内部的工具调用
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
)

// executeToolResult 执行一个工具调用，返回结果文本与失败语义。
//
// toolCtx 是调用方传入的超时 context，用于限制工具执行的最长时间。
// cs 用于捕获 real command 执行过程中产生的消息附件。
// sender 非空时注入工具调用 context（含审批门控的 SendTo 能力）。
//
// 本函数负责"解析来源 + 施加策略闸门"，执行本身交给对应调用器
// （runtime.FuncInvoker / runtime.CommandInvoker / runtime.SkillInvoker）：
// 优先通过 vevent 触发真实命令并捕获其回复；若工具无对应命令，
// 回退到工具自身的 Execute 回调。
//
// Err 只在"调用本身失败"时非空：工具不存在、权限不足、回调返回错误。
// 工具正常返回、正文恰好以"错误:"开头，不算失败。
func (p *Plugin) executeToolResult(ctx *eventctx.Context, tc protocol.ToolCall, toolCtx context.Context, cs *execution.CaptureSender, sender toolkit.ToolSender) runtime.ActionResult {
	callerCtx := toolkit.WithCallerInfo(toolCtx, ctx.GetSenderInfo())
	if sender != nil {
		callerCtx = toolkit.WithToolSender(callerCtx, sender)
	}
	callerCtx = toolkit.WithToolInvocation(callerCtx, toolkit.ToolSource{
		UserID:   ctx.GetSenderInfo().ID,
		ChatID:   ctx.GetChatInfo().ID,
		Platform: ctx.GetEventPlatform(),
		IsGroup:  ctx.GetChatInfo().IsGroup,
	}, ctx.GetPlatformSender())
	callerCtx = catalog.WithCapabilities(callerCtx, p.toolCapabilities())
	// 执行路径的能力端口在同一装配点构造：调用器只依赖端口，不依赖插件实例。
	// 这两个端口刻意不进入 capabilitySet——工具回调不应获得"跑命令 / 跑技能"的能力。
	skills := pluginSkillRunner{p: p}
	commands := pluginCommandCatalog{p: p}
	if skill, ok := p.skillReg.GetByOwner(ctx.GetSenderInfo().ID, tc.Name); ok {
		return runtime.SkillInvoker{Name: tc.Name, Skill: skill, Args: tc.Arguments, Runner: skills}.Invoke(callerCtx)
	}
	if skill, ok := p.skillReg.GetSystem(tc.Name); ok {
		return runtime.SkillInvoker{Name: tc.Name, Skill: skill, Args: tc.Arguments, Runner: skills}.Invoke(callerCtx)
	}

	tool, ok := p.reg.Get(tc.Name)
	if !ok {
		return runtime.ActionResult{
			Text: fmt.Sprintf("错误: 未找到工具 %q", tc.Name),
			Err:  fmt.Errorf("tool %q not found", tc.Name),
		}
	}

	if p.syncer != nil {
		// 并行工具执行时串行化真实命令路径（syncer 非线程安全）。
		p.realCmdMu.Lock()
		result := runtime.CommandInvoker{Catalog: commands, Ctx: ctx, Name: tc.Name, Args: tc.Arguments, CS: cs}.Invoke(callerCtx)
		p.realCmdMu.Unlock()
		if result.Text != "" {
			return result
		}
	}

	// 工具级权限强制校验：工具声明了 Permissions 时，调用前校验调用者
	// RBAC 权限（任一命中即放行）。权限管理器缺失时拒绝（安全默认），
	// 不依赖插件自觉实现校验。
	if perms := toolkit.ActionPolicyOf(tool).Permissions; len(perms) > 0 && !p.hasToolPermission(ctx, perms) {
		return runtime.ActionResult{
			Text: fmt.Sprintf("错误: 工具 %q 需要权限（%s），当前用户无权调用",
				tc.Name, strings.Join(perms, ", ")),
			Err: fmt.Errorf("tool %q requires permission %s", tc.Name, strings.Join(perms, ", ")),
		}
	}

	return runtime.FuncInvoker{Name: tc.Name, Args: tc.Arguments, Fn: tool.Execute, Record: RecordToolCall}.Invoke(callerCtx)
}

// executeSkill 执行一个 Skill 的内部工具调用循环。
//
// 使用自己的 Prompt 和 Tools 做最多 SkillMaxDepth 轮的非流式 LLM 调用。
// 不持久化到 session，纯函数式。
func (p *Plugin) executeSkill(ctx context.Context, skill toolkit.Skill, args map[string]any) (string, error) {
	// 记账归属：以技能自身的 owner + name 为键，嵌套调用同样按被执行的技能计数。
	p.skillReg.IncrementUsage(skill.OwnerID, skill.Name)

	argsJSON, _ := json.MarshalIndent(args, "", "  ")
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: skill.Prompt},
		{Role: protocol.RoleUser, Content: string(argsJSON)},
	}
	tools := p.buildSkillTools(skill)
	// 子循环自己持有工具（要执行），发给模型的是其动作视图。
	actions := toolkit.ActionsOf(tools)

	skillTimeout := p.cfg.SkillTimeout
	if skillTimeout <= 0 {
		skillTimeout = 60 * time.Second
	}
	skillCtx, cancel := context.WithTimeout(ctx, skillTimeout)
	defer cancel()

	for depth := 0; depth < p.cfg.SkillMaxDepth; depth++ {
		resp, err := p.runtimeClient().SingleRound(skillCtx, p.cfg.Model, msgs, wireSpecs(actions))
		if err != nil {
			return "", err
		}

		if len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		msgs = append(msgs, protocol.Message{Role: protocol.RoleAssistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result := p.executeSkillTool(skillCtx, tc, tools)
			msgs = append(msgs, protocol.Message{Role: protocol.RoleTool, Content: runtime.TruncateToolResult(result), ToolCallID: tc.ID})
		}
	}

	return "", fmt.Errorf("技能 %q 超过最大调用深度 (%d)", skill.Name, p.cfg.SkillMaxDepth)
}

// buildSkillTools 构建 Skill 可见的工具列表 = 自己的 Tools + 其他系统 Skill。
// 用户 Skill 不可见其他用户的 Skill，仅系统 Skill 被注入。
// 其他 Skill 按其自带的 Parameters 注入，无参数时使用默认 {"query": string}。
func (p *Plugin) buildSkillTools(skill toolkit.Skill) []toolkit.Tool {
	sysSkills := p.skillReg.ListByOwner(toolkit.OwnerSystem)
	tools := make([]toolkit.Tool, 0, len(skill.Tools)+len(sysSkills))
	tools = append(tools, skill.Tools...)

	for _, s := range sysSkills {
		if s.Name == skill.Name {
			continue
		}
		other := s
		params := other.Parameters
		if len(params.Properties) == 0 {
			params = protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"query": {Type: "string", Description: "需要该技能处理的问题"},
				},
				Required: []string{"query"},
			}
		}
		tools = append(tools, toolkit.Tool{
			Name:        other.Name,
			Description: other.Description,
			Parameters:  params,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return p.executeSkill(ctx, other, args)
			},
		})
	}

	return tools
}

// executeSkillTool 执行 Skill 内部的工具调用。
// 不走 syncer/real command，直接调用工具自身的 Execute 回调。
func (p *Plugin) executeSkillTool(ctx context.Context, tc protocol.ToolCall, tools []toolkit.Tool) string {
	for _, t := range tools {
		if t.Name == tc.Name {
			result, err := t.Execute(ctx, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, err)
			}
			return result
		}
	}
	return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
}

// hasToolPermission 校验调用者是否拥有任一指定权限。
// 权限管理器缺失时返回 false（安全默认）。支持格式：
// "resource.action" / "resource:action" / "resource"（action 通配）。
func (c *catalogState) hasToolPermission(ctx *eventctx.Context, perms []string) bool {
	userID := ctx.GetUserID()
	if c.perms != nil {
		for _, perm := range perms {
			perm = strings.TrimSpace(perm)
			if perm == "" {
				continue
			}
			if c.perms.HasPermission(userID, perm) {
				return true
			}
		}
		return false
	}
	// 权限插件未接线时回退到上下文权限管理器（测试场景）；
	// 两者皆无时安全拒绝。
	pm := ctx.GetPermissionManager()
	if pm == nil {
		return false
	}
	for _, perm := range perms {
		perm = strings.TrimSpace(perm)
		if perm == "" {
			continue
		}
		resource, action := execution.ParseToolPermission(perm)
		if pm.HasPermission(userID, permission.Permission{Resource: resource, Action: action}) {
			return true
		}
	}
	return false
}

// filterToolsByPermission 从动作列表中剔除当前调用者无权调用的动作
// （声明了 Permissions 且校验不通过）。供 processWithTools 按角色注入使用。
func (c *catalogState) filterToolsByPermission(ctx *eventctx.Context, actions []toolkit.Action) []toolkit.Action {
	out := make([]toolkit.Action, 0, len(actions))
	for _, a := range actions {
		if len(a.Policy.Permissions) > 0 && !c.hasToolPermission(ctx, a.Policy.Permissions) {
			continue
		}
		out = append(out, a)
	}
	return out
}
