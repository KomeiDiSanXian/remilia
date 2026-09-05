package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// counterValue 读取 CounterVec 中 (labels) 组合的当前值（无样本返回 0）。
func counterValue(cv *prometheus.CounterVec, labels ...string) float64 {
	return testutil.ToFloat64(cv.WithLabelValues(labels...))
}

// TestOpenAIUsageParsing 验证非流式与流式响应的 token 用量解析。
func TestOpenAIUsageParsing(t *testing.T) {
	// 非流式
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 34},
		})
	}))
	defer server.Close()

	prov, err := NewOpenAIProvider(&Config{BaseURL: server.URL, APIKey: "k", Model: "m", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	resp, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 34 {
		t.Errorf("非流式 usage 解析错误: %+v", resp.Usage)
	}

	// 流式：usage 块在 [DONE] 前到达
	streamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验 stream_options.include_usage 已随请求发送
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		if !strings.Contains(string(body[:n]), "include_usage") {
			t.Errorf("请求应包含 stream_options.include_usage, body: %s", string(body[:n]))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":9}}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer streamServer.Close()

	sprov, err := NewOpenAIProvider(&Config{BaseURL: streamServer.URL, APIKey: "k", Model: "m", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ch, err := sprov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var gotUsage *TokenUsage
	for ev := range ch {
		if ev.Type == StreamEventDone {
			gotUsage = ev.Usage
		}
	}
	if gotUsage == nil || gotUsage.PromptTokens != 7 || gotUsage.CompletionTokens != 9 {
		t.Errorf("流式 usage 解析错误: %+v", gotUsage)
	}
}

// TestMetricsProviderChat 验证 Provider 装饰器记录调用/延迟/token 指标。
func TestMetricsProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 6},
		})
	}))
	defer server.Close()

	prov, err := NewProvider(&Config{Provider: "openai", BaseURL: server.URL, APIKey: "k", Model: "test-model", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := counterValue(llmCalls, "test-model", "ok"); got != 1 {
		t.Errorf("ai_llm_calls_total = %v, 期望 1", got)
	}
	if got := counterValue(llmTokens, "test-model", "prompt"); got != 5 {
		t.Errorf("prompt tokens = %v, 期望 5", got)
	}
	if got := counterValue(llmTokens, "test-model", "completion"); got != 6 {
		t.Errorf("completion tokens = %v, 期望 6", got)
	}
}

// TestMetricsProviderStreamError 验证流式错误路径的指标（同步错误 → result=error）。
func TestMetricsProviderStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	prov, err := NewProvider(&Config{Provider: "openai", BaseURL: server.URL, APIKey: "k", Model: "err-model", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// doStreamRequest 对 5xx 同步返回错误（重试后），装饰器同步计数
	_, err = prov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("5xx 流式请求应同步报错")
	}
	if got := counterValue(llmCalls, "err-model", "error"); got != 1 {
		t.Errorf("error 调用应计数, got %v", got)
	}
}

// TestAnthropicStreamUsage 验证 Anthropic 流式 usage 解析（message_start/message_delta）。
func TestAnthropicStreamUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `event: message_start`)
		fmt.Fprintln(w, `data: {"type":"message_start","message":{"usage":{"input_tokens":11}}}`)
		fmt.Fprintln(w, `event: content_block_delta`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)
		fmt.Fprintln(w, `event: message_delta`)
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":22}}`)
		fmt.Fprintln(w, `event: message_stop`)
		fmt.Fprintln(w, `data: {"type":"message_stop"}`)
	}))
	defer server.Close()

	prov, err := NewAnthropicProvider(&Config{Provider: "anthropic", BaseURL: server.URL, APIKey: "k", Model: "claude-test", MaxTokens: 8})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ch, err := prov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var gotUsage *TokenUsage
	for ev := range ch {
		if ev.Type == StreamEventDone {
			gotUsage = ev.Usage
		}
	}
	if gotUsage == nil || gotUsage.PromptTokens != 11 || gotUsage.CompletionTokens != 22 {
		t.Errorf("anthropic 流式 usage 解析错误: %+v", gotUsage)
	}
}

// TestLLMRecordStopped 验证停止路径的 result=stopped 标签。
func TestLLMRecordStopped(t *testing.T) {
	recordLLMCall("stop-model", 100*time.Millisecond, nil, "stopped")
	if got := counterValue(llmCalls, "stop-model", "stopped"); got < 1 {
		t.Errorf("stopped 调用应计数, got %v", got)
	}
}
