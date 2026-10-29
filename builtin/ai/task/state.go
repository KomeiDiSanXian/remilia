// state.go — 任务与步骤状态机：哪些状态存在、哪些迁移合法。
//
// 迁移表即契约：新增状态必须同时补齐出边，并由 state_test.go 的全覆盖用例
// 守护。终态没有出边——任何"复活"都是非法迁移。

package task

import (
	"fmt"
	"slices"
)

// taskTransitions 任务状态的合法出边。
//
// 约定：
//   - 终态（succeeded/failed/cancelled）没有出边；
//   - 活动状态都可以迁移到 cancelled（取消随时可达）与 failed；
//   - interrupted 是运行时可恢复状态，只允许回到 running、收敛为终态或取消；
//   - 同状态自迁移非法（调用方应先判定再迁移，保持调用点显式）。
var taskTransitions = map[Status][]Status{
	StatusPending: {
		StatusPlanning, StatusReady, StatusRunning,
		StatusWaitingInput, StatusSucceeded, StatusFailed, StatusCancelled,
	},
	StatusPlanning: {
		StatusReady, StatusRunning, StatusWaitingInput, StatusPaused,
		StatusSucceeded, StatusFailed, StatusCancelled, StatusInterrupted,
	},
	StatusReady: {
		StatusRunning, StatusPaused, StatusSucceeded, StatusFailed, StatusCancelled,
	},
	StatusRunning: {
		StatusWaitingApproval, StatusWaitingInput, StatusPaused,
		StatusSucceeded, StatusFailed, StatusInterrupted, StatusCancelled,
	},
	StatusWaitingApproval: {
		StatusRunning, StatusWaitingInput, StatusPaused,
		StatusSucceeded, StatusFailed, StatusInterrupted, StatusCancelled,
	},
	StatusWaitingInput: {
		StatusPlanning, StatusReady, StatusRunning,
		StatusSucceeded, StatusFailed, StatusInterrupted, StatusCancelled,
	},
	StatusPaused: {
		StatusReady, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled,
	},
	StatusInterrupted: {
		StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled,
	},
	StatusSucceeded: nil,
	StatusFailed:    nil,
	StatusCancelled: nil,
}

// allStatuses 已定义任务状态的完整集合，供校验与测试使用。
var allStatuses = []Status{
	StatusPending, StatusPlanning, StatusReady, StatusRunning,
	StatusWaitingApproval, StatusWaitingInput, StatusPaused,
	StatusSucceeded, StatusFailed, StatusCancelled, StatusInterrupted,
}

// CanTransition 报告任务状态迁移 from → to 是否合法。
func CanTransition(from, to Status) bool {
	return slices.Contains(taskTransitions[from], to)
}

// IsTerminal 报告任务状态是否为终态。
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Valid 报告状态是否为已定义状态。
func (s Status) Valid() bool {
	return slices.Contains(allStatuses, s)
}

// ParseStatus 解析持久化记录中的状态字符串。
func ParseStatus(raw string) (Status, error) {
	s := Status(raw)
	if !s.Valid() {
		return "", fmt.Errorf("task: unknown status %q", raw)
	}
	return s, nil
}

// allStepStatuses 已定义步骤状态的完整集合。
var allStepStatuses = []StepStatus{
	StepPending, StepReady, StepRunning, StepWaitingApproval,
	StepSucceeded, StepFailed, StepSkipped, StepCancelled,
}

// IsTerminal 报告步骤是否已终结（完成、失败、跳过或取消）。
func (s StepStatus) IsTerminal() bool {
	switch s {
	case StepSucceeded, StepFailed, StepSkipped, StepCancelled:
		return true
	default:
		return false
	}
}

// Valid 报告步骤状态是否为已定义状态。
func (s StepStatus) Valid() bool {
	return slices.Contains(allStepStatuses, s)
}
