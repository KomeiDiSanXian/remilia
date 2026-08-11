// Package ai runner.go — 任务级执行模型（计划后台自动推进）。
//
// 本文件实现"自主 Agent"分界线上最核心的能力：计划创建后机器人无需用户
// 逐条消息推动，按 plan_auto_interval 间隔自动继续执行未完成步骤并主动汇报。
//
// 安全与约束：
//   - 默认关闭（plan_auto_continue）；用户发新消息时重置推进预算
//     （handleAIChat 调用 ResetPlanAuto），用户侧永远优先
//   - 用户回合进行中（TryLockTurn 失败）跳过本轮，不与用户并发抢话
//   - plan_auto_rounds（默认 3）限制单计划的后台推进轮次上限
//   - 无进度停止：连续两轮计划签名（步骤状态）不变即停止自动推进，
//     防止模型空转刷消息
//   - 复用原事件与平台 Sender 重建上下文回复（doSummary 同模式）；
//     生命周期 context 控制后台任务生命周期
package ai

import (
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// effectivePlanAutoRounds 返回计划后台推进轮次上限（<=0 用默认 3）。
func (p *Plugin) effectivePlanAutoRounds() int {
	if p.cfg.PlanAutoRounds <= 0 {
		return 3
	}
	return p.cfg.PlanAutoRounds
}

// maybeContinuePlan 在对话回合结束后检查是否需要安排计划后台推进：
// 计划存在、仍有未完成步骤、未达轮次上限、未被无进度停止。
func (p *Plugin) maybeContinuePlan(ctx *eventctx.Context, session *Session) {
	if !p.cfg.PlanAutoContinue || session == nil || p.lifecycleCtx == nil {
		return
	}
	plan := session.planSnapshot()
	if plan == nil || !plan.Active || !plan.hasPending() {
		return
	}
	if session.PlanAutoStopped() || session.PlanAutoRounds() >= p.effectivePlanAutoRounds() {
		return
	}
	evt := ctx.GetPlatformEvent()
	sender := ctx.GetPlatformSender()
	if evt == nil || sender == nil {
		return
	}
	p.schedulePlanContinue(session, evt, sender)
}

// schedulePlanContinue 递增轮次并调度一次后台推进（间隔后执行）。
func (p *Plugin) schedulePlanContinue(session *Session, evt platform.Event, sender platform.Sender) {
	if session.PlanAutoStopped() || session.PlanAutoRounds() >= p.effectivePlanAutoRounds() {
		return
	}
	session.BumpPlanAutoRounds()

	interval := p.cfg.PlanAutoInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	p.lifecycleSpawn(func() {
		select {
		case <-time.After(interval):
		case <-p.lifecycleCtx.Done():
			return
		}
		p.continuePlan(session, evt, sender)
	})
}

// continuePlan 执行一轮后台计划推进：
// 用户回合进行中跳过；无进度（计划签名不变）则停止自动推进；
// 完成后仍有未完成步骤则继续调度下一轮。
func (p *Plugin) continuePlan(session *Session, evt platform.Event, sender platform.Sender) {
	if !session.TryLockTurn() {
		// 用户回合进行中：跳过本轮（不消耗后续推进资格）。
		return
	}
	defer session.UnlockTurn()

	plan := session.planSnapshot()
	if plan == nil || !plan.Active || !plan.hasPending() {
		return
	}
	before := planSignature(plan)

	newCtx := eventctx.NewContextFromEvent(evt, sender)
	result, err := p.generateVerified(newCtx, session)
	if err != nil {
		logger.Debugf("[AI] Plan auto-continue round failed: %v", err)
		return
	}

	after := session.planSnapshot()
	if planSignature(after) == before {
		// 本轮无进度（模型未推进计划也未产出内容）→ 停止自动推进。
		session.StopPlanAuto()
		logger.Debugf("[AI] Plan auto-continue stopped: no progress")
		return
	}

	if result != nil && (result.Text != "" || len(result.Attachments) > 0) {
		msg := platform.OutboundMessage{}
		if p.cfg.Markdown {
			msg.Markdown = result.Text
		} else {
			msg.Text = result.Text
		}
		if len(result.Attachments) > 0 {
			msg.Attachments = result.Attachments
		}
		p.replyAndRecord(newCtx, msg)
	}

	if after != nil && after.Active && after.hasPending() && !session.PlanAutoStopped() {
		p.schedulePlanContinue(session, evt, sender)
	}
}
