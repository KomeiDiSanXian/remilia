// plan.go — 任务计划的数据结构与会话存取。
//
// 计划本身是纯数据（任务 + 步骤 + 状态）；"何时创建、如何推进、失败后如何重规划"
// 属于运行时策略，见 builtin/ai 的计划工具。
package session

import (
	"fmt"
	"strings"
)

// PlanStepStatus 计划步骤状态。
type PlanStepStatus string

const (
	// PlanPending 待执行。
	PlanPending PlanStepStatus = "pending"
	// PlanInProgress 执行中。
	PlanInProgress PlanStepStatus = "in_progress"
	// PlanDone 已完成。
	PlanDone PlanStepStatus = "done"
	// PlanFailed 已失败。
	PlanFailed PlanStepStatus = "failed"
)

// PlanStep 计划中的一个步骤。
type PlanStep struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Status      PlanStepStatus `json:"status"`
	Result      string         `json:"result,omitempty"`
}

// Plan 会话中正在执行的任务计划。
type Plan struct {
	// Task 任务描述。
	Task string `json:"task"`
	// Steps 步骤列表。
	Steps []PlanStep `json:"steps"`
	// Active 是否仍在进行（全部完成/失败后置 false）。
	Active bool `json:"active"`
}

// ClonePlan 深拷贝计划（避免锁外修改污染）。
func ClonePlan(p *Plan) *Plan {
	if p == nil {
		return nil
	}
	cp := &Plan{Task: p.Task, Active: p.Active, Steps: make([]PlanStep, len(p.Steps))}
	copy(cp.Steps, p.Steps)
	return cp
}

// Completed 判断计划是否全部结束（每步 done 或 failed）。
func (p *Plan) Completed() bool {
	if p == nil {
		return true
	}
	for _, s := range p.Steps {
		if s.Status != PlanDone && s.Status != PlanFailed {
			return false
		}
	}
	return len(p.Steps) > 0
}

// HasPending 判断是否存在尚未结束（done/failed）的步骤。
func (p *Plan) HasPending() bool {
	if p == nil {
		return false
	}
	for _, s := range p.Steps {
		if s.Status != PlanDone && s.Status != PlanFailed {
			return true
		}
	}
	return false
}

// terminal 判断步骤是否为终态（done/failed）。
func TerminalStatus(s PlanStep) bool {
	return s.Status == PlanDone || s.Status == PlanFailed
}

// FirstFailedFrontier 返回"前序步骤全部终态、自身 failed"的首个步骤
// （重规划触发点）；不存在返回 nil。
func (p *Plan) FirstFailedFrontier() *PlanStep {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Status == PlanFailed {
			blocked := false
			for j := range i {
				if !TerminalStatus(p.Steps[j]) {
					blocked = true
					break
				}
			}
			if !blocked {
				return &p.Steps[i]
			}
		}
	}
	return nil
}

// --- Session 上的计划存取 ---

// SetPlan 写入会话计划（线程安全，存储副本）。
func (s *Session) SetPlan(p *Plan) {
	s.Lock()
	defer s.Unlock()
	s.plan = ClonePlan(p)
}

// PlanSnapshot 返回会话计划副本（无计划返回 nil）。
func (s *Session) PlanSnapshot() *Plan {
	s.Lock()
	defer s.Unlock()
	return ClonePlan(s.plan)
}

// CancelPlan 取消当前计划（标记为非活跃：停止注入与自动推进），
// 返回是否存在可取消的进行中计划。
func (s *Session) CancelPlan() bool {
	s.Lock()
	defer s.Unlock()
	if s.plan == nil || !s.plan.Active {
		return false
	}
	s.plan.Active = false
	return true
}

// PlanText 返回当前计划的注入文本（无进行中的计划返回空串）。
func (s *Session) PlanText() string {
	s.Lock()
	defer s.Unlock()
	if s.plan == nil || !s.plan.Active {
		return ""
	}
	return FormatPlan(s.plan)
}

// FormatPlan 格式化计划为 Markdown 文本（供注入与展示）。
func FormatPlan(p *Plan) string {
	if p == nil || len(p.Steps) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "任务：%s\n", p.Task)
	for _, st := range p.Steps {
		var mark string
		switch st.Status {
		case PlanInProgress:
			mark = "[进行中]"
		case PlanDone:
			mark = "[完成]"
		case PlanFailed:
			mark = "[失败]"
		default:
			mark = "[待执行]"
		}
		fmt.Fprintf(&b, "%s %s (%s)", mark, st.Description, st.ID)
		if st.Result != "" {
			fmt.Fprintf(&b, " 备注：%s", st.Result)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
