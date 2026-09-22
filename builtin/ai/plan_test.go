package ai

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestFormatPlan(t *testing.T) {
	plan := &session.Plan{
		Task:   "查询B站UP主并整理报告",
		Active: true,
		Steps: []session.PlanStep{
			{ID: "step_1", Description: "搜索UP主", Status: session.PlanInProgress},
			{ID: "step_2", Description: "获取视频数据", Status: session.PlanPending},
			{ID: "step_3", Description: "整理报告", Status: session.PlanDone, Result: "完成"},
		},
	}
	text := session.FormatPlan(plan)
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
	pending := &session.Plan{Steps: []session.PlanStep{{Status: session.PlanPending}}}
	if pending.Completed() {
		t.Error("pending step should not be Completed")
	}
	done := &session.Plan{Steps: []session.PlanStep{{Status: session.PlanDone}, {Status: session.PlanFailed}}}
	if !done.Completed() {
		t.Error("done+failed should be Completed")
	}
	if (&session.Plan{}).Completed() {
		t.Error("empty plan should not be Completed")
	}
}

func TestSessionPlanAccessors(t *testing.T) {
	s := &session.Session{}
	if s.PlanText() != "" {
		t.Error("empty session should have no plan text")
	}
	plan := &session.Plan{Task: "t", Active: true, Steps: []session.PlanStep{{ID: "step_1", Description: "s", Status: session.PlanPending}}}
	s.SetPlan(plan)
	if s.PlanSnapshot() == nil {
		t.Fatal("PlanSnapshot should return plan")
	}
	if !strings.Contains(s.PlanText(), "t") {
		t.Errorf("PlanText should include task: %q", s.PlanText())
	}
	// 修改快照不应污染会话
	snap := s.PlanSnapshot()
	snap.Task = "changed"
	if strings.Contains(s.PlanText(), "changed") {
		t.Error("mutating snapshot should not affect session plan")
	}
	// 完成后的计划不再注入
	snap2 := s.PlanSnapshot()
	snap2.Steps[0].Status = session.PlanDone
	snap2.Active = false
	s.SetPlan(snap2)
	if s.PlanText() != "" {
		t.Error("inactive plan should not be injected")
	}
}

func TestCreatePlanTool(t *testing.T) {
	p := &Plugin{cfg: &config.Config{PlanMaxSteps: 8}}
	tools := catalog.BuildPlanTools(p.cfg.PlanMaxSteps)
	var create *toolkit.Tool
	for i := range tools {
		if tools[i].Name == catalog.PlanCreateToolName {
			create = &tools[i]
		}
	}
	if create == nil {
		t.Fatal("create_plan tool not built")
	}
	if !containsCategoryStr(create.Categories, toolkit.CategoryGeneral) {
		t.Error("create_plan should be general category")
	}

	sess := &session.Session{}
	ctx := runtime.WithPlanSession(context.Background(), sess)

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
	plan := sess.PlanSnapshot()
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
	tools2 := catalog.BuildPlanTools(p.cfg.PlanMaxSteps)
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
	p := &Plugin{cfg: &config.Config{PlanMaxSteps: 8}}
	tools := catalog.BuildPlanTools(p.cfg.PlanMaxSteps)
	var create, update *toolkit.Tool
	for i := range tools {
		switch tools[i].Name {
		case catalog.PlanCreateToolName:
			create = &tools[i]
		case catalog.PlanUpdateToolName:
			update = &tools[i]
		}
	}
	if update == nil {
		t.Fatal("update_plan_step tool not built")
	}

	sess := &session.Session{}
	ctx := runtime.WithPlanSession(context.Background(), sess)

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
	if sess.PlanText() != "" {
		t.Errorf("Completed plan should not be injected, got %q", sess.PlanText())
	}
}

func TestPlanInjectionInProcessWithTools(t *testing.T) {
	var seenPlan bool
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanMaxSteps: 8, ToolSelectMax: 20},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				for _, m := range req.Messages {
					if m.Role == protocol.RoleSystem && strings.Contains(m.Content, "当前执行计划") {
						seenPlan = true
					}
				}
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, t := range catalog.BuildPlanTools(p.cfg.PlanMaxSteps) {
		p.reg.Register(t)
	}

	sess := p.sm.GetOrCreate("test:plan", "user", "chat")
	sess.SetPlan(&session.Plan{
		Task:   "复杂任务",
		Active: true,
		Steps:  []session.PlanStep{{ID: "step_1", Description: "做某事", Status: session.PlanPending}},
	})
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "开始"})

	evt := platform.NewSyntheticEvent("c2c", "开始")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, sess); err != nil {
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
		cfg:      &config.Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanMaxSteps: 8, ToolSelectMax: 20},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				call++
				ch := make(chan protocol.StreamEvent, 3)
				switch call {
				case 1:
					// 第一轮：创建计划
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "call_1", Name: catalog.PlanCreateToolName,
						Arguments: map[string]any{"task": "查天气", "steps": []any{"查温度", "推荐穿衣"}},
					}}
				case 2:
					// 第二轮：开始并完成第一步
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "call_2", Name: catalog.PlanUpdateToolName,
						Arguments: map[string]any{"step_id": "step_1", "status": "done"},
					}}
				case 3:
					// 第三轮：完成第二步 → 计划结束
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "call_3", Name: catalog.PlanUpdateToolName,
						Arguments: map[string]any{"step_id": "step_2", "status": "done"},
					}}
				default:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "全部完成"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, t := range catalog.BuildPlanTools(p.cfg.PlanMaxSteps) {
		p.reg.Register(t)
	}

	sess := p.sm.GetOrCreate("test:planflow", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "帮我查天气并推荐穿衣"})

	evt := platform.NewSyntheticEvent("c2c", "帮我查天气并推荐穿衣")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "全部完成" {
		t.Errorf("expected final summary, got %q", result.Text)
	}
	if call != 4 {
		t.Errorf("expected 4 LLM calls, got %d", call)
	}
	plan := sess.PlanSnapshot()
	if plan == nil || plan.Active {
		t.Errorf("plan should exist and be Completed, got %+v", plan)
	}
	if plan.Steps[0].Status != session.PlanDone || plan.Steps[1].Status != session.PlanDone {
		t.Errorf("all steps should be done: %+v", plan.Steps)
	}
}
