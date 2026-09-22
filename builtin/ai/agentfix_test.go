package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// execProtectedTool 构造带权限声明的工具执行场景。
func execProtectedTool(t *testing.T, withPM bool, grant string, perms []string) string {
	t.Helper()
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	p.reg.Register(toolkit.Tool{
		Name:        "protected_tool",
		Description: "需要权限的工具",
		Permissions: perms,
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "executed", nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	if withPM {
		pm := eventctx.NewPermissionManager()
		if grant != "" {
			res, act := execution.ParseToolPermission(grant)
			pm.GrantPermission("user1", permission.Permission{Resource: res, Action: act})
		}
		ctx.SetPermissionManager(pm)
	}

	return p.executeToolResult(ctx, protocol.ToolCall{Name: "protected_tool"}, context.Background(), &execution.CaptureSender{}, nil).Text
}

func TestExecuteToolPermissionDenied(t *testing.T) {
	// 无权限管理器：安全默认拒绝
	if got := execProtectedTool(t, false, "", []string{"admin.manage"}); !strings.Contains(got, "错误") {
		t.Errorf("no permission manager should deny, got %q", got)
	}
	// 有管理器但未授权
	if got := execProtectedTool(t, true, "", []string{"admin.manage"}); !strings.Contains(got, "错误") {
		t.Errorf("ungranted user should be denied, got %q", got)
	}
	// 授权了其他权限
	if got := execProtectedTool(t, true, "other.perm", []string{"admin.manage"}); !strings.Contains(got, "错误") {
		t.Errorf("wrong permission should be denied, got %q", got)
	}
}

func TestExecuteToolPermissionGranted(t *testing.T) {
	// 精确授权
	if got := execProtectedTool(t, true, "admin.manage", []string{"admin.manage"}); got != "executed" {
		t.Errorf("granted user should execute, got %q", got)
	}
	// 任一命中即放行
	if got := execProtectedTool(t, true, "a.b", []string{"x.y", "a.b"}); got != "executed" {
		t.Errorf("any-of permission should pass, got %q", got)
	}
	// 无权限声明的工具不受影响
	if got := execProtectedTool(t, false, "", nil); got != "executed" {
		t.Errorf("tool without permissions should execute, got %q", got)
	}
}

func TestSessionPlanPersistRoundTrip(t *testing.T) {
	s := &session.Session{
		ID:     "p:g:u",
		UserID: "u",
		ChatID: "g",
	}
	s.SetPlan(&session.Plan{
		Task:   "跨重启任务",
		Active: true,
		Steps: []session.PlanStep{
			{ID: "step_1", Description: "第一步", Status: session.PlanInProgress},
			{ID: "step_2", Description: "第二步", Status: session.PlanPending},
		},
	})

	rec := s.ToRecord()
	if rec.Plan == "" {
		t.Fatal("plan should be persisted in record")
	}
	restored := rec.ToSession()
	plan := restored.PlanSnapshot()
	if plan == nil || plan.Task != "跨重启任务" || !plan.Active {
		t.Fatalf("plan not restored: %+v", plan)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Status != session.PlanInProgress {
		t.Errorf("steps not restored: %+v", plan.Steps)
	}
	// 完成后不持久化 Active=false 的计划的注入文本
	if restored.PlanText() == "" {
		t.Error("restored active plan should be injectable")
	}
}

func TestToolTraceCap(t *testing.T) {
	s := &session.Session{}
	for range session.MaxToolTrace + 10 {
		s.AppendToolTrace(session.ToolTraceEntry{ToolName: "t", Duration: time.Millisecond})
	}
	entries := s.ToolTrace()
	if len(entries) != session.MaxToolTrace {
		t.Errorf("expected %d entries, got %d", session.MaxToolTrace, len(entries))
	}
}

func TestProcessWithToolsRecordsTrace(t *testing.T) {
	var streamCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 3)
				switch streamCalls {
				case 1:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "call_1", Name: "flaky_tool", Arguments: map[string]any{"query": "天气"},
					}}
				case 2:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "call_2", Name: "flaky_tool", Arguments: map[string]any{"query": "天气"},
					}}
				default:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	var attempts int
	p.reg.Register(toolkit.Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("boom")
			}
			return "ok", nil
		},
	})

	sess := p.sm.GetOrCreate("test:trace", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "查天气"})

	evt := platform.NewSyntheticEvent("c2c", "查天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, sess); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}

	entries := sess.ToolTrace()
	if len(entries) != 2 {
		t.Fatalf("expected 2 trace entries, got %d", len(entries))
	}
	if entries[0].ToolName != "flaky_tool" || entries[0].Err == "" {
		t.Errorf("first entry should record failure: %+v", entries[0])
	}
	if !strings.Contains(entries[0].Err, "boom") {
		t.Errorf("error detail should be captured: %+v", entries[0])
	}
	if entries[1].Err != "" {
		t.Errorf("second entry should be success: %+v", entries[1])
	}
	if entries[0].Duration < 0 || entries[1].Duration < 0 {
		t.Error("duration should be non-negative")
	}
	if !strings.Contains(entries[0].Args, "query") {
		t.Errorf("args summary should be recorded: %+v", entries[0])
	}
}

func TestExecSubCommandTrace(t *testing.T) {
	p := &Plugin{
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &config.Config{},
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	sess := p.sm.GetOrCreate("discord:chat:user", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "hello"})
	sess.AppendToolTrace(session.ToolTraceEntry{
		Time: time.Now(), ToolName: "get_weather",
		Args: "city=北京", Duration: 250 * time.Millisecond,
	})

	ctx := makeContext("/ai trace")
	if err := p.execSubCommand(ctx, "trace"); err != nil {
		t.Fatalf("execSubCommand trace failed: %v", err)
	}

	// 无记录场景不报错
	p2 := &Plugin{
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &config.Config{},
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	_ = p2.sm.GetOrCreate("discord:chat:user", "user", "chat")
	ctx2 := makeContext("/ai trace")
	if err := p2.execSubCommand(ctx2, "trace"); err != nil {
		t.Fatalf("execSubCommand trace (empty) failed: %v", err)
	}
}
