package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

func TestOpenAIProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! How can I help?",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "gpt-4o-mini",
		MaxTokens: 100,
	}
	prov, err := openAIProviderForTest(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}

	resp, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Errorf("expected %q, got %q", "Hello! How can I help?", resp.Content)
	}
}

func TestOpenAIProvider_ChatWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Beijing"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	resp, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Weather?"}},
		Tools:    wireSpecs(toolkit.ActionsOf([]toolkit.Tool{{Name: "get_weather", Description: "Get weather"}})),
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected tool name %q, got %q", "get_weather", resp.ToolCalls[0].Name)
	}
}

func TestOpenAIProvider_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" World\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	var finishReason string
	for evt := range ch {
		switch evt.Type {
		case protocol.StreamEventText:
			text.WriteString(evt.Content)
		case protocol.StreamEventError:
			t.Fatalf("stream error: %v", evt.Err)
		case protocol.StreamEventDone:
			finishReason = evt.FinishReason
		}
	}
	if text.String() != "Hello World" {
		t.Errorf("expected %q, got %q", "Hello World", text.String())
	}
	if finishReason != "stop" {
		t.Errorf("expected finish_reason %q on Done, got %q", "stop", finishReason)
	}
}

func TestOpenAIProvider_ChatStreamToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\\\"Beijing\\\"}\"}}]},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"tool_calls\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Weather?"}},
		Tools:    wireSpecs(toolkit.ActionsOf([]toolkit.Tool{{Name: "get_weather"}})),
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var toolCalls []protocol.ToolCall
	for evt := range ch {
		if evt.Type == protocol.StreamEventToolCall && evt.ToolCall != nil {
			toolCalls = append(toolCalls, *evt.ToolCall)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "get_weather" {
		t.Errorf("expected name %q, got %q", "get_weather", toolCalls[0].Name)
	}
}

func TestOpenAIProvider_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid API key",
				"type":    "authentication_error",
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "bad-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	_, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestOpenAIProvider_ChatStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		return // error can also be returned directly
	}
	for evt := range ch {
		if evt.Type == protocol.StreamEventError {
			return // expected
		}
	}
	t.Error("expected stream error event")
}

// TestOpenAIProvider_ChatStreamAPIErrorInStream 验证 HTTP 200 但流内以错误对象
// 收尾（部分网关/内容过滤场景）时被识别为错误事件，而不是被忽略成一次空回复。
func TestOpenAIProvider_ChatStreamAPIErrorInStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"error\":{\"message\":\"content filter triggered\",\"type\":\"invalid_request_error\"}}\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		return // 也可由 ChatStream 直接返回错误
	}
	for evt := range ch {
		if evt.Type == protocol.StreamEventError {
			if !strings.Contains(evt.Err.Error(), "content filter triggered") {
				t.Errorf("unexpected error text: %v", evt.Err)
			}
			return
		}
	}
	t.Error("expected stream error event for in-stream error object")
}

func TestAnthropicProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_1",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello from Claude!"}},
			"stop_reason": "end_turn",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := anthropicProviderForTest(cfg)

	resp, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "You are Claude"},
			{Role: protocol.RoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "Hello from Claude!" {
		t.Errorf("expected %q, got %q", "Hello from Claude!", resp.Content)
	}
}

func TestAnthropicProvider_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: content_block_delta\n"))
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\n"))
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" World\"}}\n\n"))
		w.Write([]byte("event: message_stop\n"))
		w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := anthropicProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	for evt := range ch {
		switch evt.Type {
		case protocol.StreamEventText:
			text.WriteString(evt.Content)
		case protocol.StreamEventError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	if text.String() != "Hello World" {
		t.Errorf("expected %q, got %q", "Hello World", text.String())
	}
}

func TestOpenAIProvider_RetryOn5xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "OK"}, "finish_reason": "stop"},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "gpt-4o-mini",
		MaxTokens:  100,
		MaxRetries: 3,
		APITimeout: 5 * time.Second,
	}
	prov, _ := openAIProviderForTest(cfg)

	resp, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed after retry: %v", err)
	}
	if resp.Content != "OK" {
		t.Errorf("expected %q, got %q", "OK", resp.Content)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 success), got %d", attempts)
	}
}

func TestOpenAIProvider_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "key", Model: "gpt-4o-mini", MaxTokens: 100, APITimeout: 10 * time.Second}
	prov, _ := openAIProviderForTest(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := prov.Chat(ctx, &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Error("expected error for context cancel/timeout")
	}
}

func TestOpenAIProvider_ChatUsesRequestModelAndMaxTokens(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "k", Model: "default-model", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	_, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Model:     "override-model",
		MaxTokens: 77,
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if !strings.Contains(gotBody, `"model":"override-model"`) {
		t.Errorf("expected request-level model override, body: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"max_tokens":77`) {
		t.Errorf("expected request-level max_tokens, body: %s", gotBody)
	}
}

func TestOpenAIProvider_ChatStreamUsesRequestModelAndMaxTokens(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "k", Model: "default-model", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Model:     "override-model",
		MaxTokens: 88,
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	for range ch {
	}
	if !strings.Contains(gotBody, `"model":"override-model"`) {
		t.Errorf("expected request-level model override, body: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"max_tokens":88`) {
		t.Errorf("expected request-level max_tokens, body: %s", gotBody)
	}
}

func TestOpenAIProvider_ChatStreamRetryOn5xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "k", Model: "gpt-4o-mini", MaxTokens: 100, MaxRetries: 3}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	for evt := range ch {
		switch evt.Type {
		case protocol.StreamEventText:
			text.WriteString(evt.Content)
		case protocol.StreamEventError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	if text.String() != "ok" {
		t.Errorf("expected %q, got %q", "ok", text.String())
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 success), got %d", attempts)
	}
}

func TestAnthropicProvider_ChatStreamParallelToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{}}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\\\"Beijing\\\"\"}}\n\n"))
		w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_2\",\"name\":\"get_time\",\"input\":{}}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"tz\\\":\\\"UTC\\\"\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"}\"}}\n\n"))
		w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"}\"}}\n\n"))
		w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := anthropicProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Weather and time?"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var calls []protocol.ToolCall
	for evt := range ch {
		if evt.Type == protocol.StreamEventToolCall && evt.ToolCall != nil {
			calls = append(calls, *evt.ToolCall)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 parallel tool calls, got %d", len(calls))
	}

	byName := make(map[string]protocol.ToolCall, len(calls))
	for _, c := range calls {
		byName[c.Name] = c
	}

	wc, ok := byName["get_weather"]
	if !ok {
		t.Fatal("missing get_weather tool call")
	}
	if wc.ID != "toolu_1" {
		t.Errorf("expected get_weather id %q, got %q", "toolu_1", wc.ID)
	}
	if wc.Arguments["city"] != "Beijing" {
		t.Errorf("expected get_weather city=Beijing, got %v", wc.Arguments["city"])
	}

	tc, ok := byName["get_time"]
	if !ok {
		t.Fatal("missing get_time tool call")
	}
	if tc.ID != "toolu_2" {
		t.Errorf("expected get_time id %q, got %q", "toolu_2", tc.ID)
	}
	if tc.Arguments["tz"] != "UTC" {
		t.Errorf("expected get_time tz=UTC, got %v", tc.Arguments["tz"])
	}
}

func TestAnthropicProvider_ChatStreamErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := anthropicProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var gotErr error
	for evt := range ch {
		if evt.Type == protocol.StreamEventError {
			gotErr = evt.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected stream error event")
	}
	if !strings.Contains(gotErr.Error(), "Overloaded") {
		t.Errorf("expected error message %q in %q", "Overloaded", gotErr.Error())
	}
}

// TestOpenAIProvider_ChatStreamReasoning 思考型模型（DeepSeek 等）把推理内容放在
// reasoning_content 通道、把推理 token 数放在 completion_tokens_details：适配器
// 必须把两者都暴露出来，否则"预算被思考耗尽、正文为空"的截断会被误判成模型
// 无话可说，日志里也看不到任何线索。
func TestOpenAIProvider_ChatStreamReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":null,\"reasoning_content\":\"先想\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":null,\"reasoning_content\":\"再想\"},\"index\":0}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\",\"reasoning_content\":null},\"index\":0,\"finish_reason\":\"length\"}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":42,\"completion_tokens\":2048,\"completion_tokens_details\":{\"reasoning_tokens\":2048}}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "test-key", Model: "deepseek-flash", MaxTokens: 2048}
	prov, _ := openAIProviderForTest(cfg)

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "在吗"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var (
		reasoning    strings.Builder
		text         strings.Builder
		finishReason string
		reasoningTok int
	)
	for evt := range ch {
		switch evt.Type {
		case protocol.StreamEventReasoning:
			reasoning.WriteString(evt.Content)
		case protocol.StreamEventText:
			text.WriteString(evt.Content)
		case protocol.StreamEventError:
			t.Fatalf("stream error: %v", evt.Err)
		case protocol.StreamEventDone:
			finishReason = evt.FinishReason
			if evt.Usage != nil {
				reasoningTok = evt.Usage.ReasoningTokens
			}
		}
	}
	if reasoning.String() != "先想再想" {
		t.Errorf("expected reasoning %q, got %q", "先想再想", reasoning.String())
	}
	if text.String() != "" {
		t.Errorf("expected no visible content, got %q", text.String())
	}
	if finishReason != "length" {
		t.Errorf("expected finish_reason %q, got %q", "length", finishReason)
	}
	if reasoningTok != 2048 {
		t.Errorf("expected reasoning_tokens 2048, got %d", reasoningTok)
	}
}

// TestOpenAIProvider_ReasoningEffort 思考程度只在配置后出现在请求体里：未配置
// （空值）时完全不发送该字段，保证不认识 reasoning_effort 的网关不被拒绝。
func TestOpenAIProvider_ReasoningEffort(t *testing.T) {
	bodies := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sent := string(body)
		if strings.Contains(sent, `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n"))
			w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
				},
			})
		}
		bodies <- sent
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, APIKey: "k", Model: "m", MaxTokens: 100}
	prov, _ := openAIProviderForTest(cfg)
	msgs := []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}

	ch, err := prov.ChatStream(context.Background(), &protocol.ChatRequest{
		Messages: msgs, ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	for range ch {
	}
	if body := <-bodies; !strings.Contains(body, `"reasoning_effort":"none"`) {
		t.Errorf("expected stream payload to carry reasoning_effort, body: %s", body)
	}

	if _, err := prov.Chat(context.Background(), &protocol.ChatRequest{
		Messages: msgs, ReasoningEffort: "high",
	}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if body := <-bodies; !strings.Contains(body, `"reasoning_effort":"high"`) {
		t.Errorf("expected payload to carry reasoning_effort, body: %s", body)
	}

	if _, err := prov.Chat(context.Background(), &protocol.ChatRequest{Messages: msgs}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if body := <-bodies; strings.Contains(body, "reasoning_effort") {
		t.Errorf("reasoning_effort must be omitted when unset, body: %s", body)
	}
}
