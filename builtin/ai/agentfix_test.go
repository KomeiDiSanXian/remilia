package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestParseToolPermission(t *testing.T) {
	cases := []struct {
		in           string
		wantRes, act string
	}{
		{"bilibili.manage", "bilibili", "manage"},
		{"bilibili:manage", "bilibili", "manage"},
		{"bilibili", "bilibili", "*"},
		{"*", "*", "*"},
	}
	for _, tt := range cases {
		res, act := parseToolPermission(tt.in)
		if res != tt.wantRes || act != tt.act {
			t.Errorf("parseToolPermission(%q) = (%q,%q), want (%q,%q)", tt.in, res, act, tt.wantRes, tt.act)
		}
	}
}

// execProtectedTool 构造带权限声明的工具执行场景。
func execProtectedTool(t *testing.T, withPM bool, grant string, perms []string) string {
	t.Helper()
	p := &Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	p.reg.Register(Tool{
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
			res, act := parseToolPermission(grant)
			pm.GrantPermission("user1", permission.Permission{Resource: res, Action: act})
		}
		ctx.SetPermissionManager(pm)
	}

	return p.executeTool(ctx, ToolCall{Name: "protected_tool"}, context.Background(), &captureSender{})
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
	s := &Session{
		ID:     "p:g:u",
		UserID: "u",
		ChatID: "g",
	}
	s.setPlan(&Plan{
		Task:   "跨重启任务",
		Active: true,
		Steps: []PlanStep{
			{ID: "step_1", Description: "第一步", Status: PlanInProgress},
			{ID: "step_2", Description: "第二步", Status: PlanPending},
		},
	})

	rec := s.toRecord()
	if rec.Plan == "" {
		t.Fatal("plan should be persisted in record")
	}
	restored := rec.toSession()
	plan := restored.planSnapshot()
	if plan == nil || plan.Task != "跨重启任务" || !plan.Active {
		t.Fatalf("plan not restored: %+v", plan)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Status != PlanInProgress {
		t.Errorf("steps not restored: %+v", plan.Steps)
	}
	// 完成后不持久化 Active=false 的计划的注入文本
	if restored.planText() == "" {
		t.Error("restored active plan should be injectable")
	}
}

func TestToolTraceCap(t *testing.T) {
	s := &Session{}
	for range maxToolTrace + 10 {
		s.appendToolTrace(ToolTraceEntry{ToolName: "t", Duration: time.Millisecond})
	}
	entries := s.ToolTrace()
	if len(entries) != maxToolTrace {
		t.Errorf("expected %d entries, got %d", maxToolTrace, len(entries))
	}
}

func TestProcessWithToolsRecordsTrace(t *testing.T) {
	var streamCalls int
	p := &Plugin{
		cfg:      &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls++
				ch := make(chan StreamEvent, 3)
				switch streamCalls {
				case 1:
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "call_1", Name: "flaky_tool", Arguments: map[string]any{"query": "天气"},
					}}
				case 2:
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "call_2", Name: "flaky_tool", Arguments: map[string]any{"query": "天气"},
					}}
				default:
					ch <- StreamEvent{Type: StreamEventText, Content: "done"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	var attempts int
	p.reg.Register(Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("boom")
			}
			return "ok", nil
		},
	})

	session := p.sm.GetOrCreate("test:trace", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "查天气"})

	evt := platform.NewSyntheticEvent("c2c", "查天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, session); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}

	entries := session.ToolTrace()
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
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	session := p.sm.GetOrCreate("discord:chat:user", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})
	session.appendToolTrace(ToolTraceEntry{
		Time: time.Now(), ToolName: "get_weather",
		Args: "city=北京", Duration: 250 * time.Millisecond,
	})

	ctx := makeContext("/ai trace")
	if err := p.execSubCommand(ctx, "trace"); err != nil {
		t.Fatalf("execSubCommand trace failed: %v", err)
	}

	// 无记录场景不报错
	p2 := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	_ = p2.sm.GetOrCreate("discord:chat:user", "user", "chat")
	ctx2 := makeContext("/ai trace")
	if err := p2.execSubCommand(ctx2, "trace"); err != nil {
		t.Fatalf("execSubCommand trace (empty) failed: %v", err)
	}
}
