package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestTaskLifecycleMetrics 任务生命周期指标按目标状态计数：
// 创建、收敛、取消与重启恢复各产生一次对应增量。
func TestTaskLifecycleMetrics(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	create := planToolForTest(t, catalog.PlanCreateToolName)
	update := planToolForTest(t, catalog.PlanUpdateToolName)

	createdBefore := testutil.ToFloat64(taskCreated)
	runningBefore := counterValue(taskTransitions, string(task.StatusRunning))
	succeededBefore := counterValue(taskTransitions, string(task.StatusSucceeded))

	execPlanTool(t, create, pa, map[string]any{"task": "指标任务", "steps": []any{"第一步", "第二步"}})
	checkDelta(t, "tasks_created_total", createdBefore, testutil.ToFloat64(taskCreated), 1)
	checkDelta(t, "task_transitions_total{running}",
		runningBefore, counterValue(taskTransitions, string(task.StatusRunning)), 1)

	execPlanTool(t, update, pa, map[string]any{"step_id": "step_1", "status": "done"})
	execPlanTool(t, update, pa, map[string]any{"step_id": "step_2", "status": "done"})
	checkDelta(t, "task_transitions_total{succeeded}",
		succeededBefore, counterValue(taskTransitions, string(task.StatusSucceeded)), 1)

	cancelledBefore := counterValue(taskTransitions, string(task.StatusCancelled))
	execPlanTool(t, create, pa, map[string]any{"task": "取消任务", "steps": []any{"第一步", "第二步"}})
	if err := p.handlePlanCommand(ctx, "cancel"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	checkDelta(t, "task_transitions_total{cancelled}",
		cancelledBefore, counterValue(taskTransitions, string(task.StatusCancelled)), 1)

	interruptedBefore := counterValue(taskTransitions, string(task.StatusInterrupted))
	execPlanTool(t, create, pa, map[string]any{"task": "恢复任务", "steps": []any{"第一步", "第二步"}})
	p.recoverInterruptedTasks()
	checkDelta(t, "task_transitions_total{interrupted}",
		interruptedBefore, counterValue(taskTransitions, string(task.StatusInterrupted)), 1)
}
