package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// newTaskPlugin 在管理命令测试插件之上装配内存任务存储。
func newTaskPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := newManagePlugin(t)
	p.tasks = task.NewMemStore()
	return p
}

// planToolForTest 取出内置规划动作。
func planToolForTest(t *testing.T, name string) toolkit.Tool {
	t.Helper()
	for _, tool := range catalog.BuildPlanTools(8) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("plan tool %q not built", name)
	return toolkit.Tool{}
}

// execPlanTool 通过装配侧的端口执行一次规划动作。
func execPlanTool(t *testing.T, tool toolkit.Tool, pa catalog.PlanAccess, args map[string]any) string {
	t.Helper()
	res, err := tool.Execute(catalog.WithPlanAccess(context.Background(), pa), args)
	if err != nil {
		t.Fatalf("%s: %v", tool.Name, err)
	}
	return res
}

func TestCreatePlanPersistsTask(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)

	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa, map[string]any{
		"task":  "整理项目报告",
		"steps": []any{"收集数据", "撰写报告"},
	})

	active, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.Status != task.StatusRunning {
		t.Fatalf("status = %q, want running", active.Status)
	}
	if active.Goal != "整理项目报告" || active.PlanVersion != 1 || len(active.Steps) != 2 {
		t.Fatalf("unexpected task: %+v", active)
	}
	if active.OwnerID != "user" || active.ChatID != "chat" {
		t.Fatalf("ownership not recorded: %+v", active)
	}

	// 进度更新：步骤状态同步到任务，任务仍在进行。
	execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa, map[string]any{
		"step_id": "step_1", "status": "in_progress",
	})
	active, err = p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.Steps[0].Status != task.StepRunning {
		t.Fatalf("step_1 = %q, want running", active.Steps[0].Status)
	}

	execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa, map[string]any{
		"step_id": "step_1", "status": "done", "note": "数据已收集",
	})
	execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa, map[string]any{
		"step_id": "step_2", "status": "done",
	})

	// 步骤全部终结：任务收敛为成功，不再出现在活动查询里，但历史保留。
	if _, err := p.tasks.ActiveBySession(sess.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("task should no longer be active, got %v", err)
	}
	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	if list[0].Status != task.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", list[0].Status)
	}
	if list[0].Steps[0].Result != "数据已收集" {
		t.Fatalf("step result not preserved: %+v", list[0].Steps[0])
	}
}

func TestNewPlanSupersedesActiveTask(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	create := planToolForTest(t, catalog.PlanCreateToolName)

	execPlanTool(t, create, pa, map[string]any{"task": "旧目标", "steps": []any{"旧步骤一", "旧步骤二"}})
	first, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}

	execPlanTool(t, create, pa, map[string]any{"task": "新目标", "steps": []any{"新步骤一", "新步骤二"}})
	active, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.ID == first.ID {
		t.Fatal("new plan should create a new task")
	}

	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	var superseded *task.Task
	for _, tk := range list {
		if tk.ID == first.ID {
			superseded = tk
		}
	}
	if superseded == nil || superseded.Status != task.StatusCancelled {
		t.Fatalf("superseded task = %+v, want cancelled", superseded)
	}
	if len(superseded.Steps) != 2 {
		t.Fatalf("superseded task should keep its steps, got %+v", superseded.Steps)
	}
}

func TestParallelPlanUpdatesKeepTaskConsistent(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "并行更新", "steps": []any{"第一步", "第二步"}})

	// 同一会话的计划动作可能并行执行（tool_parallel）。并发更新同一目标状态，
	// 最终任务必须保持单条记录且进度不丢——由 taskMu 串行化的镜像过程保证。
	update := planToolForTest(t, catalog.PlanUpdateToolName)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := update.Execute(catalog.WithPlanAccess(context.Background(), p.planAccessFor(ctx, sess)),
				map[string]any{"step_id": "step_1", "status": "done"})
			if err != nil || strings.Contains(res, "错误") {
				t.Errorf("parallel update = (%q, %v)", res, err)
			}
		}()
	}
	wg.Wait()

	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	if list[0].Steps[0].Status != task.StepSucceeded {
		t.Fatalf("step_1 = %q, want succeeded", list[0].Steps[0].Status)
	}
	if list[0].Steps[1].Status != task.StepPending {
		t.Fatalf("step_2 = %q, want pending", list[0].Steps[1].Status)
	}
	if list[0].PlanVersion != 1 {
		t.Fatalf("progress updates must not bump the plan version: v%d", list[0].PlanVersion)
	}
}

func TestRecoverInterruptedTasks(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "重启恢复", "steps": []any{"第一步", "第二步"}})

	// 模拟进程重启：任务记录仍在 running，插件重新装配时执行恢复。
	p.recoverInterruptedTasks()
	interrupted, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if interrupted.Status != task.StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", interrupted.Status)
	}

	// 下一次计划推进把任务带回执行中。
	execPlanTool(t, planToolForTest(t, catalog.PlanUpdateToolName), pa,
		map[string]any{"step_id": "step_1", "status": "in_progress"})
	recovered, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if recovered.Status != task.StatusRunning {
		t.Fatalf("status = %q, want running after recovery", recovered.Status)
	}
	if recovered.Steps[0].Status != task.StepRunning {
		t.Fatalf("step_1 = %q, want running", recovered.Steps[0].Status)
	}
}

func TestTaskCommandStatusListCancel(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai task", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "检查任务命令", "steps": []any{"第一步", "第二步"}})

	if err := p.handleTaskCommand(ctx, ""); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := p.handleTaskCommand(ctx, "list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := p.handleTaskCommand(ctx, "bogus"); err != nil {
		t.Fatalf("help: %v", err)
	}
	if err := p.handleTaskCommand(ctx, "cancel"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	active, err := p.tasks.ActiveBySession(sess.ID)
	if !errors.Is(err, task.ErrNotFound) || active != nil {
		t.Fatalf("task should be cancelled, got (%+v, %v)", active, err)
	}
	if plan := sess.PlanSnapshot(); plan != nil && plan.Active {
		t.Fatal("cancel should also deactivate the session plan")
	}
	// 重复取消不报错。
	if err := p.handleTaskCommand(ctx, "cancel"); err != nil {
		t.Fatalf("second cancel: %v", err)
	}
}

func TestPlanCancelAlsoCancelsTask(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai plan cancel", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "取消计划", "steps": []any{"第一步", "第二步"}})

	if err := p.handlePlanCommand(ctx, "cancel"); err != nil {
		t.Fatalf("plan cancel: %v", err)
	}
	if _, err := p.tasks.ActiveBySession(sess.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("task should be cancelled with the plan, got %v", err)
	}
}

func TestStopCancelsTask(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai stop", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "停止生成", "steps": []any{"第一步", "第二步"}})

	if err := p.handleStopCommand(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	if list[0].Status != task.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", list[0].Status)
	}
}

func TestSyncActiveTaskBackfillsLegacyPlan(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")

	// 旧版本遗留的会话：有计划但没有任务记录。
	sess.SetPlan(&session.Plan{
		Task:   "遗留计划",
		Active: true,
		Steps: []session.PlanStep{
			{ID: "step_1", Description: "第一步", Status: session.PlanPending},
			{ID: "step_2", Description: "第二步", Status: session.PlanPending},
		},
	})

	pa := p.planAccessFor(ctx, sess)
	if _, err := pa.UpdatePlan(func(plan *session.Plan) error {
		plan.Steps[0].Status = session.PlanInProgress
		return nil
	}); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}

	active, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("backfilled task missing: %v", err)
	}
	if active.Status != task.StatusRunning || active.PlanVersion != 1 {
		t.Fatalf("backfilled task = %+v", active)
	}
	if active.Steps[0].Status != task.StepRunning {
		t.Fatalf("step_1 = %q, want running", active.Steps[0].Status)
	}
}

func TestTaskCommandWithoutStoreDoesNotPanic(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{TriggerCmd: "/ai"},
		sm:  session.NewSessionManager(10, 20, 0, nil),
	}
	ctx := makeManageCtx("/ai task", false)
	if err := p.handleTaskCommand(ctx, ""); err != nil {
		t.Fatalf("status without store: %v", err)
	}
	if err := p.handleTaskCommand(ctx, "list"); err != nil {
		t.Fatalf("list without store: %v", err)
	}
	if err := p.handleTaskCommand(ctx, "cancel"); err != nil {
		t.Fatalf("cancel without store: %v", err)
	}
}

func TestTaskFormatHelpers(t *testing.T) {
	tk := task.New("生成月度报告", "synthetic:chat:user", "user", "chat", "synthetic")
	tk.PlanVersion = 2
	tk.Steps = []task.Step{
		{ID: "step_1", Description: "收集数据", Status: task.StepSucceeded, Result: "完成"},
		{ID: "step_2", Description: "撰写报告", Status: task.StepRunning},
	}
	tk.Status = task.StatusRunning
	detail := formatTaskDetail(tk)
	for _, want := range []string{tk.ID, "生成月度报告", "进行中", "收集数据", "v2"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q: %s", want, detail)
		}
	}
	if !strings.Contains(formatTaskDetail(nil), "没有进行中的任务") {
		t.Fatal("nil task detail should say no task")
	}
	if !strings.Contains(formatTaskList(nil, "/ai"), "还没有任务记录") {
		t.Fatal("empty list should say no records")
	}
	long := make([]*task.Task, 0, taskMaxListEntries+3)
	for range taskMaxListEntries + 3 {
		long = append(long, tk)
	}
	listText := formatTaskList(long, "/ai")
	if !strings.Contains(listText, "/ai task") {
		t.Fatalf("list should point at the detail command: %s", listText)
	}
	if !strings.Contains(listText, "共 13 条") {
		t.Fatalf("list should report the total count: %s", listText)
	}
}

func TestTaskHelpText(t *testing.T) {
	if h := (&Plugin{}).taskHelpText("/ai"); !strings.Contains(h, "/ai task cancel") {
		t.Fatalf("help text missing usage: %q", h)
	}
}
