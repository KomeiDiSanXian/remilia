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
//
// 这些计数器是包级 Prometheus 指标，进程内只增不减，所以用例必须断言「本次
// 调用带来的增量」而不是绝对值：绝对值只在单次运行（-count=1）且没有其他用例
// 写过同一标签时成立，`go test -count=N` 会因累计值被放大而失败。core/engine、
// middleware、infra/metrics 等包的指标用例同样使用增量断言。
func counterValue(cv *prometheus.CounterVec, labels ...string) float64 {
	return testutil.ToFloat64(cv.WithLabelValues(labels...))
}

// checkDelta 断言计数器在 before→after 区间恰好增加了 want。
//
// 失败信息同时给出两个绝对值，便于区分「少记/多记」与「累计值干扰」两种成因。
func checkDelta(t *testing.T, name string, before, after, want float64) {
	t.Helper()
	if got := after - before; got != want {
		t.Errorf("%s 增量 = %v, 期望 %v（计数器 %v → %v）", name, got, want, before, after)
	}
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
	callsBefore := counterValue(llmCalls, "test-model", "ok")
	promptBefore := counterValue(llmTokens, "test-model", "prompt")
	completionBefore := counterValue(llmTokens, "test-model", "completion")

	if _, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	checkDelta(t, "ai_llm_calls_total{result=ok}", callsBefore, counterValue(llmCalls, "test-model", "ok"), 1)
	checkDelta(t, "ai_llm_tokens_total{type=prompt}", promptBefore, counterValue(llmTokens, "test-model", "prompt"), 5)
	checkDelta(t, "ai_llm_tokens_total{type=completion}", completionBefore, counterValue(llmTokens, "test-model", "completion"), 6)
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
	before := counterValue(llmCalls, "err-model", "error")

	_, err = prov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("5xx 流式请求应同步报错")
	}
	checkDelta(t, "ai_llm_calls_total{result=error}", before, counterValue(llmCalls, "err-model", "error"), 1)
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
	before := counterValue(llmCalls, "stop-model", "stopped")

	recordLLMCall("stop-model", 100*time.Millisecond, nil, "stopped")

	// 原先写作 got < 1：重复运行时恒真，等于没有断言增量恰好为 1。
	checkDelta(t, "ai_llm_calls_total{result=stopped}", before, counterValue(llmCalls, "stop-model", "stopped"), 1)
}
