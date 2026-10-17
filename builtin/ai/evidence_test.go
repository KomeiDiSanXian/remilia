package ai

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
)

func TestExecEvidenceRecordAndClear(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)

	if _, ok := p.lastExecEvidence("synthetic:chat:user"); ok {
		t.Fatal("no evidence expected initially")
	}
	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", Args: []string{"test", "./..."}, ExitCode: 1})
	ev, ok := p.lastExecEvidence("synthetic:chat:user")
	if !ok || ev.ExitCode != 1 || ev.Command != "go test ./..." {
		t.Fatalf("evidence = (%+v, %v)", ev, ok)
	}

	p.clearExecEvidence("synthetic:chat:user")
	if _, ok := p.lastExecEvidence("synthetic:chat:user"); ok {
		t.Fatal("evidence should be cleared")
	}
}

func TestCompletionGuardBlocksFailedCommand(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "带验证的任务", "steps": []any{"修改代码", "运行测试"}})

	// 中间步骤的完成不受证据影响。
	execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa,
		map[string]any{"step_id": "step_1", "status": "done"})

	// 失败的验证命令：最终步骤拒绝完成。
	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", Args: []string{"test"}, ExitCode: 1})
	res := execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa,
		map[string]any{"step_id": "step_2", "status": "done"})
	if !strings.Contains(res, "错误") || !strings.Contains(res, "退出码") {
		t.Fatalf("completion should be rejected, got %q", res)
	}
	plan := sess.PlanSnapshot()
	if plan == nil || !plan.Active || plan.Steps[1].Status == session.PlanDone {
		t.Fatalf("plan should not be completed: %+v", plan)
	}

	// 重新验证成功后允许完成。
	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", Args: []string{"test"}, ExitCode: 0})
	res = execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa,
		map[string]any{"step_id": "step_2", "status": "done"})
	if strings.Contains(res, "错误") {
		t.Fatalf("completion should be allowed after a successful command, got %q", res)
	}

	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 1 || list[0].Status != task.StatusSucceeded {
		t.Fatalf("task should be succeeded: (%v, %v)", list, err)
	}
}

func TestNewPlanClearsOldEvidence(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	create := planToolForTest(t, catalog.PlanCreateToolName)

	execPlanTool(t, create, pa, map[string]any{"task": "任务一", "steps": []any{"步骤甲", "步骤乙"}})
	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", Args: []string{"test"}, ExitCode: 1})
	execPlanTool(t, create, pa, map[string]any{"task": "任务二", "steps": []any{"步骤丙", "步骤丁"}})

	if _, ok := p.lastExecEvidence(sess.ID); ok {
		t.Fatal("a new plan must clear stale execution evidence")
	}
}
