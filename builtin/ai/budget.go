// budget.go — 任务级预算的运行时强制执行。
//
// 每个会话的活动任务有工具调用次数与执行时长两个预算
// （配置 task_max_tool_calls / task_max_duration）。预算在工具调用前检查并
// 累加：超限时任务置为失败、计划取消，本次调用被拒绝并返回原因。模型只能在
// 剩余预算内提出动作，无法自行提高上限。
//
// 计数是会话级、进程内状态：新计划开始时重置；重启后从零开始（重启恢复本身
// 会把在途任务置为 interrupted，不会绕过恢复语义）。

package ai

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// taskUsage 单个会话的任务预算计数。
type taskUsage struct {
	// ToolCalls 已消耗的工具调用次数。
	ToolCalls int
	// StartedAt 本轮任务的起始时间（新计划时重置）。
	StartedAt time.Time
}

// chargeTaskBudget 记为会话消耗一次工具调用，返回超限原因（未超限为空串）。
//
// 计数与判定在锁内完成；终结动作（任务失败、计划取消）由调用方在锁外执行，
// 避免与任务记录锁形成反向加锁顺序。
func (p *Plugin) chargeTaskBudget(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	p.taskUsageMu.Lock()
	defer p.taskUsageMu.Unlock()
	if p.taskUsage == nil {
		p.taskUsage = make(map[string]*taskUsage)
	}
	usage := p.taskUsage[sess.ID]
	if usage == nil {
		usage = &taskUsage{StartedAt: time.Now()}
		p.taskUsage[sess.ID] = usage
	}
	usage.ToolCalls++

	maxCalls := 0
	maxDuration := time.Duration(0)
	if p.cfg != nil {
		maxCalls = p.cfg.TaskMaxToolCalls
		maxDuration = p.cfg.TaskMaxDuration
	}
	if maxCalls > 0 && usage.ToolCalls > maxCalls {
		return fmt.Sprintf("工具调用次数超过上限（%d 次）", maxCalls)
	}
	if maxDuration > 0 && time.Since(usage.StartedAt) > maxDuration {
		return fmt.Sprintf("执行时长超过上限（%s）", maxDuration)
	}
	return ""
}

// resetTaskUsage 重置会话的任务预算计数（新计划开始时调用）。
func (p *Plugin) resetTaskUsage(sessionID string) {
	p.taskUsageMu.Lock()
	defer p.taskUsageMu.Unlock()
	delete(p.taskUsage, sessionID)
}

// pruneSessionState 清理过期的会话级运行时状态（执行证据与预算计数）。
//
// 这两张表按会话键累积，长跑进程里会随会话数量无界增长；会话 TTL 之后它们
// 已无意义（重启恢复与闸门都不依赖过期会话的旧证据），由插件的周期清理调用。
func (p *Plugin) pruneSessionState(cutoff time.Time) int {
	removed := 0

	p.evidenceMu.Lock()
	for sessionID, evidence := range p.execEvidence {
		if evidence.At.Before(cutoff) {
			delete(p.execEvidence, sessionID)
			removed++
		}
	}
	p.evidenceMu.Unlock()

	p.taskUsageMu.Lock()
	for sessionID, usage := range p.taskUsage {
		if usage.StartedAt.Before(cutoff) {
			delete(p.taskUsage, sessionID)
			removed++
		}
	}
	p.taskUsageMu.Unlock()

	return removed
}

// terminateTaskForBudget 预算超限的终结动作：活动任务置为失败并取消会话计划。
// 终态任务不受影响（迟到结果不能覆盖已终结的任务）。
func (p *Plugin) terminateTaskForBudget(sess *session.Session, reason string) {
	if sess == nil {
		return
	}
	p.taskMu.Lock()
	store := p.taskStore()
	if t, err := store.ActiveBySession(sess.ID); err == nil {
		if terr := t.Transition(task.StatusFailed); terr == nil {
			recordTaskStatus(t.Status)
			if serr := store.Save(t); serr != nil {
				logger.Errorf("[AI] Failed to persist budget-failed task %s: %v", t.ID, serr)
			}
		}
	}
	p.taskMu.Unlock()

	if sess.CancelPlan() && p.sm != nil {
		p.sm.SaveSession(sess)
	}
	logger.Warnf("[AI] Task budget exceeded, task terminated: %s", reason)
}
