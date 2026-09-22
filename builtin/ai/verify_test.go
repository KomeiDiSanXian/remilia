package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		in       string
		wantPass bool
		wantHasR bool
	}{
		{`{"verdict":"pass","reason":""}`, true, false},
		{`{"verdict":"fail","reason":"没有回答用户问题"}`, false, true},
		{`评审结果：{"verdict": "fail", "reason": "捏造数据"}`, false, true},
		{`PASS`, true, false},
		{`通过`, true, false},
		{`回答已覆盖全部要点`, true, false},
		{`不通过：回答与问题无关`, false, true},
		{`{"verdict":"fail"}`, false, false},
		{`无法判断的乱输出`, true, false},
	}
	for _, tt := range cases {
		v := runtime.ParseVerdict(tt.in)
		if v.Pass != tt.wantPass {
			t.Errorf("parseVerdict(%q).Pass = %v, want %v", tt.in, v.Pass, tt.wantPass)
		}
		if tt.wantHasR && v.Reason == "" {
			t.Errorf("parseVerdict(%q).Reason should be non-empty", tt.in)
		}
	}
}

func TestVerifyAnswer(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{APITimeout: 5 * time.Second},
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{Content: `{"verdict":"pass","reason":""}`}, nil
			},
		},
	}
	v, err := p.verifier().Verify(context.Background(), "今天天气怎么样", "今天晴转多云")
	if err != nil {
		t.Fatalf("verifyAnswer failed: %v", err)
	}
	if !v.Pass {
		t.Errorf("expected pass verdict, got %+v", v)
	}
}

func TestVerifyAnswerLLMError(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{APITimeout: 5 * time.Second},
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return nil, context.DeadlineExceeded
			},
		},
	}
	if _, err := p.verifier().Verify(context.Background(), "q", "a"); err == nil {
		t.Error("expected error from verifyAnswer on LLM failure")
	}
}

func TestBuildVerifyRetryMessage(t *testing.T) {
	msg := runtime.BuildVerifyRetryMessage("回答与问题无关")
	if !strings.Contains(msg, "回答与问题无关") || !strings.Contains(msg, "修正") {
		t.Errorf("retry message mismatch: %q", msg)
	}
}

// TestGenerateVerifiedPass 校验通过 → 直接返回，不再调用 LLM。
func TestGenerateVerifiedPass(t *testing.T) {
	var chatCalls, streamCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, VerifyEnabled: true, VerifyMaxRetries: 1},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "这是回答"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				chatCalls++
				return &protocol.ChatResponse{Content: `{"verdict":"pass","reason":""}`}, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:v1", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "问题"})

	evt := platform.NewSyntheticEvent("c2c", "问题")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.generateVerified(ctx, sess)
	if err != nil {
		t.Fatalf("generateVerified failed: %v", err)
	}
	if result.Text != "这是回答" {
		t.Errorf("expected answer text, got %q", result.Text)
	}
	if streamCalls != 1 || chatCalls != 1 {
		t.Errorf("expected 1 stream + 1 verify call, got stream=%d chat=%d", streamCalls, chatCalls)
	}
}

// TestGenerateVerifiedRetryOnce 校验失败 → 注入修正指令重生成一次 → 通过。
func TestGenerateVerifiedRetryOnce(t *testing.T) {
	var chatCalls int
	var streamCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, VerifyEnabled: true, VerifyMaxRetries: 1},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 2)
				if streamCalls == 1 {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "第一次回答"}
				} else {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "修正后的回答"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				chatCalls++
				if chatCalls == 1 {
					return &protocol.ChatResponse{Content: `{"verdict":"fail","reason":"没有回答用户问题"}`}, nil
				}
				return &protocol.ChatResponse{Content: `{"verdict":"pass","reason":""}`}, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:v2", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "问题"})

	evt := platform.NewSyntheticEvent("c2c", "问题")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.generateVerified(ctx, sess)
	if err != nil {
		t.Fatalf("generateVerified failed: %v", err)
	}
	if result.Text != "修正后的回答" {
		t.Errorf("expected corrected answer, got %q", result.Text)
	}
	if streamCalls != 2 || chatCalls != 2 {
		t.Errorf("expected 2 stream + 2 verify calls, got stream=%d chat=%d", streamCalls, chatCalls)
	}
	// 会话中留有修正指令
	hasRetry := false
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser && strings.Contains(m.Content, "质量校验未通过") {
			hasRetry = true
		}
	}
	if !hasRetry {
		t.Error("expected verify retry message in session")
	}
}

// TestGenerateVerifiedRetryLimit 持续失败 → 达到重试上限后返回最后回答。
func TestGenerateVerifiedRetryLimit(t *testing.T) {
	var streamCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, VerifyEnabled: true, VerifyMaxRetries: 2},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls++
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "回答N"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{Content: `{"verdict":"fail","reason":"仍不达标"}`}, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:v3", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "问题"})

	evt := platform.NewSyntheticEvent("c2c", "问题")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.generateVerified(ctx, sess)
	if err != nil {
		t.Fatalf("generateVerified failed: %v", err)
	}
	if result.Text == "" {
		t.Error("expected final answer despite failed verification")
	}
	// 1 次生成 + 2 次重试 = 3 次流式调用
	if streamCalls != 3 {
		t.Errorf("expected 3 stream calls (1 + retry limit 2), got %d", streamCalls)
	}
}

// TestGenerateVerifiedDisabled 未开启校验 → 零校验调用。
func TestGenerateVerifiedDisabled(t *testing.T) {
	var chatCalls int
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, VerifyEnabled: false},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "回答"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				chatCalls++
				return &protocol.ChatResponse{Content: `{"verdict":"pass"}`}, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:v4", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "问题"})

	evt := platform.NewSyntheticEvent("c2c", "问题")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.generateVerified(ctx, sess); err != nil {
		t.Fatalf("generateVerified failed: %v", err)
	}
	if chatCalls != 0 {
		t.Errorf("verification disabled should not call judge LLM, got %d calls", chatCalls)
	}
}
