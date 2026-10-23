package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/platform"
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

	prov, err := openAIProviderForTest(&config.Config{BaseURL: server.URL, APIKey: "k", Model: "m", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	resp, err := prov.Chat(context.Background(), &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}})
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

	sprov, err := openAIProviderForTest(&config.Config{BaseURL: streamServer.URL, APIKey: "k", Model: "m", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ch, err := sprov.ChatStream(context.Background(), &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var gotUsage *protocol.TokenUsage
	for ev := range ch {
		if ev.Type == protocol.StreamEventDone {
			gotUsage = ev.Usage
		}
	}
	if gotUsage == nil || gotUsage.PromptTokens != 7 || gotUsage.CompletionTokens != 9 {
		t.Errorf("流式 usage 解析错误: %+v", gotUsage)
	}
}

// TestMetricsProviderChat 验证 Provider 装饰器记录调用/延迟/token 指标，
// 并按 ctx 上的 kind 标注拆分 token 用量标签。
func TestMetricsProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens":           5,
				"completion_tokens":       6,
				"prompt_cache_hit_tokens": 3,
			},
		})
	}))
	defer server.Close()

	prov, err := newProvider(&config.Config{Provider: "openai", BaseURL: server.URL, APIKey: "k", Model: "test-model", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	callsBefore := counterValue(llmCalls, "test-model", "ok")
	promptBefore := counterValue(llmTokens, "test-model", "prompt", string(protocol.CallKindChat))
	completionBefore := counterValue(llmTokens, "test-model", "completion", string(protocol.CallKindChat))
	cachedBefore := counterValue(llmTokens, "test-model", "prompt_cached", string(protocol.CallKindChat))

	ctx := protocol.WithCallKind(context.Background(), protocol.CallKindChat)
	if _, err := prov.Chat(ctx, &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	checkDelta(t, "ai_llm_calls_total{result=ok}", callsBefore, counterValue(llmCalls, "test-model", "ok"), 1)
	checkDelta(t, "ai_llm_tokens_total{type=prompt,kind=chat}", promptBefore, counterValue(llmTokens, "test-model", "prompt", string(protocol.CallKindChat)), 5)
	checkDelta(t, "ai_llm_tokens_total{type=completion,kind=chat}", completionBefore, counterValue(llmTokens, "test-model", "completion", string(protocol.CallKindChat)), 6)
	checkDelta(t, "ai_llm_tokens_total{type=prompt_cached,kind=chat}", cachedBefore, counterValue(llmTokens, "test-model", "prompt_cached", string(protocol.CallKindChat)), 3)
}

// TestMetricsProviderKindDefault 验证未标注 ctx 的调用落为 kind=other
// （而不是被静默记进 chat，掩盖漏标的调用点）。
func TestMetricsProviderKindDefault(t *testing.T) {
	before := counterValue(llmTokens, "kind-model", "prompt", string(protocol.CallKindOther))

	recordLLMCall(llmCallMeta{model: "kind-model", kind: protocol.CallKindOther},
		llmCallResult{duration: time.Millisecond, usage: &protocol.TokenUsage{PromptTokens: 11}, result: "ok"})

	checkDelta(t, "ai_llm_tokens_total{type=prompt,kind=other}", before,
		counterValue(llmTokens, "kind-model", "prompt", string(protocol.CallKindOther)), 11)
	if got := protocol.CallKindOf(context.Background()); got != protocol.CallKindOther {
		t.Errorf("未标注 ctx 的 kind = %q, 期望 %q", got, protocol.CallKindOther)
	}
}

// TestMetricsProviderStreamError 验证流式错误路径的指标（同步错误 → result=error）。
func TestMetricsProviderStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	prov, err := newProvider(&config.Config{Provider: "openai", BaseURL: server.URL, APIKey: "k", Model: "err-model", MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	// doStreamRequest 对 5xx 同步返回错误（重试后），装饰器同步计数
	before := counterValue(llmCalls, "err-model", "error")

	_, err = prov.ChatStream(context.Background(), &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "x"}}})
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

	prov, err := anthropicProviderForTest(&config.Config{Provider: "anthropic", BaseURL: server.URL, APIKey: "k", Model: "claude-test", MaxTokens: 8})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var gotUsage *protocol.TokenUsage
	for ev := range ch {
		if ev.Type == protocol.StreamEventDone {
			gotUsage = ev.Usage
		}
	}
	if gotUsage == nil || gotUsage.PromptTokens != 11 || gotUsage.CompletionTokens != 22 {
		t.Errorf("anthropic 流式 usage 解析错误: %+v", gotUsage)
	}
}

// TestDecodeSpeed 验证生成速度的采样边界：只用首字之后的时长，
// 未知首字 / 无 token 数 / 解码窗口过短时一律不采样。
func TestDecodeSpeed(t *testing.T) {
	cases := []struct {
		name string
		dur  time.Duration
		ttft time.Duration
		n    int
		want float64
	}{
		{"正常", 2 * time.Second, 500 * time.Millisecond, 300, 200},
		{"无首字", 2 * time.Second, 0, 300, 0},
		{"无 completion token", 2 * time.Second, 500 * time.Millisecond, 0, 0},
		{"解码窗口过短", 500 * time.Millisecond, 495 * time.Millisecond, 1, 0},
		{"首字晚于结束", time.Second, 2 * time.Second, 3, 0},
	}
	for _, tc := range cases {
		if got := decodeSpeed(tc.dur, tc.ttft, tc.n); got != tc.want {
			t.Errorf("%s: decodeSpeed(%v, %v, %d) = %v, 期望 %v",
				tc.name, tc.dur, tc.ttft, tc.n, got, tc.want)
		}
	}
}

// TestMetricsProviderStreamStats 验证首字延迟与生成速度只对"真正产出内容"的
// 流式调用采样；非流式调用观测不到首字，不得写入这两个指标。
func TestMetricsProviderStreamStats(t *testing.T) {
	streamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 首包前延迟：模拟排队 + prefill，应计入首字延迟
		time.Sleep(40 * time.Millisecond)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"he"}}]}`)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"llo"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2}}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer streamSrv.Close()

	// 模型名带纳秒后缀：-count=N 重复运行时始终是新的标签组合，
	// CollectAndCount 的增量断言才成立。
	model := fmt.Sprintf("ttft-model-%d", time.Now().UnixNano())
	prov, err := newProvider(&config.Config{Provider: "openai", BaseURL: streamSrv.URL,
		APIKey: "k", Model: model, MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	ctx := protocol.WithCallKind(context.Background(), protocol.CallKindChat)
	ttftBefore := testutil.CollectAndCount(llmTTFT)
	speedBefore := testutil.CollectAndCount(llmSpeed)

	ch, err := prov.ChatStream(ctx, &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for range ch {
	}

	if got := testutil.CollectAndCount(llmTTFT); got != ttftBefore+1 {
		t.Errorf("流式调用应新增 1 个首字延迟序列, got %d want %d", got, ttftBefore+1)
	}
	if got := testutil.CollectAndCount(llmSpeed); got != speedBefore+1 {
		t.Errorf("流式调用应新增 1 个生成速度序列, got %d want %d", got, speedBefore+1)
	}

	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 6},
		})
	}))
	defer jsonSrv.Close()

	model2 := fmt.Sprintf("nostream-model-%d", time.Now().UnixNano())
	prov2, err := newProvider(&config.Config{Provider: "openai", BaseURL: jsonSrv.URL,
		APIKey: "k", Model: model2, MaxTokens: 8, IncludeUsage: true})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	ttftBefore2 := testutil.CollectAndCount(llmTTFT)
	speedBefore2 := testutil.CollectAndCount(llmSpeed)
	if _, err := prov2.Chat(ctx, &protocol.ChatRequest{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := testutil.CollectAndCount(llmTTFT); got != ttftBefore2 {
		t.Errorf("非流式调用不得写入首字延迟, got %d want %d", got, ttftBefore2)
	}
	if got := testutil.CollectAndCount(llmSpeed); got != speedBefore2 {
		t.Errorf("非流式调用不得写入生成速度, got %d want %d", got, speedBefore2)
	}
}

// TestLLMErrorClass 验证失败分类：429/401/上下文超长/5xx/超时/网络各归一类。
func TestLLMErrorClass(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"rate limit", &protocol.APIError{StatusCode: 429}, "rate_limit"},
		{"auth", &protocol.APIError{StatusCode: 401}, "auth"},
		{"forbidden", &protocol.APIError{StatusCode: 403}, "auth"},
		{"context length", &protocol.APIError{StatusCode: 400, Body: "This model's maximum context length is 65536 tokens"}, "context_length"},
		{"client", &protocol.APIError{StatusCode: 400, Body: "bad request"}, "client"},
		{"server", &protocol.APIError{StatusCode: 503}, "server"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "canceled"},
		{"network", &url.Error{Op: "Get", URL: "https://api.example.com", Err: errors.New("connection reset")}, "network"},
		{"other", errors.New("boom"), "other"},
	}
	for _, tc := range cases {
		if got := llmErrorClass(tc.err); got != tc.want {
			t.Errorf("%s: llmErrorClass = %q, 期望 %q", tc.name, got, tc.want)
		}
	}
}

// TestTurnResult 验证回合结果归类：中断优先、错误次之、空产出最后。
func TestTurnResult(t *testing.T) {
	cases := []struct {
		name        string
		result      *chatResult
		err         error
		interrupted bool
		want        string
	}{
		{"ok", &chatResult{Text: "hi"}, nil, false, "ok"},
		{"ok with attachment", &chatResult{Attachments: make([]platform.Attachment, 1)}, nil, false, "ok"},
		{"empty", &chatResult{}, nil, false, "empty"},
		{"nil", nil, nil, false, "empty"},
		{"error", &chatResult{Text: "partial"}, errors.New("x"), false, "error"},
		{"interrupted", &chatResult{Text: "partial"}, nil, true, "interrupted"},
	}
	for _, tc := range cases {
		if got := turnResult(tc.result, tc.err, tc.interrupted); got != tc.want {
			t.Errorf("%s: turnResult = %q, 期望 %q", tc.name, got, tc.want)
		}
	}
}

// TestLLMRecordStopped 验证停止路径的 result=stopped 标签。
func TestLLMRecordStopped(t *testing.T) {
	before := counterValue(llmCalls, "stop-model", "stopped")

	recordLLMCall(llmCallMeta{model: "stop-model", kind: protocol.CallKindChat},
		llmCallResult{duration: 100 * time.Millisecond, result: "stopped"})

	// 原先写作 got < 1：重复运行时恒真，等于没有断言增量恰好为 1。
	checkDelta(t, "ai_llm_calls_total{result=stopped}", before, counterValue(llmCalls, "stop-model", "stopped"), 1)
}
