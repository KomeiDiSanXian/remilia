// Package task 承载任务的领域模型与持久化端口。
//
// 任务（Task）是"用户想要达成的目标"的持久化记录，独立于产生它的对话回合；
// 计划（Plan）由会话持有（builtin/ai/session），是可被模型修改的执行提案。
// 任务记录通过 [Task.SyncPlan] 从计划刷新，并额外负责：
//
//   - 身份与归属：任务 ID、所属会话、发起者与聊天上下文；
//   - 生命周期：状态机与合法迁移（见 state.go）；
//   - 计划版本：计划内容变化时递增 [Task.PlanVersion]，纯进度更新不递增；
//   - 终态保护：进入 succeeded / failed / cancelled 后拒绝任何刷新，
//     迟到的工具结果不能把已取消的任务改回成功。
//
// 本包只承载领域模型与存储端口，不含 LLM 调用、能力选择与执行策略；
// 装配与运行逻辑在 builtin/ai 侧。设计背景见 docs/notes/31-ai-agent.md。
package task

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
)

// 领域错误。
var (
	// ErrNilPlan 传入空计划。
	ErrNilPlan = errors.New("task: nil plan")
	// ErrIllegalTransition 非法的状态迁移。
	ErrIllegalTransition = errors.New("task: illegal status transition")
	// ErrTaskTerminal 任务已处于终态，拒绝任何状态或计划刷新。
	ErrTaskTerminal = errors.New("task: task is in a terminal status")
)

// Status 任务生命周期状态。
type Status string

// 任务状态。取值与含义见 docs/notes/31-ai-agent.md 的任务状态机。
const (
	// StatusPending 已创建，尚未开始。
	StatusPending Status = "pending"
	// StatusPlanning 正在分析目标或生成计划。
	StatusPlanning Status = "planning"
	// StatusReady 具备执行条件。
	StatusReady Status = "ready"
	// StatusRunning 正在执行。
	StatusRunning Status = "running"
	// StatusWaitingApproval 等待人工审批。
	StatusWaitingApproval Status = "waiting_approval"
	// StatusWaitingInput 等待用户补充信息。
	StatusWaitingInput Status = "waiting_input"
	// StatusPaused 已暂停。
	StatusPaused Status = "paused"
	// StatusSucceeded 目标已达成。
	StatusSucceeded Status = "succeeded"
	// StatusFailed 任务失败。
	StatusFailed Status = "failed"
	// StatusCancelled 任务已取消。
	StatusCancelled Status = "cancelled"
	// StatusInterrupted 执行中断，等待恢复策略处理。
	StatusInterrupted Status = "interrupted"
)

// StepStatus 任务步骤状态。
type StepStatus string

// 步骤状态。当前由计划步骤映射产生的只有 pending/running/succeeded/failed；
// 其余取值供后续引入依赖调度与等待态时使用。
const (
	// StepPending 尚未开始。
	StepPending StepStatus = "pending"
	// StepReady 依赖已满足，可执行。
	StepReady StepStatus = "ready"
	// StepRunning 正在执行。
	StepRunning StepStatus = "running"
	// StepWaitingApproval 等待审批。
	StepWaitingApproval StepStatus = "waiting_approval"
	// StepSucceeded 已完成。
	StepSucceeded StepStatus = "succeeded"
	// StepFailed 已失败。
	StepFailed StepStatus = "failed"
	// StepSkipped 已跳过。
	StepSkipped StepStatus = "skipped"
	// StepCancelled 已取消。
	StepCancelled StepStatus = "cancelled"
)

// Step 任务中的一个可追踪步骤。
type Step struct {
	// ID 步骤标识（沿用计划步骤 ID，如 step_1）。
	ID string `json:"id"`
	// Description 步骤描述。
	Description string `json:"description"`
	// Status 步骤状态。
	Status StepStatus `json:"status"`
	// Result 步骤执行结果备注（可选）。
	Result string `json:"result,omitempty"`
}

// Task 一个持久化的任务实例。
type Task struct {
	// ID 任务唯一标识。
	ID string `json:"id"`
	// SessionID 产生任务的会话标识（platform:chatID:userID）。
	SessionID string `json:"session_id"`
	// OwnerID 任务发起者。
	OwnerID string `json:"owner_id"`
	// ChatID 会话上下文中的聊天标识。
	ChatID string `json:"chat_id"`
	// Platform 事件平台名。
	Platform string `json:"platform"`
	// Goal 用户目标（会话计划的任务描述）。
	Goal string `json:"goal"`
	// Status 任务状态。
	Status Status `json:"status"`
	// PlanVersion 计划内容修订号：任务描述或步骤集合变化时递增。
	PlanVersion uint64 `json:"plan_version"`
	// Steps 当前步骤快照（最新一次计划刷新）。
	Steps []Step `json:"steps,omitempty"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 最后一次变更时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// New 创建一个待执行任务（状态 pending）。ID 与时间戳由本函数生成。
func New(goal, sessionID, ownerID, chatID, platform string) *Task {
	now := time.Now()
	return &Task{
		ID:        NewID(now),
		SessionID: sessionID,
		OwnerID:   ownerID,
		ChatID:    chatID,
		Platform:  platform,
		Goal:      strings.TrimSpace(goal),
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewID 生成任务 ID：时间戳（毫秒）加随机后缀。
//
// 时间戳让 ID 可按创建顺序粗略排序，便于人工排查；随机后缀避免同一毫秒内
// 创建的任务撞号。随机源不可用时退化为纯时间戳，仍保证毫秒级唯一。
func NewID(now time.Time) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t-%d", now.UnixMilli())
	}
	return fmt.Sprintf("t-%d-%s", now.UnixMilli(), hex.EncodeToString(b[:]))
}

// Clone 深拷贝任务，避免调用方修改存储或快照中的切片。
func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}
	cp := *t
	cp.Steps = make([]Step, len(t.Steps))
	copy(cp.Steps, t.Steps)
	return &cp
}

// Active 报告任务是否处于活动（非终态）状态。
func (t *Task) Active() bool { return t != nil && !t.Status.IsTerminal() }

// Completed 报告任务的全部步骤是否已终结。没有步骤时返回 false：
// 空计划不构成"完成"，避免误判。
func (t *Task) Completed() bool {
	if t == nil || len(t.Steps) == 0 {
		return false
	}
	for i := range t.Steps {
		if !t.Steps[i].Status.IsTerminal() {
			return false
		}
	}
	return true
}

// HasFailedSteps 报告是否存在失败步骤。
func (t *Task) HasFailedSteps() bool {
	if t == nil {
		return false
	}
	for i := range t.Steps {
		if t.Steps[i].Status == StepFailed {
			return true
		}
	}
	return false
}

// Start 将任务置为执行中。已在执行中时幂等；终态时返回 [ErrTaskTerminal]。
func (t *Task) Start() error {
	if t.Status == StatusRunning {
		return nil
	}
	if t.Status.IsTerminal() {
		return ErrTaskTerminal
	}
	return t.Transition(StatusRunning)
}

// Cancel 取消任务。已取消时幂等；已完成或已失败的任务不可取消，返回 [ErrTaskTerminal]。
func (t *Task) Cancel() error {
	if t.Status == StatusCancelled {
		return nil
	}
	if t.Status.IsTerminal() {
		return ErrTaskTerminal
	}
	return t.Transition(StatusCancelled)
}

// Transition 校验并应用一次任务状态迁移；不合法时返回 [ErrIllegalTransition]。
func (t *Task) Transition(to Status) error {
	if !CanTransition(t.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, t.Status, to)
	}
	t.Status = to
	t.UpdatedAt = time.Now()
	return nil
}

// SyncPlan 用会话计划刷新任务：更新目标、步骤与计划版本；计划全部终结时
// 收敛任务状态（无失败步骤为 succeeded，否则为 failed）。
//
// 终态任务拒绝刷新并返回 [ErrTaskTerminal]——这是"取消后迟到的工具结果
// 不得覆盖状态"的落点。
func (t *Task) SyncPlan(plan *session.Plan) error {
	if plan == nil {
		return ErrNilPlan
	}
	if t.Status.IsTerminal() {
		return ErrTaskTerminal
	}
	if goal := strings.TrimSpace(plan.Task); goal != "" {
		t.Goal = goal
	}
	steps := StepsFromPlan(plan)
	if planContentChanged(t.Steps, steps) {
		t.PlanVersion++
	}
	t.Steps = steps
	t.UpdatedAt = time.Now()
	if t.Completed() {
		return t.settle()
	}
	// 恢复后的任务在下一次计划推进时回到执行中：interrupted 表示"进程退出时
	// 仍在执行"，不是失败；有新的进度即说明执行已继续。
	if t.Status == StatusInterrupted {
		return t.Transition(StatusRunning)
	}
	return nil
}

// settle 依据步骤终态收敛任务状态。
func (t *Task) settle() error {
	to := StatusSucceeded
	if t.HasFailedSteps() {
		to = StatusFailed
	}
	if t.Status == to {
		return nil
	}
	return t.Transition(to)
}

// StepsFromPlan 把会话计划步骤映射为任务步骤。
//
// 状态映射：pending→pending、in_progress→running、done→succeeded、failed→failed；
// 未识别的状态按 pending 处理（持久化数据跨版本时的保守降级）。
// 计划侧的顺序与依赖校验由规划动作负责，任务记录只如实映射当前快照。
func StepsFromPlan(plan *session.Plan) []Step {
	if plan == nil {
		return nil
	}
	steps := make([]Step, 0, len(plan.Steps))
	for _, ps := range plan.Steps {
		steps = append(steps, Step{
			ID:          ps.ID,
			Description: ps.Description,
			Status:      stepStatusFromPlan(ps.Status),
			Result:      ps.Result,
		})
	}
	return steps
}

// stepStatusFromPlan 映射单个计划步骤状态。
func stepStatusFromPlan(s session.PlanStepStatus) StepStatus {
	switch s {
	case session.PlanInProgress:
		return StepRunning
	case session.PlanDone:
		return StepSucceeded
	case session.PlanFailed:
		return StepFailed
	default:
		return StepPending
	}
}

// planContentChanged 报告步骤集合是否发生了内容变化（数量、标识或描述）。
// 仅状态与备注变化不算新版本：那是执行进度，不是重新规划。
func planContentChanged(prev, next []Step) bool {
	if len(prev) != len(next) {
		return true
	}
	for i := range prev {
		if prev[i].ID != next[i].ID || prev[i].Description != next[i].Description {
			return true
		}
	}
	return false
}
