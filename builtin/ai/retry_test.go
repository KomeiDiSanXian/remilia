package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestIsToolErrorResult(t *testing.T) {
	cases := []struct {
		result string
		want   bool
	}{
		{"错误: 工具 \"x\" 执行失败: boom", true},
		{"错误：工具 \"x\" 执行超时", true},
		{"错误: 技能 \"s\" 执行失败: boom", true},
		{"错误: 未找到工具 \"x\"", true},
		{"工具 `x` 已被用户拒绝执行（审批未通过）", false}, // 审批拒绝不是执行失败
		{"ok, 一切正常", false},
		{"", false},
	}
	for _, tt := range cases {
		if got := isToolErrorResult(tt.result); got != tt.want {
			t.Errorf("isToolErrorResult(%q) = %v, want %v", tt.result, got, tt.want)
		}
	}
}

func TestBuildReflectionMessage(t *testing.T) {
	msg := buildReflectionMessage("get_weather", 2, "错误: 工具执行失败")
	if msg.Role != RoleUser {
		t.Errorf("reflection should be a user message, got %v", msg.Role)
	}
	if !strings.Contains(msg.Content, "get_weather") || !strings.Contains(msg.Content, "反思") {
		t.Errorf("reflection content should mention tool and ask for reflection: %q", msg.Content)
	}
}

func TestBuildRetryAbortMessage(t *testing.T) {
	msg := buildRetryAbortMessage("get_weather", 3, "错误: 超时")
	if !strings.Contains(msg, "get_weather") || !strings.Contains(msg, "已停止尝试") {
		t.Errorf("abort message mismatch: %q", msg)
	}
}

func TestEffectiveToolRetryLimit(t *testing.T) {
	if got := (&Plugin{cfg: &Config{}}).effectiveToolRetryLimit(); got != 2 {
		t.Errorf("default retry limit should be 2, got %d", got)
	}
	if got := (&Plugin{cfg: &Config{ToolRetryLimit: 5}}).effectiveToolRetryLimit(); got != 5 {
		t.Errorf("configured retry limit should be 5, got %d", got)
	}
}

func TestSessionToolFailureCounters(t *testing.T) {
	s := &Session{}
	if got := s.incrToolFailure("a"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if got := s.incrToolFailure("a"); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
	if got := s.incrToolFailure("b"); got != 1 {
		t.Errorf("per-tool counter expected 1, got %d", got)
	}
	s.resetToolFailure("a")
	if got := s.incrToolFailure("a"); got != 1 {
		t.Errorf("expected reset to 1, got %d", got)
	}
}

// alwaysFailingStream 每次流式调用都请求调用指定工具。
func alwaysFailingStream(toolName string) func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	return func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
		ch := make(chan StreamEvent, 3)
		ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "call_1", Name: toolName}}
		ch <- StreamEvent{Type: StreamEventDone}
		close(ch)
		return ch, nil
	}
}

// TestProcessWithToolsRetryAbort 工具连续失败达到预算上限后优雅中止，
// 期间注入反思指令，返回的回复包含失败说明而非裸错误。
func TestProcessWithToolsRetryAbort(t *testing.T) {
	var streamCalls int
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls++
				return alwaysFailingStream("failing_tool")(ctx, req)
			},
		},
	}
	p.reg.Register(Tool{
		Name: "failing_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("internal error")
		},
	})

	session := p.sm.GetOrCreate("test:retry", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("expected graceful abort without error, got: %v", err)
	}
	if !strings.Contains(result.Text, "已停止尝试") {
		t.Errorf("abort reply should explain the stop: %q", result.Text)
	}
	// 尝试 1 次 + 重试 2 次 = 3 次流式调用后中止
	if streamCalls != 3 {
		t.Errorf("expected 3 stream calls (1 + retry budget 2), got %d", streamCalls)
	}
	// 反思指令在会话历史中
	hasReflection := false
	for _, m := range session.SnapshotMessages() {
		if m.Role == RoleUser && strings.Contains(m.Content, "反思提示") {
			hasReflection = true
		}
	}
	if !hasReflection {
		t.Error("expected reflection instruction in session history")
	}
}

// TestProcessWithToolsRetrySuccessAfterReflection 连续失败后注入反思，
// 模型换策略成功执行则继续正常流程（计数器清零，不再中止）。
func TestProcessWithToolsRetrySuccessAfterReflection(t *testing.T) {
	var streamCalls int
	var attempts int
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls++
				ch := make(chan StreamEvent, 3)
				switch streamCalls {
				case 1, 2:
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "call_1", Name: "flaky_tool"}}
				case 3:
					// 反思后换参数重试成功
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "call_2", Name: "flaky_tool"}}
				default:
					ch <- StreamEvent{Type: StreamEventText, Content: "done"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts <= 2 {
				return "", errors.New("transient failure")
			}
			return "ok", nil
		},
	})

	session := p.sm.GetOrCreate("test:retry2", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "done" {
		t.Errorf("expected final text done, got %q", result.Text)
	}
	// 3 次工具轮 + 1 次收尾
	if streamCalls != 4 {
		t.Errorf("expected 4 stream calls, got %d", streamCalls)
	}
	// 反思指令确实注入过
	hasReflection := false
	for _, m := range session.SnapshotMessages() {
		if m.Role == RoleUser && strings.Contains(m.Content, "反思提示") {
			hasReflection = true
		}
	}
	if !hasReflection {
		t.Error("expected reflection instruction in session history")
	}
}

// TestProcessWithToolsSingleFailureNoReflection 单次失败不注入反思、不中止，
// 下一次成功后失败计数清零。
func TestProcessWithToolsSingleFailureNoReflection(t *testing.T) {
	var streamCalls int
	var attempts int
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls++
				ch := make(chan StreamEvent, 3)
				switch streamCalls {
				case 1:
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "call_1", Name: "flaky_tool"}}
				case 2:
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "call_2", Name: "flaky_tool"}}
				default:
					ch <- StreamEvent{Type: StreamEventText, Content: "done"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("transient failure")
			}
			return "ok", nil
		},
	})

	session := p.sm.GetOrCreate("test:retry3", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "done" {
		t.Errorf("expected final text done, got %q", result.Text)
	}
	for _, m := range session.SnapshotMessages() {
		if m.Role == RoleUser && strings.Contains(m.Content, "反思提示") {
			t.Error("single failure should not inject reflection")
		}
	}
}
