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

	cfg := &Config{
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "gpt-4o-mini",
		MaxTokens: 100,
	}
	prov, err := NewOpenAIProvider(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}

	resp, err := prov.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	resp, err := prov.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather?"}},
		Tools:    []Tool{{Name: "get_weather", Description: "Get weather"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	for evt := range ch {
		switch evt.Type {
		case StreamEventText:
			text.WriteString(evt.Content)
		case StreamEventError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	if text.String() != "Hello World" {
		t.Errorf("expected %q, got %q", "Hello World", text.String())
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

	cfg := &Config{BaseURL: server.URL, APIKey: "test-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather?"}},
		Tools:    []Tool{{Name: "get_weather"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var toolCalls []ToolCall
	for evt := range ch {
		if evt.Type == StreamEventToolCall && evt.ToolCall != nil {
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

	cfg := &Config{BaseURL: server.URL, APIKey: "bad-key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	_, err := prov.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "key", Model: "gpt-4o-mini", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		return // error can also be returned directly
	}
	for evt := range ch {
		if evt.Type == StreamEventError {
			return // expected
		}
	}
	t.Error("expected stream error event")
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

	cfg := &Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := NewAnthropicProvider(cfg)

	resp, err := prov.Chat(context.Background(), &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are Claude"},
			{Role: RoleUser, Content: "Hi"},
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

	cfg := &Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := NewAnthropicProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	for evt := range ch {
		switch evt.Type {
		case StreamEventText:
			text.WriteString(evt.Content)
		case StreamEventError:
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

	cfg := &Config{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "gpt-4o-mini",
		MaxTokens:  100,
		MaxRetries: 3,
		APITimeout: 5 * time.Second,
	}
	prov, _ := NewOpenAIProvider(cfg)

	resp, err := prov.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "key", Model: "gpt-4o-mini", MaxTokens: 100, APITimeout: 10 * time.Second}
	prov, _ := NewOpenAIProvider(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := prov.Chat(ctx, &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "k", Model: "default-model", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	_, err := prov.Chat(context.Background(), &ChatRequest{
		Model:     "override-model",
		MaxTokens: 77,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "k", Model: "default-model", MaxTokens: 100}
	prov, _ := NewOpenAIProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Model:     "override-model",
		MaxTokens: 88,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
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

	cfg := &Config{BaseURL: server.URL, APIKey: "k", Model: "gpt-4o-mini", MaxTokens: 100, MaxRetries: 3}
	prov, _ := NewOpenAIProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var text strings.Builder
	for evt := range ch {
		switch evt.Type {
		case StreamEventText:
			text.WriteString(evt.Content)
		case StreamEventError:
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

	cfg := &Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := NewAnthropicProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather and time?"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var calls []ToolCall
	for evt := range ch {
		if evt.Type == StreamEventToolCall && evt.ToolCall != nil {
			calls = append(calls, *evt.ToolCall)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 parallel tool calls, got %d", len(calls))
	}

	byName := make(map[string]ToolCall, len(calls))
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

	cfg := &Config{
		Provider:  "anthropic",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
	}
	prov, _ := NewAnthropicProvider(cfg)

	ch, err := prov.ChatStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var gotErr error
	for evt := range ch {
		if evt.Type == StreamEventError {
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
