// execute.go — 工具与技能（Skill）的执行逻辑。
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

// executionTarget 一次工具调用的"执行三件套"：时间预算、输出捕获、调用期能力。
//
// 三者总是一起构造、一起传递，且类型各异（context / *CaptureSender /
// *Invocation）——作为相邻位置参数时，顺序写错编译器不会报错。收成结构体后，
// 调用点只表达"这一次执行的是什么"。
type executionTarget struct {
	// Ctx 工具调用的时间预算（从回合上下文派生，受 tool_timeout / skill_timeout 约束）。
	Ctx context.Context
	// CS 命令通道的输出捕获器（工具以真实命令执行时，其回复落在这里）。
	CS *execution.CaptureSender
	// Inv 调用期能力：调用者、授权器、发送/解析能力、能力端口。
	Inv *toolkit.Invocation
}

// newToolInvocation 构造一次工具调用的显式上下文。
//
// 调用者身份、授权器、发送/解析能力、审计来源与能力端口都按调用构造，不再经
// context 传递。plans 由装配侧按会话计算后传入；测试/无会话路径显式传 nil，
// 让"缺少计划端口"在调用点可见，而不是漏一次 SetPort。Invocation 只在本次
// 调用内有效，工具不得缓存它；技能内部循环复用同一份，并以
// [toolkit.Invocation.WithSkill] 追加调用链。
func (p *Plugin) newToolInvocation(ctx *eventctx.Context, sender toolkit.ToolSender, plans catalog.PlanAccess) *toolkit.Invocation {
	caller := ctx.GetSenderInfo()
	inv := &toolkit.Invocation{
		Caller:     caller,
		Authorizer: p.toolAuthorizer(ctx),
		Sender:     sender,
		Source: toolkit.ToolSource{
			UserID:   caller.ID,
			ChatID:   ctx.GetChatInfo().ID,
			Platform: ctx.GetEventPlatform(),
			IsGroup:  ctx.GetChatInfo().IsGroup,
		},
	}
	if sender != nil {
		// 目标解析只读、不受审批门控，供 send_message 的 @ 提及复用
		// send_to 的同一套解析规则。
		inv.Resolver = targetResolverAdapter{sender: sender}
	}
	// 进程执行端口外再加一层证据记录：计划完成闸门依赖"最近一次命令的退出码"。
	caps := p.toolCapabilities(ctx.GetPlatformSender())
	if runner := p.evidenceRecordingRunner(ctx, caps.Process); runner != nil {
		caps.Process = runner
	}
	// 工作区写入端口外加一层产物登记：任务报告需要"这次改了什么"。
	if recorder := p.fileArtifactRecordingWriter(ctx, caps.FileWrite); recorder != nil {
		caps.FileWrite = recorder
	}
	catalog.AttachCapabilities(inv, caps)
	catalog.AttachPlans(inv, plans)
	return inv
}

// executeToolResultWithSnapshot 执行一个工具调用，返回结果文本与失败语义。
//
// target 携带本次调用的执行三件套；snapshot 是调用前取得的工具定义快照，
// 必须与 decideToolInvocationWithSnapshot 收到的同一份——不一致会导致按旧定义
// 审批、按新定义执行。
//
// 本函数负责"解析来源 + 施加策略闸门"，执行本身交给对应调用器
// （runtime.FuncInvoker / runtime.CommandInvoker / runtime.SkillInvoker）：
// 优先通过 vevent 触发真实命令并捕获其回复；若工具无对应命令，
// 回退到工具自身的 Execute 回调。
//
// 来源判定是显式的：只有快照中登记了命令模式的动作才走命令通道，
// 命中后结果完全由命令通道负责，不再以"文本为空"回退到占位 Execute——命令
// 跑通但没输出会如实回填"未返回文本"，而不是冒充成功。
//
// Err 只在"调用本身失败"时非空：工具不存在、权限不足、回调/命令/Skill 返回错误。
// 工具正常返回、正文恰好以"错误:"开头，不算失败。
func (p *Plugin) executeToolResultWithSnapshot(ctx *eventctx.Context, tc protocol.ToolCall,
	target executionTarget, snapshot toolExecutionSnapshot) runtime.ActionResult {
	toolCtx, cs, inv := target.Ctx, target.CS, target.Inv
	// 执行路径的能力端口在同一装配点构造：调用器只依赖端口，不依赖插件实例。
	// 这两个端口刻意不进入 capabilitySet——工具回调不应获得"跑命令 / 跑技能"的能力。
	skills := pluginSkillRunner{p: p}
	commands := pluginCommandCatalog{p: p}
	if skill, ok := p.skillReg.GetByOwner(ctx.GetSenderInfo().ID, tc.Name); ok {
		return runtime.SkillInvoker{Name: tc.Name, Skill: skill, Args: tc.Arguments, Runner: skills, Inv: inv, Record: recordToolCall}.Invoke(toolCtx)
	}
	if skill, ok := p.skillReg.GetSystem(tc.Name); ok {
		return runtime.SkillInvoker{Name: tc.Name, Skill: skill, Args: tc.Arguments, Runner: skills, Inv: inv, Record: recordToolCall}.Invoke(toolCtx)
	}

	if !snapshot.found {
		return runtime.ActionResult{
			Text: fmt.Sprintf("错误: 未找到工具 %q", tc.Name),
			Err:  fmt.Errorf("tool %q not found", tc.Name),
		}
	}
	tool := snapshot.tool

	// 参数结构校验：模型给出的参数必须满足工具声明。失败按工具级结果回填，
	// 让模型按字段定位修正后重试；未声明参数的工具保持原有行为。
	// 校验只覆盖 ToolParamSchema 能表达的约束，外部来源的复杂 Schema 不被误拒。
	if err := protocol.ValidateArguments(tool.Parameters, tc.Arguments); err != nil {
		return runtime.ActionResult{
			Text: fmt.Sprintf("错误: 工具 %q 的参数不合法: %v；请按参数定义修正后重试", tc.Name, err),
			Err:  err,
		}
	}

	// 真实命令路径：来源判定在装配侧——只有确实登记了命令模式的动作才走合成
	// 事件重放，命中后结果完全由命令通道负责（包括"命令执行了但没有输出"），
	// 不再以"文本为空"回退到动作自身的占位 Execute 冒充成功。
	if snapshot.hasCommand {
		// 并行工具执行时串行化真实命令路径（syncer 非线程安全）。
		p.realCmdMu.Lock()
		// 每次调用使用独立的捕获器：共享捕获器的 CapturedText 只在非空时覆盖、
		// 从不重置，会让"本次命令无输出"错误回填上一次命令的旧文本。
		cmdCS := &execution.CaptureSender{}
		commands.commandPattern = snapshot.commandPattern
		commands.patternResolved = true
		result := runtime.CommandInvoker{
			Catalog: commands, Ctx: ctx, Name: tc.Name, Args: tc.Arguments,
			CS: cmdCS, Record: recordToolCall,
		}.Invoke(toolCtx)
		// 汇入回合级捕获器：文本保留为"模型无输出"时的兜底回复，附件随最终回复发送。
		cs.Merge(cmdCS)
		p.realCmdMu.Unlock()
		return result
	}

	// 工具级权限强制校验：工具声明了 Permissions 时，调用前校验调用者
	// RBAC 权限（任一命中即放行）。权限管理器缺失时拒绝（安全默认），
	// 不依赖插件自觉实现校验。
	if perms := tool.Permissions; len(perms) > 0 && !p.hasToolPermission(ctx, perms) {
		return runtime.ActionResult{
			Text: fmt.Sprintf("错误: 工具 %q 需要权限（%s），当前用户无权调用",
				tc.Name, strings.Join(perms, ", ")),
			Err: fmt.Errorf("tool %q requires permission %s", tc.Name, strings.Join(perms, ", ")),
		}
	}

	return runtime.FuncInvoker{Name: tc.Name, Args: tc.Arguments, Fn: tool.Execute, Inv: inv, Record: recordToolCall}.Invoke(toolCtx)
}

// executeSkill 执行一个 Skill 的内部工具调用循环。
//
// 使用自己的 Prompt 和 Tools 做最多 SkillMaxDepth 轮的非流式 LLM 调用。
// 不持久化到 session，纯函数式。
//
// 递归防护：调用栈由 [toolkit.Invocation.SkillPath] 显式携带。嵌套只重置
// 本技能内部轮数计数，两个互引的系统技能（A 注入 B、B 注入 A）会 A→B→A→…
// 无界嵌套直到 60s 超时兜底，期间每层用量记账 + token 白烧；已在链中的技能
// 直接拒绝执行，模型看到错误便改走其他路径。
func (p *Plugin) executeSkill(ctx context.Context, inv *toolkit.Invocation, skill toolkit.Skill, args map[string]any) (string, error) {
	if inv.InSkill(skill.Name) {
		return "", fmt.Errorf("技能 %q 递归调用被阻止：它已在当前调用链中，请检查技能对其他技能的引用", skill.Name)
	}
	// 记账归属：以技能自身的 owner + name 为键，嵌套调用同样按被执行的技能计数。
	p.skillReg.IncrementUsage(skill.OwnerID, skill.Name)

	argsJSON, _ := json.MarshalIndent(args, "", "  ")
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: skill.Prompt},
		{Role: protocol.RoleUser, Content: string(argsJSON)},
	}
	skillInv := inv.WithSkill(skill.Name)
	tools := p.buildSkillTools(ctx, skillInv, skill)
	// 子循环自己持有工具（要执行），发给模型的是其动作视图。
	actions := toolkit.ActionsOf(tools)

	skillTimeout := p.cfg.SkillTimeout
	if skillTimeout <= 0 {
		skillTimeout = 60 * time.Second
	}
	skillCtx, cancel := context.WithTimeout(ctx, skillTimeout)
	defer cancel()

	for depth := 0; depth < p.cfg.SkillMaxDepth; depth++ {
		resp, err := p.runtimeClient(protocol.CallKindSkill).SingleRound(skillCtx, p.cfg.Model, msgs, wireSpecs(actions))
		if err != nil {
			return "", err
		}

		if len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		msgs = append(msgs, protocol.Message{Role: protocol.RoleAssistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result := p.executeSkillTool(skillCtx, skillInv, tc, tools)
			msgs = append(msgs, protocol.Message{Role: protocol.RoleTool, Content: runtime.TruncateToolResult(result), ToolCallID: tc.ID})
		}
	}

	return "", fmt.Errorf("技能 %q 超过最大调用深度 (%d)", skill.Name, p.cfg.SkillMaxDepth)
}

// buildSkillTools 构建 Skill 可见的工具列表 = 自己的 Tools + 其他系统 Skill。
// 用户 Skill 不可见其他用户的 Skill，仅系统 Skill 被注入。
// 其他 Skill 按其自带的 Parameters 注入，无参数时使用默认 {"query": string}。
//
// 工具列表按调用者授权过滤（见 toolkit.Authorizer）：无权限或需要人工审批的
// 工具不会出现在模型视图里。没有授权器时退化为"过滤所有声明 Permissions 的
// 工具"（安全默认：无法验证身份时不给能力）。
func (p *Plugin) buildSkillTools(ctx context.Context, inv *toolkit.Invocation, skill toolkit.Skill) []toolkit.Tool {
	allowed := func(t toolkit.Tool) bool {
		ok, _ := inv.CanUse(t)
		return ok
	}

	sysSkills := p.skillReg.ListByOwner(toolkit.OwnerSystem)
	tools := make([]toolkit.Tool, 0, len(skill.Tools)+len(sysSkills))
	for _, t := range skill.Tools {
		if allowed(t) {
			tools = append(tools, t)
		}
	}

	for _, s := range sysSkills {
		if s.Name == skill.Name {
			continue
		}
		other := s
		if !allowed(toolkit.Tool{Name: other.Name}) {
			continue
		}
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
			Execute: func(ctx context.Context, inv *toolkit.Invocation, args map[string]any) (toolkit.ToolResult, error) {
				out, err := p.executeSkill(ctx, inv, other, args)
				return toolkit.TextResult(out), err
			},
		})
	}

	return tools
}

// executeSkillTool 执行 Skill 内部的工具调用。
// 不走 syncer/real command，直接调用工具自身的 Execute 回调。
// 执行前按调用者授权复核一次：注入阶段已过滤，这里防止绕过或并发注入变化。
func (p *Plugin) executeSkillTool(ctx context.Context, inv *toolkit.Invocation, tc protocol.ToolCall, tools []toolkit.Tool) string {
	for _, t := range tools {
		if t.Name == tc.Name {
			if allowed, reason := inv.CanUse(t); !allowed {
				return "错误: " + reason
			}
			result, err := t.ExecuteText(ctx, inv, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, err)
			}
			return result
		}
	}
	return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
}

// toolAuthorizer 按事件上下文构造技能内部工具授权器。
//
// 判断与顶层工具调用保持一致：RBAC 权限（Permissions）+ 审批模式。技能内部
// 是模型自循环，无法等待人工审批，因此需要审批的工具一律拒绝，并提示模型
// 直接调用该工具（走正常审批路径）。
func (p *Plugin) toolAuthorizer(ctx *eventctx.Context) toolkit.Authorizer {
	return toolkit.AuthorizerFunc(func(t toolkit.Tool) (bool, string) {
		if len(t.Permissions) > 0 && !p.hasToolPermission(ctx, t.Permissions) {
			return false, fmt.Sprintf("工具 %q 需要权限：%s，当前用户无权在技能内使用",
				t.Name, strings.Join(t.Permissions, ", "))
		}
		if p.approvalModeFor(ctx, t.Name) {
			return false, fmt.Sprintf("工具 %q 需要人工审批，技能内部不支持；请直接调用该工具", t.Name)
		}
		return true, ""
	})
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
