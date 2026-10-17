// Package ai tasks.go — 任务记录的生命周期装配。
//
// 计划（session.Plan）是模型可修改的执行提案，任务（task.Task）是持久化的目标
// 记录。两者在计划动作的调用点同步（见 [taskPlanAccess]），本文件是这一同步的
// 唯一装配处：
//
//   - create_plan → 新建任务并接管当前计划；同一会话中已有的活动任务被置为
//     取消（其步骤与结果保留为历史，不被静默覆盖）；
//   - update_plan_step → 刷新任务步骤；计划全部终结时任务收敛为成功或失败；
//   - 取消（/ai plan cancel、/ai task cancel、/ai stop）→ 任务置 cancelled；
//   - /ai task → 查询任务；任务独立于会话缓存与会话记录，重启后仍可查询。
//
// 任务存储未装配（测试直接构造 Plugin）时所有操作退化为空实现，不影响对话路径。
package ai

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// taskStore 返回任务存储；未装配时退化为空实现（只读查询返回"无任务"，
// 写入静默丢弃），保证既有单元测试直接构造 Plugin 的路径不受影响。
func (p *Plugin) taskStore() task.Store {
	if p == nil || p.tasks == nil {
		return task.NoopStore{}
	}
	return p.tasks
}

// planAccessFor 构造本次工具调用可见的计划端口：读写委托会话，
// 任务记录在写路径上同步落库。
func (p *Plugin) planAccessFor(ctx *eventctx.Context, sess *session.Session) catalog.PlanAccess {
	return taskPlanAccess{p: p, ctx: ctx, sess: sess}
}

// taskPlanAccess 计划端口的装配实现（catalog.PlanAccess）。
type taskPlanAccess struct {
	p    *Plugin
	ctx  *eventctx.Context
	sess *session.Session
}

// SetPlan 建立新计划：先把会话中此前的活动任务置为取消（保留历史），
// 再为新计划创建任务。
func (a taskPlanAccess) SetPlan(plan *session.Plan) {
	a.p.taskMu.Lock()
	defer a.p.taskMu.Unlock()
	a.sess.SetPlan(plan)
	a.p.startTaskForPlan(a.ctx, a.sess, plan)
}

// UpdatePlan 原子更新会话计划，并把更新后的快照同步到活动任务。
func (a taskPlanAccess) UpdatePlan(mutate func(*session.Plan) error) (*session.Plan, error) {
	a.p.taskMu.Lock()
	defer a.p.taskMu.Unlock()
	plan, err := a.sess.UpdatePlan(mutate)
	if err != nil {
		return plan, err
	}
	a.p.syncActiveTask(a.ctx, a.sess, plan)
	return plan, nil
}

// PlanSnapshot 返回当前计划快照（委托会话）。
func (a taskPlanAccess) PlanSnapshot() *session.Plan { return a.sess.PlanSnapshot() }

// ResetPlanAuto 重置后台自动推进预算（委托会话）。
func (a taskPlanAccess) ResetPlanAuto() { a.sess.ResetPlanAuto() }

// startTaskForPlan 用新计划创建任务：取消旧活动任务 → 创建并启动新任务 → 落库。
func (p *Plugin) startTaskForPlan(ctx *eventctx.Context, sess *session.Session, plan *session.Plan) {
	store := p.taskStore()
	if old, err := store.ActiveBySession(sess.ID); err == nil {
		// 同一会话出现新计划 = 旧任务不再执行：取消而不是删除，
		// 已完成步骤与结果保留为可查询的历史。
		if cerr := old.Cancel(); cerr == nil {
			if serr := store.Save(old); serr != nil {
				logger.Errorf("[AI] Failed to persist superseded task %s: %v", old.ID, serr)
			}
		}
	} else if !errors.Is(err, task.ErrNotFound) {
		logger.Errorf("[AI] Failed to look up active task for session %s: %v", sess.ID, err)
	}

	t := task.New(plan.Task, sess.ID, ctx.GetSenderInfo().ID, ctx.GetChatInfo().ID, ctx.GetEventPlatform())
	if err := t.Start(); err != nil {
		logger.Errorf("[AI] Failed to start task %s: %v", t.ID, err)
		return
	}
	if err := t.SyncPlan(plan); err != nil {
		logger.Errorf("[AI] Failed to sync plan into task %s: %v", t.ID, err)
		return
	}
	if err := store.Save(t); err != nil {
		logger.Errorf("[AI] Failed to persist task %s: %v", t.ID, err)
	}
}

// syncActiveTask 把计划快照同步到会话的活动任务；没有活动任务时补建
// （覆盖旧版本升级后"有计划但无任务记录"的存量会话）。
func (p *Plugin) syncActiveTask(ctx *eventctx.Context, sess *session.Session, plan *session.Plan) {
	store := p.taskStore()
	t, err := store.ActiveBySession(sess.ID)
	switch {
	case errors.Is(err, task.ErrNotFound):
		t = task.New(plan.Task, sess.ID, ctx.GetSenderInfo().ID, ctx.GetChatInfo().ID, ctx.GetEventPlatform())
		if serr := t.Start(); serr != nil {
			logger.Errorf("[AI] Failed to start task %s: %v", t.ID, serr)
			return
		}
	case err != nil:
		logger.Errorf("[AI] Failed to look up active task for session %s: %v", sess.ID, err)
		return
	}
	if err := t.SyncPlan(plan); err != nil {
		// 终态任务拒绝刷新；这是预期内的竞态（取消与工具结果并发），不重复报错。
		logger.Debugf("[AI] Task %s not refreshed: %v", t.ID, err)
		return
	}
	if err := store.Save(t); err != nil {
		logger.Errorf("[AI] Failed to persist task %s: %v", t.ID, err)
	}
}

// cancelActiveTask 取消会话当前的活动任务，返回是否确实发生取消。
func (p *Plugin) cancelActiveTask(sess *session.Session) bool {
	if p == nil || sess == nil {
		return false
	}
	store := p.taskStore()
	p.taskMu.Lock()
	defer p.taskMu.Unlock()
	t, err := store.ActiveBySession(sess.ID)
	if err != nil {
		if !errors.Is(err, task.ErrNotFound) {
			logger.Errorf("[AI] Failed to look up active task for session %s: %v", sess.ID, err)
		}
		return false
	}
	if err := t.Cancel(); err != nil {
		logger.Debugf("[AI] Task %s cannot be cancelled: %v", t.ID, err)
		return false
	}
	if err := store.Save(t); err != nil {
		logger.Errorf("[AI] Failed to persist cancelled task %s: %v", t.ID, err)
	}
	return true
}

// --- /ai task ---

// handleTaskCommand 处理 /ai task 子命令（status / list / cancel）。
//
// 查询直接读任务存储而不是会话缓存：任务在会话被 LRU 淘汰或进程重启后
// 依然可查询，这是任务记录相对会话计划的核心价值。
func (p *Plugin) handleTaskCommand(ctx *eventctx.Context, rest string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := runtime.MakeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)

	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "", "status", "查看":
		t, err := p.taskStore().ActiveBySession(sessionID)
		if errors.Is(err, task.ErrNotFound) {
			p.replyFormatted(ctx, "📭 当前没有进行中的任务")
			return nil
		}
		if err != nil {
			logger.Errorf("[AI] Failed to query active task: %v", err)
			p.replyFormatted(ctx, "❌ 任务查询失败，请稍后再试")
			return nil
		}
		p.replyFormatted(ctx, formatTaskDetail(t))
		return nil
	case "list", "列表":
		list, err := p.taskStore().ListBySession(sessionID)
		if err != nil {
			logger.Errorf("[AI] Failed to list tasks: %v", err)
			p.replyFormatted(ctx, "❌ 任务查询失败，请稍后再试")
			return nil
		}
		p.replyFormatted(ctx, formatTaskList(list, p.cfg.TriggerCmd))
		return nil
	case "cancel", "取消":
		return p.cancelTaskCommand(ctx, sessionID)
	default:
		p.replyFormatted(ctx, p.taskHelpText(p.cfg.TriggerCmd))
		return nil
	}
}

// cancelTaskCommand 取消当前活动任务，并同步取消会话中的计划。
func (p *Plugin) cancelTaskCommand(ctx *eventctx.Context, sessionID string) error {
	sess := p.sm.GetOrCreate(sessionID, ctx.GetSenderInfo().ID, ctx.GetChatInfo().ID)
	if sess == nil {
		p.replyFormatted(ctx, "📭 当前没有进行中的任务可取消")
		return nil
	}
	planCancelled := sess.CancelPlan()
	if planCancelled {
		p.sm.SaveSession(sess)
	}
	taskCancelled := p.cancelActiveTask(sess)
	if !planCancelled && !taskCancelled {
		p.replyFormatted(ctx, "📭 当前没有进行中的任务可取消")
		return nil
	}
	p.replyFormatted(ctx, "🛑 已取消当前任务（剩余步骤不再自动推进）")
	return nil
}

// taskMaxListEntries 列表展示上限：更早的任务仍可通过任务 ID 查询，
// 但聊天里不无限刷屏。
const taskMaxListEntries = 10

// formatTaskDetail 格式化单个任务的详情。
func formatTaskDetail(t *task.Task) string {
	if t == nil {
		return "📭 当前没有进行中的任务"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 **任务** `%s`\n\n", t.ID)
	fmt.Fprintf(&b, "  状态：%s\n", taskStatusText(t.Status))
	if t.Goal != "" {
		fmt.Fprintf(&b, "  目标：%s\n", t.Goal)
	}
	fmt.Fprintf(&b, "  计划版本：v%d\n", t.PlanVersion)
	fmt.Fprintf(&b, "  创建：%s\n", t.CreatedAt.Format(time.DateTime))
	fmt.Fprintf(&b, "  更新：%s\n", t.UpdatedAt.Format(time.DateTime))
	for _, st := range t.Steps {
		fmt.Fprintf(&b, "  - [%s] %s (%s)", taskStepStatusText(st.Status), st.Description, st.ID)
		if st.Result != "" {
			fmt.Fprintf(&b, " 备注：%s", st.Result)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTaskList 格式化任务列表（最近创建的在前）。
func formatTaskList(list []*task.Task, triggerCmd string) string {
	if len(list) == 0 {
		return "📭 还没有任务记录"
	}
	var b strings.Builder
	b.WriteString("📋 **任务记录**\n")
	limit := min(len(list), taskMaxListEntries)
	for i := range limit {
		t := list[i]
		goal := t.Goal
		if goal == "" {
			goal = "（无目标描述）"
		}
		fmt.Fprintf(&b, "\n  `%s` [%s] %s（%s）",
			t.ID, taskStatusText(t.Status), goal, t.CreatedAt.Format("01-02 15:04"))
	}
	if len(list) > limit {
		fmt.Fprintf(&b, "\n\n（仅显示最近 %d 条，共 %d 条）", limit, len(list))
	}
	b.WriteString("\n\n用 `" + triggerCmd + " task` 查看当前任务详情。")
	return b.String()
}

// taskStatusText 任务状态的中文展示名。
func taskStatusText(s task.Status) string {
	switch s {
	case task.StatusPending:
		return "待执行"
	case task.StatusPlanning:
		return "规划中"
	case task.StatusReady:
		return "就绪"
	case task.StatusRunning:
		return "进行中"
	case task.StatusWaitingApproval:
		return "等待审批"
	case task.StatusWaitingInput:
		return "等待补充"
	case task.StatusPaused:
		return "已暂停"
	case task.StatusSucceeded:
		return "已完成"
	case task.StatusFailed:
		return "已失败"
	case task.StatusCancelled:
		return "已取消"
	case task.StatusInterrupted:
		return "已中断"
	default:
		return string(s)
	}
}

// taskStepStatusText 步骤状态的中文展示名。
func taskStepStatusText(s task.StepStatus) string {
	switch s {
	case task.StepPending:
		return "待执行"
	case task.StepReady:
		return "就绪"
	case task.StepRunning:
		return "进行中"
	case task.StepWaitingApproval:
		return "等待审批"
	case task.StepSucceeded:
		return "完成"
	case task.StepFailed:
		return "失败"
	case task.StepSkipped:
		return "跳过"
	case task.StepCancelled:
		return "取消"
	default:
		return string(s)
	}
}

// taskHelpText /ai task 的帮助文本。
func (a *adminState) taskHelpText(triggerCmd string) string {
	return fmt.Sprintf(`📋 **任务记录**

  `+"`%s task`"+`            — 查看当前任务进度
  `+"`%s task list`"+`       — 查看最近的任务记录
  `+"`%s task cancel`"+`     — 取消当前任务

任务记录随 storage 插件持久化，重启后仍可查询；复杂任务由 AI 自动创建并推进。`, triggerCmd, triggerCmd, triggerCmd)
}
