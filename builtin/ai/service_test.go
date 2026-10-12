// service_test.go — 一次性 LLM 问答服务（LLMService/Complete）测试。
package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// captureProvider 捕获请求供断言。
type captureProvider struct {
	mockProvider
	lastReq  *protocol.ChatRequest
	chatErr  error
	response string
}

func (c *captureProvider) Chat(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
	if c.chatErr != nil {
		return nil, c.chatErr
	}
	c.lastReq = req
	return &protocol.ChatResponse{Content: c.response}, nil
}

func newServicePlugin(cap *captureProvider) *Plugin {
	return &Plugin{
		cfg: &config.Config{
			Model:           "gpt-test",
			Temperature:     0.7,
			TopP:            1.0,
			MaxTokens:       4096,
			APITimeout:      5 * time.Second,
			ReasoningEffort: "low",
		},
		prov: cap,
	}
}

// 接口断言：*Plugin 必须满足 LLMService（消费者可安全持有窄接口）。
var _ LLMService = (*Plugin)(nil)

func TestCompleteBuildsRequest(t *testing.T) {
	cap := &captureProvider{response: "ok"}
	p := newServicePlugin(cap)

	var srv LLMService = p
	got, err := srv.Complete(context.Background(), "你是摘要助手", "这是一篇文章")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "ok" {
		t.Errorf("content = %q", got)
	}
	req := cap.lastReq
	if req == nil {
		t.Fatal("no request captured")
	}
	if req.Model != "gpt-test" {
		t.Errorf("model = %q, want default %q", req.Model, "gpt-test")
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != protocol.RoleSystem ||
		req.Messages[1].Role != protocol.RoleUser || req.Messages[1].Content != "这是一篇文章" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	if req.Temperature != 0.7 || req.MaxTokens != 4096 {
		t.Errorf("temperature/max_tokens = %v/%v", req.Temperature, req.MaxTokens)
	}
	// ReasoningEffort 继承配置。
	if req.ReasoningEffort != "low" {
		t.Errorf("reasoning_effort = %q", req.ReasoningEffort)
	}
}

func TestCompleteOptionsOverride(t *testing.T) {
	cap := &captureProvider{response: "ok"}
	p := newServicePlugin(cap)

	_, err := p.Complete(context.Background(), "", "hi",
		WithModel("cheap-model"), WithTemperature(0), WithMaxTokens(64),
		WithTemperature(5), WithMaxTokens(0), WithModel("   "))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	req := cap.lastReq
	if req.Model != "cheap-model" {
		t.Errorf("model = %q, want override", req.Model)
	}
	if req.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", req.Temperature)
	}
	if req.MaxTokens != 64 {
		t.Errorf("max_tokens = %v, want 64", req.MaxTokens)
	}
	// 空 system 不入消息序列。
	if len(req.Messages) != 1 || req.Messages[0].Role != protocol.RoleUser {
		t.Fatalf("empty system should be omitted, got %+v", req.Messages)
	}
}

func TestCompleteNoDeadlineAppliesFallbackDeadline(t *testing.T) {
	cap := &captureProvider{response: "ok"}
	p := newServicePlugin(cap)

	done := make(chan error, 1)
	// context.Background 无 deadline：运行期会套上 APITimeout 兜底，
	// 这里只验证不挂起且正常返回（nil ctx 防御在运行期同样生效）。
	go func() {
		_, err := p.Complete(context.Background(), "sys", "user")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadline-less ctx must not hang (fallback deadline missing)")
	}
}

func TestCompleteErrorPassthrough(t *testing.T) {
	sprite := errors.New("api boom")
	cap := &captureProvider{chatErr: sprite}
	p := newServicePlugin(cap)

	_, err := p.Complete(context.Background(), "s", "u")
	if !errors.Is(err, sprite) {
		t.Errorf("error = %v, want passthrough %v", err, sprite)
	}
}
