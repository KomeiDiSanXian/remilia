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
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestToolFailureIsTypedNotTextual 成败由 ActionResult.Err 判定，而不是结果
// 文本前缀：工具正文以"错误:"开头但调用成功，不得算失败。
func TestToolFailureIsTypedNotTextual(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.reg.Register(toolkit.Tool{
		Name: "looks_like_error",
		Execute: func(context.Context, map[string]any) (string, error) {
			return "错误: 这是正常输出，只是恰好以此开头", nil
		},
	})
	p.reg.Register(toolkit.Tool{
		Name: "really_fails",
		Execute: func(context.Context, map[string]any) (string, error) {
			return "", errors.New("boom")
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	ok := p.executeToolResult(ctx, protocol.ToolCall{Name: "looks_like_error"}, context.Background(), &execution.CaptureSender{}, nil)
	if ok.Err != nil {
		t.Errorf("successful call must not be reported as failure, got err %v", ok.Err)
	}
	if ok.Text != "错误: 这是正常输出，只是恰好以此开头" {
		t.Errorf("result text must be preserved verbatim, got %q", ok.Text)
	}

	bad := p.executeToolResult(ctx, protocol.ToolCall{Name: "really_fails"}, context.Background(), &execution.CaptureSender{}, nil)
	if bad.Err == nil {
		t.Error("failed call must report an error")
	}
	if !strings.HasPrefix(bad.Text, "错误:") {
		t.Errorf("failure text should still be model-visible, got %q", bad.Text)
	}
}

func TestEffectiveToolRetryLimit(t *testing.T) {
	if got := runtime.EffectiveToolRetryLimit(&config.Config{}); got != 2 {
		t.Errorf("default retry limit should be 2, got %d", got)
	}
	if got := runtime.EffectiveToolRetryLimit(&config.Config{ToolRetryLimit: 5}); got != 5 {
		t.Errorf("configured retry limit should be 5, got %d", got)
	}
}

func TestSessionToolFailureCounters(t *testing.T) {
	s := &session.Session{}
	if got := s.IncrToolFailure("a"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if got := s.IncrToolFailure("a"); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
	if got := s.IncrToolFailure("b"); got != 1 {
		t.Errorf("per-tool counter expected 1, got %d", got)
	}
	s.ResetToolFailure("a")
	if got := s.IncrToolFailure("a"); got != 1 {
		t.Errorf("expected reset to 1, got %d", got)
	}
}

// alwaysFailingStream 每次流式调用都请求调用指定工具。
func alwaysFailingStream(toolName string) func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
	return func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
		ch := make(chan protocol.StreamEvent, 3)
		ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "call_1", Name: toolName}}
		ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
		close(ch)
		return ch, nil
	}
}

// TestProcessWithToolsRetryAbort 工具连续失败达到预算上限后优雅中止，
// 期间注入反思指令，返回的回复包含失败说明而非裸错误。
func TestProcessWithToolsRetryAbort(t *testing.T) {
	var streamCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				return alwaysFailingStream("failing_tool")(ctx, req)
			},
		},
	}
	p.reg.Register(toolkit.Tool{
		Name: "failing_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("internal error")
		},
	})

	sess := p.sm.GetOrCreate("test:retry", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
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
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser && strings.Contains(m.Content, "反思提示") {
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
		cfg:      &config.Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 3)
				switch streamCalls {
				case 1, 2:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "call_1", Name: "flaky_tool"}}
				case 3:
					// 反思后换参数重试成功
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "call_2", Name: "flaky_tool"}}
				default:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(toolkit.Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts <= 2 {
				return "", errors.New("transient failure")
			}
			return "ok", nil
		},
	})

	sess := p.sm.GetOrCreate("test:retry2", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
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
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser && strings.Contains(m.Content, "反思提示") {
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
		cfg:      &config.Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolRetryLimit: 2},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 3)
				switch streamCalls {
				case 1:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "call_1", Name: "flaky_tool"}}
				case 2:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "call_2", Name: "flaky_tool"}}
				default:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(toolkit.Tool{
		Name: "flaky_tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("transient failure")
			}
			return "ok", nil
		},
	})

	sess := p.sm.GetOrCreate("test:retry3", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "done" {
		t.Errorf("expected final text done, got %q", result.Text)
	}
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser && strings.Contains(m.Content, "反思提示") {
			t.Error("single failure should not inject reflection")
		}
	}
}
