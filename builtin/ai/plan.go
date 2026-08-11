// Package ai plan.go — 任务规划层（Plan 工具）。
//
// 本文件实现显式的任务规划能力：
//   - Plan / PlanStep：会话内的任务计划数据结构（json:"-" 不持久化）
//   - create_plan：为复杂任务创建执行计划（2-N 步），存入会话
//   - update_plan_step：更新某一步状态（进行中/完成/失败），可附备注
//   - 计划注入：每轮 LLM 调用前把"当前计划+进度"作为 system 消息注入，
//     模型有状态可依，不再靠隐式记忆推进多步任务
//   - 用户可见：创建计划时同步把计划文本发送给用户（chat 内展示）
//
// 触发策略：两个工具注册为 general 类别（恒被选中），由模型按需调用——
// 简单任务（1-2 步）不创建计划零开销；复杂任务先建计划再逐步执行。
package ai

import (
	"context"
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

// validPlanStatuses 合法步骤状态（update_plan_step 的 status 枚举）。
var validPlanStatuses = []string{
	string(PlanInProgress), string(PlanDone), string(PlanFailed),
}

// 内置计划工具名。
const (
	planCreateToolName = "create_plan"
	planUpdateToolName = "update_plan_step"
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

// clonePlan 深拷贝计划（避免锁外修改污染）。
func clonePlan(p *Plan) *Plan {
	if p == nil {
		return nil
	}
	cp := &Plan{Task: p.Task, Active: p.Active, Steps: make([]PlanStep, len(p.Steps))}
	copy(cp.Steps, p.Steps)
	return cp
}

// completed 判断计划是否全部结束（每步 done 或 failed）。
func (p *Plan) completed() bool {
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

// hasPending 判断是否存在尚未结束（done/failed）的步骤。
func (p *Plan) hasPending() bool {
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
func terminalStatus(s PlanStep) bool {
	return s.Status == PlanDone || s.Status == PlanFailed
}

// firstFailedFrontier 返回"前序步骤全部终态、自身 failed"的首个步骤
// （重规划触发点）；不存在返回 nil。
func (p *Plan) firstFailedFrontier() *PlanStep {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Status == PlanFailed {
			blocked := false
			for j := range i {
				if !terminalStatus(p.Steps[j]) {
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

// planSignature 生成计划进度签名（无进度检测：连续两轮签名不变则停止自动推进）。
func planSignature(p *Plan) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.Task)
	for _, s := range p.Steps {
		b.WriteString("|")
		b.WriteString(s.ID)
		b.WriteString("=")
		b.WriteString(string(s.Status))
	}
	return b.String()
}

// --- Session 上的计划存取 ---

// setPlan 写入会话计划（线程安全，存储副本）。
func (s *Session) setPlan(p *Plan) {
	s.Lock()
	defer s.Unlock()
	s.plan = clonePlan(p)
}

// planSnapshot 返回会话计划副本（无计划返回 nil）。
func (s *Session) planSnapshot() *Plan {
	s.Lock()
	defer s.Unlock()
	return clonePlan(s.plan)
}

// planText 返回当前计划的注入文本（无进行中的计划返回空串）。
func (s *Session) planText() string {
	s.Lock()
	defer s.Unlock()
	if s.plan == nil || !s.plan.Active {
		return ""
	}
	return formatPlan(s.plan)
}

// formatPlan 格式化计划为 Markdown 文本（供注入与展示）。
func formatPlan(p *Plan) string {
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

// --- context 传递 session（供内置工具 Execute 访问） ---

// ctxKeyPlanSession 是 context 中存储计划会话的键。
type ctxKeyPlanSession struct{}

// WithPlanSession 将会话注入 context，供 create_plan/update_plan_step 的 Execute 使用。
func WithPlanSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, ctxKeyPlanSession{}, session)
}

// planSessionFromContext 从 context 提取计划会话（未注入返回 nil）。
func planSessionFromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(ctxKeyPlanSession{}).(*Session)
	return s
}

// buildReplanMessage 构建"步骤失败 → 系统级重规划"指令。
// 由 processWithTools 在检测到前序终态的前沿失败步骤时自动追加，
// 要求模型重新规划剩余步骤（而非自行猜测下一步）。
func buildReplanMessage(step *PlanStep) Message {
	return Message{
		Role: RoleUser,
		Content: fmt.Sprintf(
			"计划步骤 `%s`（%s）已标记失败。请重新评估剩余步骤：可以调用 create_plan 调整计划（跳过/替换失败步骤），或直接告知用户无法完成并说明原因。不要继续执行基于旧计划的后续步骤。",
			step.ID, step.Description),
	}
}

// --- 内置计划工具 ---

// buildPlanTools 构建规划层内置工具（general 类别，恒被选中）。
// maxSteps 为单个计划的最大步骤数（plan_max_steps，默认 8）。
func buildPlanTools(maxSteps int) []Tool {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	return []Tool{
		{
			Name:        planCreateToolName,
			Description: "为复杂的多步任务创建执行计划。当任务需要 3 步以上操作时，先调用本工具制定计划（2-8 步），再按步骤执行",
			Categories:  []string{CategoryGeneral},
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"task": {
						Type:        "string",
						Description: "任务描述（一句话概括用户需求）",
					},
					"steps": {
						Type:        "array",
						Items:       &ToolParamSchema{Type: "string", Description: "一个执行步骤"},
						Description: "执行步骤列表，每步一句话，2-8 步",
					},
				},
				Required: []string{"task", "steps"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				session := planSessionFromContext(ctx)
				if session == nil {
					return "错误: 计划上下文不可用", nil
				}
				task, _ := args["task"].(string)
				steps := stringSliceArg(args["steps"])
				steps = trimBlank(steps)
				if len(steps) < 2 {
					return "错误: 计划至少需要 2 个步骤", nil
				}
				if len(steps) > maxSteps {
					return fmt.Sprintf("错误: 步骤数超过上限（最多 %d 步），请合并步骤", maxSteps), nil
				}
				plan := &Plan{Task: strings.TrimSpace(task), Active: true}
				for i, s := range steps {
					plan.Steps = append(plan.Steps, PlanStep{
						ID:          fmt.Sprintf("step_%d", i+1),
						Description: strings.TrimSpace(s),
						Status:      PlanPending,
					})
				}
				session.setPlan(plan)
				// 新计划重置后台自动推进预算（用户侧重新获得完整轮次）。
				session.ResetPlanAuto()
				return "计划已创建：\n" + formatPlan(plan), nil
			},
		},
		{
			Name:        planUpdateToolName,
			Description: "更新计划中某一步的状态（in_progress/done/failed），可附执行结果备注。每开始/完成/失败一步都调用一次",
			Categories:  []string{CategoryGeneral},
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"step_id": {
						Type:        "string",
						Description: "步骤 ID（如 step_1），由 create_plan 返回",
					},
					"status": {
						Type:        "string",
						Enum:        validPlanStatuses,
						Description: "新状态：in_progress=开始执行 / done=完成 / failed=失败",
					},
					"note": {
						Type:        "string",
						Description: "执行结果备注（可选），如查到的关键数据或失败原因",
					},
				},
				Required: []string{"step_id", "status"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				session := planSessionFromContext(ctx)
				if session == nil {
					return "错误: 计划上下文不可用", nil
				}
				plan := session.planSnapshot()
				if plan == nil || !plan.Active {
					return "错误: 当前没有进行中的计划，请先调用 create_plan", nil
				}
				stepID, _ := args["step_id"].(string)
				status, _ := args["status"].(string)
				note, _ := args["note"].(string)

				// 顺序强制：目标步骤置任何非 pending 状态前，其前序步骤必须
				// 全部终态（done/failed）——计划按序执行，不允许跳步。
				found := false
				for i := range plan.Steps {
					if plan.Steps[i].ID == stepID {
						found = true
						for j := range i {
							if !terminalStatus(plan.Steps[j]) {
								return "错误: 前序步骤 " + plan.Steps[j].ID + " 尚未完成，请按计划顺序执行（先处理前序步骤）", nil
							}
						}
						switch status {
						case string(PlanInProgress), string(PlanDone), string(PlanFailed):
							plan.Steps[i].Status = PlanStepStatus(status)
						default:
							return "错误: 非法状态，可选 in_progress/done/failed", nil
						}
						if strings.TrimSpace(note) != "" {
							plan.Steps[i].Result = strings.TrimSpace(note)
						}
						break
					}
				}
				if !found {
					return "错误: 未找到步骤 " + stepID + "，请检查 create_plan 返回的计划", nil
				}
				if plan.completed() {
					plan.Active = false
				}
				session.setPlan(plan)
				return "计划已更新：\n" + formatPlan(plan), nil
			},
		},
	}
}

// stringSliceArg 将参数值转换为字符串切片（兼容 []any / []string）。
func stringSliceArg(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// trimBlank 去除空字符串项。
func trimBlank(items []string) []string {
	out := items[:0]
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
