// runner.go — 任务级执行模型（计划后台自动推进）。
//
// 本文件实现"自主 Agent"分界线上最核心的能力：计划创建后机器人无需用户
// 逐条消息推动，按 plan_auto_interval 间隔自动继续执行未完成步骤并主动汇报。
//
// 安全与约束：
//   - 默认关闭（plan_auto_continue）；用户发新消息时重置推进预算
//     （handleAIChat 调用 ResetPlanAuto），用户侧永远优先
//   - 用户回合进行中（TryLockTurn 失败）跳过本轮，不与用户并发抢话
//   - plan_auto_rounds（默认 3）限制单计划的后台推进轮次上限
//   - 后台轮也走完整回合流程（BeginTurn）：接上中断信号，/ai stop 与
//     用户新消息抢占同样能中止后台推进
//   - 无进度停止：连续两轮计划签名（步骤状态）不变即停止自动推进，
//     防止模型空转刷消息；停止前仍发送本轮已产出的内容
//   - 复用原事件的 ctx.Clone() 重建可发送上下文（Clone 携带 dispatcher，
//     Reply 才能真正投递；裸 NewContextFromEvent 没有 dispatcher，
//     回复会被静默吞掉）；生命周期 context 控制后台任务生命周期

package ai

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// effectivePlanAutoRounds 返回计划后台推进轮次上限（<=0 用默认 3）。
// maybeContinuePlan 在对话回合结束后检查是否需要安排计划后台推进：
// 计划存在、仍有未完成步骤、未达轮次上限、未被无进度停止。
func (p *Plugin) maybeContinuePlan(ctx *eventctx.Context, sess *session.Session) {
	if !p.cfg.PlanAutoContinue || sess == nil || p.lifecycleCtx == nil {
		return
	}
	plan := sess.PlanSnapshot()
	if plan == nil || !plan.Active || !plan.HasPending() {
		return
	}
	if sess.PlanAutoStopped() || sess.PlanAutoRounds() >= runtime.EffectivePlanAutoRounds(runtimeLimits(p.cfg)) {
		return
	}
	// 克隆当前上下文供后台使用：Clone 复制 dispatcher 与扩展/botID，
	// 且 std ctx 独立于事件池回收，后台任务安全；裸 NewContextFromEvent
	// 缺 dispatcher，ctx.Reply 会以 ErrNoPlatformSender 静默失败。
	clone := ctx.Clone()
	evt := clone.GetPlatformEvent()
	sender := clone.GetPlatformSender()
	if evt == nil || sender == nil {
		return
	}
	p.schedulePlanContinue(sess, evt, sender, clone)
}

// schedulePlanContinue 递增轮次并调度一次后台推进（间隔后执行）。
func (p *Plugin) schedulePlanContinue(sess *session.Session, evt platform.Event, sender platform.Sender, replyCtx *eventctx.Context) {
	if sess.PlanAutoStopped() || sess.PlanAutoRounds() >= runtime.EffectivePlanAutoRounds(runtimeLimits(p.cfg)) {
		return
	}
	sess.BumpPlanAutoRounds()

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
		p.continuePlan(sess, evt, sender, replyCtx)
	})
}

// continuePlan 执行一轮后台计划推进：
// 用户回合进行中跳过；本轮作为完整回合执行（BeginTurn 接上中断信号，
// /ai stop 与用户新消息抢占同样中止后台轮）；无进度（计划签名不变）
// 则停止自动推进；完成后仍有未完成步骤则继续调度下一轮。
func (p *Plugin) continuePlan(sess *session.Session, evt platform.Event, sender platform.Sender, replyCtx *eventctx.Context) {
	if !sess.TryLockTurn() {
		// 用户回合进行中：跳过本轮（不消耗后续推进资格）。
		return
	}
	defer sess.UnlockTurn()

	// 接上回合中断信号：signal 存在后 TurnCtx/RequestInterrupt/Interrupted
	// 才有效（否则 /ai stop 判定回合不活跃，无法中止后台轮）。
	if !sess.BeginTurn() {
		return
	}
	defer sess.EndTurn()

	plan := sess.PlanSnapshot()
	if plan == nil || !plan.Active || !plan.HasPending() {
		return
	}

	result, err := p.generateVerified(replyCtx, sess)
	if err != nil {
		if sess.Interrupted() {
			// 被停止/抢占：静默结束，不再推进也不再补发错误提示。
			return
		}
		logger.Debugf("[AI] Plan auto-continue round failed: %v", err)
		p.replyAndRecord(replyCtx, platform.TextMessage("⚠️ 后台计划推进失败，已暂停自动推进："+runtime.FormatAIError(err)))
		return
	}

	// 先把本轮产出发出去，再做"无进度"判定——模型没推进计划但产出了
	// 有价值的回复文本时，丢弃它等于让一整轮 LLM/工具工作凭空蒸发。
	if result != nil && (result.Text != "" || len(result.Attachments) > 0) {
		msg := p.formatReplyMessage(result.Text)
		if len(result.Attachments) > 0 {
			msg.Attachments = result.Attachments
		}
		p.replyAndRecord(replyCtx, msg)
	}

	after := sess.PlanSnapshot()
	if runtime.PlanSignature(after) == runtime.PlanSignature(plan) {
		// 本轮无进度（计划签名不变）→ 停止自动推进。
		if !sess.Interrupted() {
			sess.StopPlanAuto()
			logger.Debugf("[AI] Plan auto-continue stopped: no progress")
		}
		return
	}

	if after != nil && after.Active && after.HasPending() && !sess.PlanAutoStopped() {
		p.schedulePlanContinue(sess, evt, sender, replyCtx)
	}
}
