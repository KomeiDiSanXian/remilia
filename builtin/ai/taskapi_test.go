package ai

import (
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
)

func TestTaskAPIQueriesAndCancel(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "接口任务", "steps": []any{"第一步", "第二步"}})

	list, err := p.TasksBySession(sess.ID, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("TasksBySession = (%v, %v)", list, err)
	}
	if list[0].Goal != "接口任务" {
		t.Fatalf("goal = %q", list[0].Goal)
	}
	if _, err := p.TasksBySession("   ", 0); !errors.Is(err, ErrTaskSessionRequired) {
		t.Fatalf("empty session error = %v, want ErrTaskSessionRequired", err)
	}

	tk, err := p.TaskByID(list[0].ID)
	if err != nil {
		t.Fatalf("TaskByID: %v", err)
	}
	if tk.Status != task.StatusRunning {
		t.Fatalf("status = %q, want running", tk.Status)
	}
	if _, err := p.TaskByID("missing"); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("missing task error = %v, want ErrNotFound", err)
	}

	if err := p.CancelTaskByID(list[0].ID); err != nil {
		t.Fatalf("CancelTaskByID: %v", err)
	}
	after, err := p.TaskByID(list[0].ID)
	if err != nil || after.Status != task.StatusCancelled {
		t.Fatalf("status after cancel = (%v, %v)", after, err)
	}
	if plan := sess.PlanSnapshot(); plan != nil && plan.Active {
		t.Fatal("cancelling the task should deactivate the session plan")
	}
	// 重复取消幂等。
	if err := p.CancelTaskByID(list[0].ID); err != nil {
		t.Fatalf("second cancel: %v", err)
	}
}

func TestTaskAPIRecentTasks(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	create := planToolForTest(t, catalog.PlanCreateToolName)

	for _, chat := range []string{"chat", "group-1"} {
		sess := p.sm.GetOrCreate("synthetic:"+chat+":user", "user", chat)
		pa := p.planAccessFor(ctx, sess)
		execPlanTool(t, create, pa, map[string]any{"task": "任务 " + chat, "steps": []any{"第一步", "第二步"}})
	}

	recent, err := p.RecentTasks(0)
	if err != nil || len(recent) != 2 {
		t.Fatalf("RecentTasks = (%v, %v)", recent, err)
	}
	capped, err := p.RecentTasks(1)
	if err != nil || len(capped) != 1 {
		t.Fatalf("RecentTasks(1) = (%v, %v)", capped, err)
	}
}

func TestTaskAPICancelFinishedTask(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "已完成任务", "steps": []any{"第一步", "第二步"}})

	list, err := p.TasksBySession(sess.ID, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("TasksBySession = (%v, %v)", list, err)
	}
	update := planToolForTest(t, catalog.PlanUpdateToolName)
	for _, stepID := range []string{"step_1", "step_2"} {
		execPlanTool(t, update, pa, map[string]any{"step_id": stepID, "status": "done"})
	}
	if err := p.CancelTaskByID(list[0].ID); !errors.Is(err, task.ErrTaskTerminal) {
		t.Fatalf("cancelling a finished task = %v, want ErrTaskTerminal", err)
	}
}
