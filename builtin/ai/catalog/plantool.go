// plantool.go — 任务规划动作集（create_plan / update_plan_step）。
//
// 计划的数据结构、快照与存取属于会话状态（builtin/ai/session），本文件只负责
// 动作的形状与参数校验。读写计划经消费方端口 [PlanAccess]：端口由装配侧在
// 调用前注入 context，因此本包不反向依赖插件实例，也不决定计划何时被使用。
//
// 触发策略：两个动作注册为 general 类别（恒被选中），由模型按需调用——
// 简单任务（1-2 步）不创建计划零开销；复杂任务先建计划再逐步执行。
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// 内置计划动作名。
const (
	// PlanCreateToolName 创建计划的动作名。
	PlanCreateToolName = "create_plan"
	// PlanUpdateToolName 更新计划步骤的动作名。
	PlanUpdateToolName = "update_plan_step"
)

// validPlanStatuses 合法步骤状态（update_plan_step 的 status 枚举）。
var validPlanStatuses = []string{
	string(session.PlanInProgress), string(session.PlanDone), string(session.PlanFailed),
}

// PlanAccess 计划状态的读写端口。
//
// 暴露完成规划动作所需的操作：原子更新、读快照、重置后台自动推进预算。
// 方法名与会话状态（builtin/ai/session）已有的计划 API 一致，因此装配侧注入
// 会话本身即可，无需再套一层适配。
//
// 更新必须走 UpdatePlan（锁内原子快照-修改-写回）：并行工具调用下
// "读快照→副本改→SetPlan 写回" 会互相覆盖，先完成的步骤状态被静默回滚。
type PlanAccess interface {
	// UpdatePlan 在锁内原子更新计划：mutate 收到计划深拷贝，返回 nil 时写回。
	// 返回值为更新后的计划快照；没有进行中的计划时返回 (nil, session.ErrNoPlan)。
	UpdatePlan(mutate func(*session.Plan) error) (*session.Plan, error)
	// SetPlan 整体写入计划（仅 create_plan 用于建立新计划）。
	SetPlan(plan *session.Plan)
	// PlanSnapshot 返回当前计划的快照（无计划时返回 nil）。
	PlanSnapshot() *session.Plan
	// ResetPlanAuto 重置后台自动推进预算（用户侧重新获得完整轮次）。
	ResetPlanAuto()
}

// ctxKeyPlanAccess 是 context 中存储计划端口的键。
type ctxKeyPlanAccess struct{}

// WithPlanAccess 将计划端口注入 context，供规划动作的 Execute 使用。
func WithPlanAccess(ctx context.Context, plans PlanAccess) context.Context {
	return context.WithValue(ctx, ctxKeyPlanAccess{}, plans)
}

// planAccessFromContext 从 context 提取计划端口（未注入返回 nil）。
func planAccessFromContext(ctx context.Context) PlanAccess {
	plans, _ := ctx.Value(ctxKeyPlanAccess{}).(PlanAccess)
	return plans
}

// BuildPlanTools 构建规划动作集（general 类别，恒被选中）。
// maxSteps 为单个计划的最大步骤数（plan_max_steps，默认 8）。
func BuildPlanTools(maxSteps int) []toolkit.Tool {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	return []toolkit.Tool{
		{
			Name:        PlanCreateToolName,
			Description: "为复杂的多步任务创建执行计划。当任务需要 3 步以上操作时，先调用本工具制定计划（2-8 步），再按步骤执行",
			Categories:  []string{toolkit.CategoryGeneral},
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"task": {
						Type:        "string",
						Description: "任务描述（一句话概括用户需求）",
					},
					"steps": {
						Type:        "array",
						Items:       &protocol.ToolParamSchema{Type: "string", Description: "一个执行步骤"},
						Description: "执行步骤列表，每步一句话，2-8 步",
					},
				},
				Required: []string{"task", "steps"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				plans := planAccessFromContext(ctx)
				if plans == nil {
					return "错误: 计划上下文不可用", nil
				}
				task, _ := args["task"].(string)
				steps := trimBlank(stringSliceArg(args["steps"]))
				if len(steps) < 2 {
					return "错误: 计划至少需要 2 个步骤", nil
				}
				if len(steps) > maxSteps {
					return fmt.Sprintf("错误: 步骤数超过上限（最多 %d 步），请合并步骤", maxSteps), nil
				}
				plan := &session.Plan{Task: strings.TrimSpace(task), Active: true}
				for i, s := range steps {
					plan.Steps = append(plan.Steps, session.PlanStep{
						ID:          fmt.Sprintf("step_%d", i+1),
						Description: strings.TrimSpace(s),
						Status:      session.PlanPending,
					})
				}
				// 新计划重置后台自动推进预算（用户侧重新获得完整轮次）。
				plans.SetPlan(plan)
				plans.ResetPlanAuto()
				return "计划已创建：\n" + session.FormatPlan(plan), nil
			},
		},
		{
			Name:        PlanUpdateToolName,
			Description: "更新计划中某一步的状态（in_progress/done/failed），可附执行结果备注。每开始/完成/失败一步都调用一次",
			Categories:  []string{toolkit.CategoryGeneral},
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
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
				plans := planAccessFromContext(ctx)
				if plans == nil {
					return "错误: 计划上下文不可用", nil
				}
				stepID, _ := args["step_id"].(string)
				status, _ := args["status"].(string)
				note, _ := args["note"].(string)
				if status != string(session.PlanInProgress) &&
					status != string(session.PlanDone) && status != string(session.PlanFailed) {
					return "错误: 非法状态，可选 in_progress/done/failed", nil
				}
				newStatus := session.PlanStepStatus(status)

				// 顺序强制：目标步骤置任何非 pending 状态前，其前序步骤必须
				// 全部终态（done/failed）——计划按序执行，不允许跳步。
				plan, err := plans.UpdatePlan(func(plan *session.Plan) error {
					for i := range plan.Steps {
						if plan.Steps[i].ID != stepID {
							continue
						}
						for j := range i {
							if !session.TerminalStatus(plan.Steps[j]) {
								return fmt.Errorf("前序步骤 %s 尚未完成，请按计划顺序执行（先处理前序步骤）", plan.Steps[j].ID)
							}
						}
						plan.Steps[i].Status = newStatus
						if strings.TrimSpace(note) != "" {
							plan.Steps[i].Result = strings.TrimSpace(note)
						}
						if plan.Completed() {
							plan.Active = false
						}
						return nil
					}
					return errors.New("未找到步骤 " + stepID + "，请检查 create_plan 返回的计划")
				})
				if err != nil {
					if errors.Is(err, session.ErrNoPlan) {
						return "错误: 当前没有进行中的计划，请先调用 create_plan", nil
					}
					return "错误: " + err.Error(), nil
				}
				return "计划已更新：\n" + session.FormatPlan(plan), nil
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
