package ai

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestNewToolRegistry(t *testing.T) {
	r := NewToolRegistry()
	if r == nil {
		t.Fatal("NewToolRegistry returned nil")
	}
	if len(r.List()) != 0 {
		t.Error("new registry should be empty")
	}
}

func TestToolRegistryRegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	tool := Tool{
		Name:        "test_tool",
		Description: "a test tool",
		Categories:  []string{CategoryGeneral},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "done", nil
		},
	}
	r.Register(tool)

	got, ok := r.Get("test_tool")
	if !ok {
		t.Fatal("expected to find tool")
	}
	if got.Name != "test_tool" {
		t.Errorf("expected name %q, got %q", "test_tool", got.Name)
	}
	if got.Description != "a test tool" {
		t.Errorf("expected description %q, got %q", "a test tool", got.Description)
	}
}

func TestToolRegistryRegisterDuplicate(t *testing.T) {
	r := NewToolRegistry()
	tool := Tool{Name: "dup"}
	r.Register(tool)
	r.Register(tool) // should not panic or overwrite

	_, ok := r.Get("dup")
	if !ok {
		t.Error("tool should exist after duplicate register")
	}
}

func TestToolRegistryGetNotFound(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent tool")
	}
}

func TestToolRegistryList(t *testing.T) {
	r := NewToolRegistry()
	r.Register(Tool{Name: "a"})
	r.Register(Tool{Name: "b"})
	r.Register(Tool{Name: "c"})

	tools := r.List()
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestToolRegistryListCopy(t *testing.T) {
	r := NewToolRegistry()
	r.Register(Tool{Name: "original"})

	tools := r.List()
	tools[0].Name = "modified"

	got, _ := r.Get("original")
	if got.Name != "original" {
		t.Error("List should return a copy, not modify original")
	}
}

func TestToOpenAITools(t *testing.T) {
	tools := []Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"city": {Type: "string"},
				},
			},
		},
	}
	openaiTools := toOpenAITools(tools)
	if len(openaiTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(openaiTools))
	}
	if openaiTools[0].Function.Name != "get_weather" {
		t.Errorf("expected name %q, got %q", "get_weather", openaiTools[0].Function.Name)
	}
	if openaiTools[0].Type != "function" {
		t.Errorf("expected type %q, got %q", "function", openaiTools[0].Type)
	}
}

func TestToAnthropicTools(t *testing.T) {
	tools := []Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
		},
	}
	anthropicTools := toAnthropicTools(tools)
	if len(anthropicTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(anthropicTools))
	}
	if anthropicTools[0].Name != "get_weather" {
		t.Errorf("expected name %q, got %q", "get_weather", anthropicTools[0].Name)
	}
}

func TestParseOpenAIToolCalls(t *testing.T) {
	raw := []openaiToolCall{
		{
			ID:    "call_1",
			Type:  "function",
			Index: 0,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "get_weather",
				Arguments: `{"city":"Beijing"}`,
			},
		},
	}
	calls, err := parseOpenAIToolCalls(raw)
	if err != nil {
		t.Fatalf("parseOpenAIToolCalls failed: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Errorf("expected name %q, got %q", "get_weather", calls[0].Name)
	}
	if calls[0].ID != "call_1" {
		t.Errorf("expected ID %q, got %q", "call_1", calls[0].ID)
	}
	city, ok := calls[0].Arguments["city"].(string)
	if !ok || city != "Beijing" {
		t.Errorf("expected city=Beijing, got %v", calls[0].Arguments["city"])
	}
}

func TestParseOpenAIToolCallsInvalidJSON(t *testing.T) {
	raw := []openaiToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "test",
				Arguments: `{invalid json}`,
			},
		},
	}
	_, err := parseOpenAIToolCalls(raw)
	if err == nil {
		t.Error("expected error for invalid JSON arguments")
	}
}

func TestParseOpenAIToolCallsEmptyName(t *testing.T) {
	raw := []openaiToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "",
				Arguments: "",
			},
		},
	}
	calls, err := parseOpenAIToolCalls(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for empty name, got %d", len(calls))
	}
}

func TestParseOpenAIToolCallsEmptyID(t *testing.T) {
	raw := []openaiToolCall{
		{
			Index: 0,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "test",
				Arguments: `{}`,
			},
		},
	}
	calls, err := parseOpenAIToolCalls(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ID == "" {
		t.Error("expected auto-generated ID for empty ID")
	}
}

func TestParseAnthropicToolCalls(t *testing.T) {
	blocks := []anthropicContentBlock{
		{Type: "text", Text: "Some text"},
		{
			Type: "tool_use",
			ID:   "toolu_1",
			Name: "get_weather",
			Input: map[string]any{
				"city": "Shanghai",
			},
		},
	}
	calls := parseAnthropicToolCalls(blocks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Errorf("expected name %q, got %q", "get_weather", calls[0].Name)
	}
}

func TestParseAnthropicToolCallsTextOnly(t *testing.T) {
	blocks := []anthropicContentBlock{
		{Type: "text", Text: "Just text"},
	}
	calls := parseAnthropicToolCalls(blocks)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for text-only, got %d", len(calls))
	}
}

func TestParseAnthropicToolCallsWithInputConversion(t *testing.T) {
	blocks := []anthropicContentBlock{
		{
			Type: "tool_use",
			ID:   "toolu_2",
			Name: "search",
			Input: map[string]any{
				"query": "test",
			},
		},
	}
	calls := parseAnthropicToolCalls(blocks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Arguments["query"] != "test" {
		t.Errorf("expected query=test, got %v", calls[0].Arguments["query"])
	}
}

func TestWithCallerInfo(t *testing.T) {
	ctx := context.Background()
	info := platform.UserInfo{ID: "user123", DisplayName: "TestUser"}
	newCtx := WithCallerInfo(ctx, info)

	got, ok := CallerInfoFromContext(newCtx)
	if !ok {
		t.Fatal("expected caller info in context")
	}
	if got.ID != "user123" {
		t.Errorf("expected ID %q, got %q", "user123", got.ID)
	}
	if got.DisplayName != "TestUser" {
		t.Errorf("expected DisplayName %q, got %q", "TestUser", got.DisplayName)
	}
}

func TestCallerInfoFromContextNoInfo(t *testing.T) {
	ctx := context.Background()
	_, ok := CallerInfoFromContext(ctx)
	if ok {
		t.Error("expected false when no caller info in context")
	}
}
