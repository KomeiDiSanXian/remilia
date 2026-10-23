package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

func TestTaskBudgetBlocksAndTerminates(t *testing.T) {
	p := newTaskPlugin(t)
	p.cfg.TaskMaxToolCalls = 2
	p.cfg.TaskMaxDuration = 0
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "预算任务", "steps": []any{"第一步", "第二步"}})

	for i := range 2 {
		if reason := p.chargeTaskBudget(sess); reason != "" {
			t.Fatalf("charge %d should pass, got %q", i+1, reason)
		}
	}
	reason := p.chargeTaskBudget(sess)
	if reason == "" || !strings.Contains(reason, "工具调用次数") {
		t.Fatalf("third charge should exceed the budget, got %q", reason)
	}
	p.terminateTaskForBudget(sess, reason)

	list, err := p.tasks.ListBySession(sess.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	if list[0].Status != task.StatusFailed {
		t.Fatalf("task status = %q, want failed", list[0].Status)
	}
	if plan := sess.PlanSnapshot(); plan != nil && plan.Active {
		t.Fatal("the session plan should be cancelled when the budget is exceeded")
	}
	if _, err := p.tasks.ActiveBySession(sess.ID); err == nil {
		t.Fatal("budget-failed task must not stay active")
	}
}

func TestTaskBudgetDuration(t *testing.T) {
	p := newTaskPlugin(t)
	p.cfg.TaskMaxToolCalls = 0
	p.cfg.TaskMaxDuration = time.Millisecond
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")

	if reason := p.chargeTaskBudget(sess); reason != "" {
		t.Fatalf("first charge should start the budget, got %q", reason)
	}
	time.Sleep(5 * time.Millisecond)
	reason := p.chargeTaskBudget(sess)
	if reason == "" || !strings.Contains(reason, "执行时长") {
		t.Fatalf("duration budget should be exceeded, got %q", reason)
	}
}

func TestNewPlanResetsTaskBudget(t *testing.T) {
	p := newTaskPlugin(t)
	p.cfg.TaskMaxToolCalls = 2
	p.cfg.TaskMaxDuration = 0
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")

	p.chargeTaskBudget(sess)
	p.chargeTaskBudget(sess)
	if reason := p.chargeTaskBudget(sess); reason == "" {
		t.Fatal("budget should be exhausted before the new plan")
	}

	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), p.planAccessFor(ctx, sess),
		map[string]any{"task": "新任务", "steps": []any{"第一步", "第二步"}})
	for i := range 2 {
		if reason := p.chargeTaskBudget(sess); reason != "" {
			t.Fatalf("charge %d after a new plan should pass, got %q", i+1, reason)
		}
	}
}

func TestPruneSessionState(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")

	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", ExitCode: 0})
	p.chargeTaskBudget(sess)

	// 把两份运行时状态改为已过期，验证按 TTL 清理。
	stale := time.Now().Add(-48 * time.Hour)
	p.evidenceMu.Lock()
	evidence := p.execEvidence["synthetic:chat:user"]
	evidence.At = stale
	p.execEvidence["synthetic:chat:user"] = evidence
	p.evidenceMu.Unlock()
	p.taskUsageMu.Lock()
	usage := p.taskUsage["synthetic:chat:user"]
	usage.StartedAt = stale
	p.taskUsageMu.Unlock()

	if removed := p.pruneSessionState(time.Now().Add(-24 * time.Hour)); removed != 2 {
		t.Fatalf("pruned = %d, want 2", removed)
	}
	if _, ok := p.lastExecEvidence("synthetic:chat:user"); ok {
		t.Fatal("expired evidence should be pruned")
	}
	p.taskUsageMu.Lock()
	_, usageLeft := p.taskUsage["synthetic:chat:user"]
	p.taskUsageMu.Unlock()
	if usageLeft {
		t.Fatal("expired usage should be pruned")
	}

	// 新鲜状态不受影响。
	p.recordExecEvidence(ctx, catalog.ExecResult{Command: "go", ExitCode: 0})
	if removed := p.pruneSessionState(time.Now().Add(-24 * time.Hour)); removed != 0 {
		t.Fatalf("fresh state should not be pruned, removed %d", removed)
	}
}

func TestExecOneToolRejectsOverBudget(t *testing.T) {
	p := &Plugin{
		cfg:      &config.Config{TaskMaxToolCalls: 1, ToolTimeout: time.Second},
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	p.reg.Register(toolkit.Tool{
		Name: "echo",
		Execute: func(context.Context, *toolkit.Invocation, map[string]any) (toolkit.ToolResult, error) {
			return toolkit.TextResult("ok"), nil
		},
	})
	ctx := makeManageCtx("/ai", false)
	sess := &session.Session{ID: "synthetic:chat:user"}

	first := p.execOneTool(ctx, context.Background(), sess, &execution.CaptureSender{}, &sendBudget{limit: 1},
		protocol.ToolCall{Name: "echo"})
	if first.Err != nil || first.Result != "ok" {
		t.Fatalf("first call should run: %+v", first)
	}

	second := p.execOneTool(ctx, context.Background(), sess, &execution.CaptureSender{}, &sendBudget{limit: 1},
		protocol.ToolCall{Name: "echo"})
	if second.Err == nil || !strings.Contains(second.Result, "预算") {
		t.Fatalf("over-budget call should be rejected: %+v", second)
	}
}
