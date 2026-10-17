package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
)

// planOf 构造一个测试计划：task 为任务描述，steps 为 (id, 描述, 状态) 三元组。
func planOf(task string, steps ...session.PlanStep) *session.Plan {
	return &session.Plan{Task: task, Steps: steps, Active: true}
}

func step(id, desc string, status session.PlanStepStatus) session.PlanStep {
	return session.PlanStep{ID: id, Description: desc, Status: status}
}

func TestNewTaskDefaults(t *testing.T) {
	tk := New("  修复构建失败  ", "qq:100:200", "200", "100", "qq")
	if tk.ID == "" || !strings.HasPrefix(tk.ID, "t-") {
		t.Fatalf("unexpected task ID %q", tk.ID)
	}
	if tk.Status != StatusPending {
		t.Fatalf("new task status = %q, want pending", tk.Status)
	}
	if tk.Goal != "修复构建失败" {
		t.Fatalf("goal = %q, want trimmed", tk.Goal)
	}
	if tk.SessionID != "qq:100:200" || tk.OwnerID != "200" || tk.ChatID != "100" || tk.Platform != "qq" {
		t.Fatalf("ownership fields not preserved: %+v", tk)
	}
	if !tk.Active() {
		t.Fatal("new task should be active")
	}
	if tk.CreatedAt.IsZero() || tk.UpdatedAt.IsZero() {
		t.Fatal("timestamps should be set")
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := make(map[string]bool, 256)
	for range 256 {
		id := NewID(time.Now())
		if seen[id] {
			t.Fatalf("duplicate task ID %q", id)
		}
		seen[id] = true
	}
}

func TestSyncPlanMapsStepsAndVersions(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	plan := planOf("解析仓库并修复测试",
		step("step_1", "分析问题", session.PlanInProgress),
		step("step_2", "修改代码", session.PlanPending),
		step("step_3", "运行测试", session.PlanPending),
	)
	if err := tk.SyncPlan(plan); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if tk.PlanVersion != 1 {
		t.Fatalf("plan version = %d, want 1", tk.PlanVersion)
	}
	if tk.Goal != "解析仓库并修复测试" {
		t.Fatalf("goal = %q", tk.Goal)
	}
	if len(tk.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(tk.Steps))
	}
	if tk.Steps[0].Status != StepRunning || tk.Steps[1].Status != StepPending {
		t.Fatalf("unexpected step statuses: %+v", tk.Steps)
	}
	if tk.Status.IsTerminal() {
		t.Fatalf("task must stay active while steps remain: %q", tk.Status)
	}

	// 纯进度更新：内容未变，版本不递增。
	plan.Steps[0].Status = session.PlanDone
	plan.Steps[1].Status = session.PlanInProgress
	if err := tk.SyncPlan(plan); err != nil {
		t.Fatalf("SyncPlan(progress): %v", err)
	}
	if tk.PlanVersion != 1 {
		t.Fatalf("progress update bumped plan version to %d", tk.PlanVersion)
	}
	if tk.Steps[0].Status != StepSucceeded {
		t.Fatalf("step 0 = %q, want succeeded", tk.Steps[0].Status)
	}
}

func TestSyncPlanContentChangeBumpsVersion(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	base := planOf("原始目标",
		step("step_1", "步骤甲", session.PlanPending),
		step("step_2", "步骤乙", session.PlanPending),
	)
	if err := tk.SyncPlan(base); err != nil {
		t.Fatalf("SyncPlan(base): %v", err)
	}
	if tk.PlanVersion != 1 {
		t.Fatalf("version = %d, want 1", tk.PlanVersion)
	}

	changed := planOf("调整后的目标",
		step("step_1", "步骤甲", session.PlanPending),
		step("step_2", "新的步骤乙", session.PlanPending),
		step("step_3", "新增步骤丙", session.PlanPending),
	)
	if err := tk.SyncPlan(changed); err != nil {
		t.Fatalf("SyncPlan(changed): %v", err)
	}
	if tk.PlanVersion != 2 {
		t.Fatalf("version = %d, want 2 after content change", tk.PlanVersion)
	}
	if tk.Goal != "调整后的目标" {
		t.Fatalf("goal = %q", tk.Goal)
	}
}

func TestSyncPlanSettlesSucceeded(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	plan := planOf("完成任务",
		step("step_1", "第一步", session.PlanDone),
		step("step_2", "第二步", session.PlanDone),
	)
	if err := tk.SyncPlan(plan); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if tk.Status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", tk.Status)
	}
	if tk.HasFailedSteps() {
		t.Fatal("should not report failed steps")
	}
}

func TestSyncPlanSettlesFailed(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	plan := planOf("尝试修复",
		step("step_1", "第一步", session.PlanDone),
		step("step_2", "第二步", session.PlanFailed),
	)
	if err := tk.SyncPlan(plan); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if tk.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", tk.Status)
	}
	if !tk.HasFailedSteps() {
		t.Fatal("failed step should be reported")
	}
}

func TestCancelledTaskRejectsLatePlan(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.SyncPlan(planOf("目标",
		step("step_1", "第一步", session.PlanInProgress),
		step("step_2", "第二步", session.PlanPending),
	)); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if err := tk.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// 迟到的"全部完成"计划不得把已取消的任务改回成功。
	late := planOf("目标",
		step("step_1", "第一步", session.PlanDone),
		step("step_2", "第二步", session.PlanDone),
	)
	err := tk.SyncPlan(late)
	if !errors.Is(err, ErrTaskTerminal) {
		t.Fatalf("late SyncPlan error = %v, want ErrTaskTerminal", err)
	}
	if tk.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", tk.Status)
	}
	if tk.Steps[0].Status != StepRunning {
		t.Fatalf("late result overwrote steps: %+v", tk.Steps)
	}
}

func TestCancelIdempotentAndTerminal(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.Cancel(); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	if err := tk.Cancel(); err != nil {
		t.Fatalf("second Cancel must be idempotent, got %v", err)
	}

	done := New("task", "s", "u", "c", "p")
	if err := done.SyncPlan(planOf("目标",
		step("step_1", "第一步", session.PlanDone),
		step("step_2", "第二步", session.PlanDone),
	)); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if err := done.Cancel(); !errors.Is(err, ErrTaskTerminal) {
		t.Fatalf("Cancel on succeeded task = %v, want ErrTaskTerminal", err)
	}
}

func TestInterruptedTaskResumesOnPlanProgress(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.SyncPlan(planOf("目标",
		step("step_1", "第一步", session.PlanInProgress),
		step("step_2", "第二步", session.PlanPending),
	)); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if err := tk.Transition(StatusInterrupted); err != nil {
		t.Fatalf("Transition(interrupted): %v", err)
	}

	// 恢复后收到新的进度：任务回到执行中，而不是停留在中断态。
	if err := tk.SyncPlan(planOf("目标",
		step("step_1", "第一步", session.PlanDone),
		step("step_2", "第二步", session.PlanInProgress),
	)); err != nil {
		t.Fatalf("SyncPlan after recovery: %v", err)
	}
	if tk.Status != StatusRunning {
		t.Fatalf("status = %q, want running after recovery", tk.Status)
	}
}

func TestStartIdempotentAndTerminal(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if tk.Status != StatusRunning {
		t.Fatalf("status = %q, want running", tk.Status)
	}
	if err := tk.Start(); err != nil {
		t.Fatalf("Start must be idempotent, got %v", err)
	}
	if err := tk.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := tk.Start(); !errors.Is(err, ErrTaskTerminal) {
		t.Fatalf("Start on cancelled task = %v, want ErrTaskTerminal", err)
	}
}

func TestSyncPlanNilPlan(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.SyncPlan(nil); !errors.Is(err, ErrNilPlan) {
		t.Fatalf("SyncPlan(nil) = %v, want ErrNilPlan", err)
	}
}

func TestStepsFromPlanUnknownStatusFallsBackToPending(t *testing.T) {
	plan := planOf("目标",
		step("step_1", "未知状态步骤", session.PlanStepStatus("mystery")),
	)
	steps := StepsFromPlan(plan)
	if len(steps) != 1 || steps[0].Status != StepPending {
		t.Fatalf("unknown status should map to pending, got %+v", steps)
	}
	if StepsFromPlan(nil) != nil {
		t.Fatal("StepsFromPlan(nil) should return nil")
	}
}

func TestCompletedRequiresSteps(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if tk.Completed() {
		t.Fatal("task without steps must not be completed")
	}
}

func TestRecordArtifactUpsertAndTerminal(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileCreated, Name: "main.go", Size: 10}); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileUpdated, Name: "main.go", Size: 42}); err != nil {
		t.Fatalf("RecordArtifact(update): %v", err)
	}
	if len(tk.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v, want create and update", tk.Artifacts)
	}
	// 同名同类就地更新。
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileUpdated, Name: "main.go", Size: 99}); err != nil {
		t.Fatalf("RecordArtifact(upsert): %v", err)
	}
	if len(tk.Artifacts) != 2 || tk.Artifacts[1].Size != 99 {
		t.Fatalf("upsert failed: %+v", tk.Artifacts)
	}
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileUpdated}); err == nil {
		t.Fatal("artifact without a name must be rejected")
	}

	if err := tk.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileUpdated, Name: "late.go"}); !errors.Is(err, ErrTaskTerminal) {
		t.Fatalf("terminal task artifact error = %v, want ErrTaskTerminal", err)
	}
}

func TestCloneIsDeepCopy(t *testing.T) {
	tk := New("task", "s", "u", "c", "p")
	tk.Steps = []Step{{ID: "step_1", Description: "d", Status: StepPending}}
	tk.Artifacts = []Artifact{{Kind: ArtifactFileCreated, Name: "a.go", Size: 1}}
	cp := tk.Clone()
	cp.Steps[0].Description = "changed"
	cp.Artifacts[0].Name = "changed"
	if tk.Steps[0].Description != "d" || tk.Artifacts[0].Name != "a.go" {
		t.Fatal("Clone must not share the steps or artifacts slices")
	}
	if (*Task)(nil).Clone() != nil {
		t.Fatal("nil Clone should be nil")
	}
}
