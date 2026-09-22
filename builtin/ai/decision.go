// Package ai decision.go — 回合动作决策的装配门面：候选发现与调用策略评估。
//
// 本文件把"本轮该把哪些动作交给模型"收敛到一处，但只做装配：
//
//	actionCandidates     有哪些动作可用（注册表 + 用户 Skill，经群策略与 RBAC 收敛）
//	        ↓
//	decideTurnActions    相关度检索与选择 + 稳定策略（委托 builtin/ai/decision）
//
// 打分、选择与会话级稳定策略是纯算法，见 builtin/ai/decision；它们的候选来源
// （注册表、群策略、RBAC）与观测端口由本侧注入。
//
// 并把"这次调用是否放行"也收敛到一处：
//
//	decideToolInvocation 策略评估（RBAC 权限 + 审批），得出放行、拒绝文案与 SendTo 授权
//
// 决策只回答"发给模型什么"与"这次调用是否放行"，从不执行动作本身：
// 执行路径只消费结论，不重新推导策略。审批交互（提示发给谁、等多久）读的是
// 配置与回复能力，只在装配点可得，因此留在本侧。
package ai

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// actionCandidates 收集本轮可见的动作候选（管线视图，不含执行载荷）。
//
// 顺序即语义：先取注册表与用户 Skill（动作全集），再按群策略收敛出本群
// 可用边界，最后按 RBAC 剔除当前调用者无权调用的动作——声明了 Permissions
// 但无权的动作不进入模型视野（避免占名额与"调用了才被告知无权"）。
// 执行路径的权限校验保留为纵深防御。
func (p *Plugin) actionCandidates(ctx *eventctx.Context, session *session.Session) []toolkit.Action {
	actions := p.reg.Actions()
	actions = append(actions, p.userSkillActions(session.UserID)...)

	// per-group 工具白名单过滤（/ai group set tools）
	if gp := p.groupPolicyFor(ctx); gp != nil {
		before := len(actions)
		actions = gp.FilterTools(actions)
		if len(actions) != before {
			logger.Debugf("[AI] Group policy filtered tools: %d→%d", before, len(actions))
		}
	}

	beforePerm := len(actions)
	actions = p.filterToolsByPermission(ctx, actions)
	if len(actions) != beforePerm {
		logger.Debugf("[AI] Permission filtered tools: %d→%d", beforePerm, len(actions))
	}
	return actions
}

// decideTurnActions 决定本轮交给模型的工具集。
//
// needAction 为 false 时只保留"无需主动动作也应留下"的部分：默认保留级别
// （Mandatory/Baseline）、会话已用工具，再叠加稳定集合（sticky）——不挑选
// 可选动作，也不读写选择缓存（避免把收敛后的集合缓存成后续回合的复用结果）。
// 这保证闸门不会让工具集在"有/无动作"之间来回抖动（前缀缓存的稳定性前提）。
//
// 选择与稳定策略的实现见 builtin/ai/decision，本方法只交出插件持有的会话。
func (p *Plugin) decideTurnActions(ctx *eventctx.Context, session *session.Session, needAction bool) []toolkit.Action {
	candidates := p.actionCandidates(ctx, session)
	if !needAction {
		return p.stabilizeToolSet(session, candidates,
			decision.RetainedActions(candidates, decision.SessionUsedTools(session)))
	}
	return p.selectToolsForTurn(ctx, session, candidates)
}

// selectToolsForTurn 从可用工具中选择本轮发送给 LLM 的子集。
// 打分、会话缓存与稳定策略见 builtin/ai/decision；本方法只交出插件持有的配置、
// 嵌入器、会话与本轮查询（最后一条用户消息）。
func (p *Plugin) selectToolsForTurn(ctx *eventctx.Context, session *session.Session, actions []toolkit.Action) []toolkit.Action {
	return decision.SelectToolsForTurn(p.cfg, p.emb, ctx, session, runtime.LastUserMessage(session), actions, recordToolSet)
}

// stabilizeToolSet 在检索候选之上应用会话级稳定策略。
// 实现见 builtin/ai/decision；recordToolSet 作为观测端口注入，指标名与标签不变。
func (p *Plugin) stabilizeToolSet(session *session.Session, available, candidate []toolkit.Action) []toolkit.Action {
	return decision.StabilizeToolSet(p.cfg, session, available, candidate, recordToolSet)
}

// needAction 判断本轮是否需要主动寻找并执行新的动作。
//
// 判定只看"是否需要新的外部观测或外部副作用"：上下文注入（历史、群聊窗口、
// 长期记忆、相关历史、运行时上下文）属检索，不计入动作，因此它们不会把
// 本判定推成真——否则记忆一开，判定恒真，闸门就失去意义。
//
// 当前实现保守地恒为真，即不抑制可选动作，保持既有工具集行为。
// 收紧判定（例如纯闲聊/纯知识问答直接判否）会改变发给模型的工具集，
// 属行为变更，需单独评估后实施（见 docs/notes/28-ai-layering-progress.md）。
func (p *Plugin) needAction() bool {
	return true
}

// ── 单次调用的策略评估 ───────────────────────────────────────────────
//
// 策略的声明在 ActionPolicy（见 action.go）：是否需要审批、是否始终审批、所需权限。
// 这里只做评估：结合生效审批模式与调用者身份，判定这次调用是否放行。
// 执行路径拿到的是结论，不再自行推导。

// toolInvocationDecision 是单次工具调用的策略评估结论。
//
// allowed 为 false 时按拒绝处理：rejectText 是回填给模型的拒绝文案；
// err 非空表示这是一次失败（计入重试预算），为空表示这是一次正常的"不许执行"
// （例如用户拒绝审批，刻意不消耗重试预算）。
// sendToGranted 表示本次调用已通过审批门、获得 SendTo 授权；嵌套 Skill 内的
// 工具调用不经此处，拿到的 sender 未授权，仍受 SendTo 门控保护。
type toolInvocationDecision struct {
	rejectText    string
	err           error
	allowed       bool
	sendToGranted bool
}

// decideToolInvocation 是单次工具调用的唯一策略评估点。
//
// 顺序即语义：
//  1. RBAC：动作声明 Permissions 时先校验调用者权限，避免"先请求审批后告知无权"；
//     执行路径内的同类校验保留为纵深防御（双保险）。
//  2. 审批：按生效审批模式判断是否需要用户批准，需要时发起审批；拒绝或超时按拒绝处理。
//
// 两种拒绝都以工具级结果返回，不中断整个对话。
func (p *Plugin) decideToolInvocation(ctx *eventctx.Context, tc protocol.ToolCall) toolInvocationDecision {
	if action, ok := p.reg.Action(tc.Name); ok {
		if perms := action.Policy.Permissions; len(perms) > 0 && !p.hasToolPermission(ctx, perms) {
			return toolInvocationDecision{
				rejectText: fmt.Sprintf("错误: 工具 %q 需要权限（%s），当前用户无权调用",
					tc.Name, strings.Join(perms, ", ")),
				err: fmt.Errorf("tool %q requires permission %s", tc.Name, strings.Join(perms, ", ")),
			}
		}
	}
	if !p.approvalModeFor(ctx, tc.Name) {
		return toolInvocationDecision{allowed: true}
	}
	if !p.requestApproval(ctx, tc.Name, p.approvalSummaryForTool(ctx, tc), runtime.EffectiveApprovalTimeout(p.cfg)) {
		// 审批被拒不消耗重试预算：模型无需"重试"一个用户已拒绝的动作，
		// 与既有行为一致（此处刻意不置 err）。
		return toolInvocationDecision{rejectText: fmt.Sprintf("工具 `%s` 已被用户拒绝执行（审批未通过）", tc.Name)}
	}
	return toolInvocationDecision{allowed: true, sendToGranted: true}
}

// needsApproval 判断指定动作在给定审批模式下是否需要用户批准。
func (p *Plugin) needsApproval(a toolkit.Action, mode string) bool {
	switch mode {
	case "always":
		return true
	case "restricted":
		return a.Policy.RequiresApproval
	default:
		return false
	}
}

// approvalModeFor 判断指定工具在当前生效审批模式下是否需要审批。
func (p *Plugin) approvalModeFor(ctx *eventctx.Context, toolName string) bool {
	mode := p.effectiveApprovalMode(ctx)
	if mode == "" || mode == string(ApprovalOff) {
		// AlwaysRequireApproval 工具（如 send_to）不受 off 模式豁免，强制审批。
		if action, ok := p.reg.Action(toolName); ok && action.Policy.AlwaysRequireApproval {
			return true
		}
		return false
	}
	action, ok := p.reg.Action(toolName)
	if !ok {
		// 工具不存在（如 Skill）时：always 模式审批，restricted 不审批
		return mode == string(ApprovalAlways)
	}
	return p.needsApproval(action, mode)
}
