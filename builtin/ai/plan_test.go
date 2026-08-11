package ai

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestFormatPlan(t *testing.T) {
	plan := &Plan{
		Task:   "查询B站UP主并整理报告",
		Active: true,
		Steps: []PlanStep{
			{ID: "step_1", Description: "搜索UP主", Status: PlanInProgress},
			{ID: "step_2", Description: "获取视频数据", Status: PlanPending},
			{ID: "step_3", Description: "整理报告", Status: PlanDone, Result: "完成"},
		},
	}
	text := formatPlan(plan)
	if !strings.Contains(text, "查询B站UP主并整理报告") {
		t.Errorf("plan text should include task: %q", text)
	}
	if !strings.Contains(text, "[进行中]") || !strings.Contains(text, "[待执行]") || !strings.Contains(text, "[完成]") {
		t.Errorf("plan text should include status marks: %q", text)
	}
	if !strings.Contains(text, "step_2") {
		t.Errorf("plan text should include step ids: %q", text)
	}
}

func TestPlanCompleted(t *testing.T) {
	pending := &Plan{Steps: []PlanStep{{Status: PlanPending}}}
	if pending.completed() {
		t.Error("pending step should not be completed")
	}
	done := &Plan{Steps: []PlanStep{{Status: PlanDone}, {Status: PlanFailed}}}
	if !done.completed() {
		t.Error("done+failed should be completed")
	}
	if (&Plan{}).completed() {
		t.Error("empty plan should not be completed")
	}
}

func TestSessionPlanAccessors(t *testing.T) {
	s := &Session{}
	if s.planText() != "" {
		t.Error("empty session should have no plan text")
	}
	plan := &Plan{Task: "t", Active: true, Steps: []PlanStep{{ID: "step_1", Description: "s", Status: PlanPending}}}
	s.setPlan(plan)
	if s.planSnapshot() == nil {
		t.Fatal("planSnapshot should return plan")
	}
	if !strings.Contains(s.planText(), "t") {
		t.Errorf("planText should include task: %q", s.planText())
	}
	// 修改快照不应污染会话
	snap := s.planSnapshot()
	snap.Task = "changed"
	if strings.Contains(s.planText(), "changed") {
		t.Error("mutating snapshot should not affect session plan")
	}
	// 完成后的计划不再注入
	snap2 := s.planSnapshot()
	snap2.Steps[0].Status = PlanDone
	snap2.Active = false
	s.setPlan(snap2)
	if s.planText() != "" {
		t.Error("inactive plan should not be injected")
	}
}

func TestCreatePlanTool(t *testing.T) {
	p := &Plugin{cfg: &Config{PlanMaxSteps: 8}}
	tools := buildPlanTools(p.cfg.PlanMaxSteps)
	var create *Tool
	for i := range tools {
		if tools[i].Name == planCreateToolName {
			create = &tools[i]
		}
	}
	if create == nil {
		t.Fatal("create_plan tool not built")
	}
	if !containsCategoryStr(create.Categories, CategoryGeneral) {
		t.Error("create_plan should be general category")
	}

	session := &Session{}
	ctx := WithPlanSession(context.Background(), session)

	// 正常创建
	result, err := create.Execute(ctx, map[string]any{
		"task":  "查天气并推荐穿搭",
		"steps": []any{"查询今日天气", "根据温度推荐穿搭"},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(result, "计划已创建") || !strings.Contains(result, "step_2") {
		t.Errorf("unexpected create result: %q", result)
	}
	plan := session.planSnapshot()
	if plan == nil || !plan.Active || len(plan.Steps) != 2 {
		t.Fatalf("plan not stored correctly: %+v", plan)
	}

	// 步骤不足
	result, _ = create.Execute(ctx, map[string]any{"task": "t", "steps": []any{"只有一步"}})
	if !strings.Contains(result, "错误") {
		t.Errorf("single step should be rejected: %q", result)
	}

	// 超过上限
	p.cfg.PlanMaxSteps = 2
	tools2 := buildPlanTools(p.cfg.PlanMaxSteps)
	create2 := &tools2[0]
	result, _ = create2.Execute(ctx, map[string]any{"task": "t", "steps": []any{"a", "b", "c"}})
	if !strings.Contains(result, "错误") {
		t.Errorf("over-limit steps should be rejected: %q", result)
	}
}

func containsCategoryStr(cats []string, target string) bool {
	return slices.Contains(cats, target)
}

func TestUpdatePlanStepTool(t *testing.T) {
	p := &Plugin{cfg: &Config{PlanMaxSteps: 8}}
	tools := buildPlanTools(p.cfg.PlanMaxSteps)
	var create, update *Tool
	for i := range tools {
		switch tools[i].Name {
		case planCreateToolName:
			create = &tools[i]
		case planUpdateToolName:
			update = &tools[i]
		}
	}
	if update == nil {
		t.Fatal("update_plan_step tool not built")
	}

	session := &Session{}
	ctx := WithPlanSession(context.Background(), session)

	// 无计划时更新报错
	result, _ := update.Execute(ctx, map[string]any{"step_id": "step_1", "status": "done"})
	if !strings.Contains(result, "错误") {
		t.Errorf("update without plan should error: %q", result)
	}

	// 创建后按序更新
	create.Execute(ctx, map[string]any{"task": "t", "steps": []any{"第一步", "第二步"}})
	result, _ = update.Execute(ctx, map[string]any{"step_id": "step_1", "status": "in_progress"})
	if !strings.Contains(result, "[进行中]") {
		t.Errorf("expected in_progress mark: %q", result)
	}
	result, _ = update.Execute(ctx, map[string]any{"step_id": "step_1", "status": "done", "note": "查询成功"})
	if !strings.Contains(result, "[完成]") || !strings.Contains(result, "查询成功") {
		t.Errorf("expected done mark with note: %q", result)
	}

	// 非法状态
	result, _ = update.Execute(ctx, map[string]any{"step_id": "step_1", "status": "bogus"})
	if !strings.Contains(result, "错误") {
		t.Errorf("invalid status should error: %q", result)
	}

	// 不存在的步骤
	result, _ = update.Execute(ctx, map[string]any{"step_id": "step_99", "status": "done"})
	if !strings.Contains(result, "错误") {
		t.Errorf("unknown step should error: %q", result)
	}

	// 全部完成 → 计划结束，不再注入
	update.Execute(ctx, map[string]any{"step_id": "step_2", "status": "done"})
	if session.planText() != "" {
		t.Errorf("completed plan should not be injected, got %q", session.planText())
	}
}

func TestPlanInjectionInProcessWithTools(t *testing.T) {
	var seenPlan bool
	p := &Plugin{
		cfg:      &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanMaxSteps: 8, ToolSelectMax: 20},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				for _, m := range req.Messages {
					if m.Role == RoleSystem && strings.Contains(m.Content, "当前执行计划") {
						seenPlan = true
					}
				}
				ch := make(chan StreamEvent, 2)
				ch <- StreamEvent{Type: StreamEventText, Content: "done"}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, t := range buildPlanTools(p.cfg.PlanMaxSteps) {
		p.reg.Register(t)
	}

	session := p.sm.GetOrCreate("test:plan", "user", "chat")
	session.setPlan(&Plan{
		Task:   "复杂任务",
		Active: true,
		Steps:  []PlanStep{{ID: "step_1", Description: "做某事", Status: PlanPending}},
	})
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "开始"})

	evt := platform.NewSyntheticEvent("c2c", "开始")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, session); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if !seenPlan {
		t.Error("expected plan injected into request messages")
	}
}

// TestPlanFullFlow 完整链路：模型建计划 → 执行步骤更新 → 总结。
func TestPlanFullFlow(t *testing.T) {
	call := 0
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanMaxSteps: 8, ToolSelectMax: 20},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				call++
				ch := make(chan StreamEvent, 3)
				switch call {
				case 1:
					// 第一轮：创建计划
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "call_1", Name: planCreateToolName,
						Arguments: map[string]any{"task": "查天气", "steps": []any{"查温度", "推荐穿衣"}},
					}}
				case 2:
					// 第二轮：开始并完成第一步
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "call_2", Name: planUpdateToolName,
						Arguments: map[string]any{"step_id": "step_1", "status": "done"},
					}}
				case 3:
					// 第三轮：完成第二步 → 计划结束
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "call_3", Name: planUpdateToolName,
						Arguments: map[string]any{"step_id": "step_2", "status": "done"},
					}}
				default:
					ch <- StreamEvent{Type: StreamEventText, Content: "全部完成"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, t := range buildPlanTools(p.cfg.PlanMaxSteps) {
		p.reg.Register(t)
	}

	session := p.sm.GetOrCreate("test:planflow", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "帮我查天气并推荐穿衣"})

	evt := platform.NewSyntheticEvent("c2c", "帮我查天气并推荐穿衣")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "全部完成" {
		t.Errorf("expected final summary, got %q", result.Text)
	}
	if call != 4 {
		t.Errorf("expected 4 LLM calls, got %d", call)
	}
	plan := session.planSnapshot()
	if plan == nil || plan.Active {
		t.Errorf("plan should exist and be completed, got %+v", plan)
	}
	if plan.Steps[0].Status != PlanDone || plan.Steps[1].Status != PlanDone {
		t.Errorf("all steps should be done: %+v", plan.Steps)
	}
}
