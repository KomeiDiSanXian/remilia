// testentrypoints_test.go — 仅供测试使用的便捷入口。
//
// 这些方法都只是"补上生产路径里由编排层现算的那一个参数"（目录代数、执行快照、
// 计划端口），本身不含任何逻辑。放在生产文件里时，它们看起来像是又一个可被
// 业务调用的 API（例如"不带代数的工具选择"），实际上业务侧从不这样调用——
// 生产路径的代数与快照来自同一次目录读取，拆开会让缓存判定与执行视图不一致。
//
// 归属测试文件后：生产代码只剩单一入口，测试仍在同包内直接调用，一行不改。

package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// selectToolsForTurn 用当前目录代数做一次工具选择。
func (p *Plugin) selectToolsForTurn(ctx *eventctx.Context, sess *session.Session, actions []toolkit.Action) []toolkit.Action {
	return p.selectToolsForTurnAtGeneration(ctx, sess, actions, p.catalogGeneration())
}

// stabilizeToolSet 用当前目录代数应用一次会话级稳定策略。
func (p *Plugin) stabilizeToolSet(sess *session.Session, available, candidate []toolkit.Action) []toolkit.Action {
	return p.stabilizeToolSetAtGeneration(sess, available, candidate, p.catalogGeneration())
}

// actionCandidates 只取候选动作，不要代数。
func (p *Plugin) actionCandidates(ctx *eventctx.Context, sess *session.Session) []toolkit.Action {
	_, actions := p.actionCandidatesWithGeneration(ctx, sess)
	return actions
}

// approvalSummaryForTool 按工具名查定义后生成审批摘要。
func (p *Plugin) approvalSummaryForTool(ctx *eventctx.Context, tc protocol.ToolCall) string {
	action, ok := p.reg.Action(tc.Name)
	return p.approvalSummaryForAction(ctx, tc, action, ok)
}

// decideToolInvocation 用此刻的执行快照做一次调用决策。
func (p *Plugin) decideToolInvocation(ctx *eventctx.Context, interruptCtx context.Context, tc protocol.ToolCall) toolInvocationDecision {
	return p.decideToolInvocationWithSnapshot(ctx, interruptCtx, tc, p.executionToolSnapshot(tc.Name))
}

// executeToolResult 无可会话入口：不注入计划端口。
func (p *Plugin) executeToolResult(ctx *eventctx.Context, tc protocol.ToolCall, toolCtx context.Context, cs *execution.CaptureSender, sender toolkit.ToolSender) runtime.ActionResult {
	return p.executeToolResultWithSnapshot(ctx, tc, executionTarget{
		Ctx: toolCtx,
		CS:  cs,
		Inv: p.newToolInvocation(ctx, sender, nil),
	}, p.executionToolSnapshot(tc.Name))
}
