package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToOpenAIMessages(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "You are a helpful assistant"},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there!", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "test_tool", Arguments: map[string]any{"arg1": "val1"}},
		}},
		{Role: RoleTool, Content: "Tool result", ToolCallID: "call_1"},
	}

	openaiMsgs := toOpenAIMessages(msgs)
	if len(openaiMsgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(openaiMsgs))
	}
	if openaiMsgs[0].Role != "system" {
		t.Errorf("expected role system, got %q", openaiMsgs[0].Role)
	}
	if openaiMsgs[3].Role != "tool" {
		t.Errorf("expected role tool, got %q", openaiMsgs[3].Role)
	}
	if openaiMsgs[3].ToolCallID != "call_1" {
		t.Errorf("expected ToolCallID call_1, got %q", openaiMsgs[3].ToolCallID)
	}
}

func TestToOpenAIMessagesWithContentParts(t *testing.T) {
	msgs := []Message{
		{
			Role: RoleUser,
			ContentParts: []ContentPart{
				{Type: ContentPartText, Text: "describe this image"},
				{Type: ContentPartImage, Data: []byte("fake-image-data"), MimeType: "image/jpeg"},
			},
		},
	}

	openaiMsgs := toOpenAIMessages(msgs)
	if len(openaiMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(openaiMsgs))
	}
	data, err := json.Marshal(openaiMsgs[0].Content)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !json.Valid(data) {
		t.Error("invalid JSON for Content with parts")
	}
}

func TestToOpenAIMessagesEmptyToolCallID(t *testing.T) {
	msgs := []Message{
		{Role: RoleTool, Content: "result", ToolCallID: ""},
	}
	openaiMsgs := toOpenAIMessages(msgs)
	if len(openaiMsgs) != 0 {
		t.Errorf("expected 0 messages for tool with empty ToolCallID, got %d", len(openaiMsgs))
	}
}

func TestToOpenAIMessagesEmptyToolCallName(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call_1", Name: ""},
		}},
	}
	openaiMsgs := toOpenAIMessages(msgs)
	if len(openaiMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(openaiMsgs))
	}
	if len(openaiMsgs[0].ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls after filtering empty name, got %d", len(openaiMsgs[0].ToolCalls))
	}
}

func TestBuildOpenAIContentParts(t *testing.T) {
	parts := []ContentPart{
		{Type: ContentPartText, Text: "hello"},
		{Type: ContentPartImage, Data: []byte("img"), MimeType: "image/png"},
		{Type: ContentPartAudio, Data: []byte("au"), MimeType: "audio/wav", AudioFormat: "wav"},
		{Type: ContentPartImage, Data: nil},
		{Type: ContentPartAudio, Data: []byte("au"), AudioFormat: ""},
	}

	out := buildOpenAIContentParts(parts)
	if len(out) != 3 {
		t.Errorf("expected 3 parts, got %d", len(out))
	}
	if out[0].Type != "text" || out[0].Text != "hello" {
		t.Errorf("expected text part 'hello', got %+v", out[0])
	}
}

func TestToAnthropicMessages(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "You are Claude"},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi!", ToolCalls: []ToolCall{
			{ID: "toolu_1", Name: "get_weather", Arguments: map[string]any{"city": "Beijing"}},
		}},
		{Role: RoleTool, Content: "Sunny", ToolCallID: "toolu_1"},
	}

	anthropicMsgs := toAnthropicMessages(msgs)
	if len(anthropicMsgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(anthropicMsgs))
	}
	if anthropicMsgs[0].Role != "user" {
		t.Errorf("expected role user, got %q", anthropicMsgs[0].Role)
	}
	if len(anthropicMsgs[1].Content) != 2 {
		t.Errorf("expected 2 content blocks, got %d", len(anthropicMsgs[1].Content))
	}
}

func TestToAnthropicMessagesWithContentParts(t *testing.T) {
	msgs := []Message{
		{
			Role: RoleUser,
			ContentParts: []ContentPart{
				{Type: ContentPartText, Text: "what's in this image"},
				{Type: ContentPartImage, Data: []byte("img-data"), MimeType: "image/png"},
				{Type: ContentPartAudio, Data: []byte("audio-data"), MimeType: "audio/wav"},
			},
		},
	}

	anthropicMsgs := toAnthropicMessages(msgs)
	if len(anthropicMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(anthropicMsgs))
	}
	if len(anthropicMsgs[0].Content) != 2 {
		t.Errorf("expected 2 content blocks (audio skipped), got %d", len(anthropicMsgs[0].Content))
	}
}

func TestToAnthropicUserBlocks(t *testing.T) {
	m := Message{Content: "just text"}
	blocks := toAnthropicUserBlocks(m)
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Errorf("expected 1 text block, got %+v", blocks)
	}

	m2 := Message{}
	blocks2 := toAnthropicUserBlocks(m2)
	if blocks2 != nil {
		t.Errorf("expected nil for empty message, got %+v", blocks2)
	}
}

func TestExtractAnthropicSystem(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleAssistant, Content: "ok"},
	}
	sys := extractAnthropicSystem(msgs)
	if sys != "system prompt" {
		t.Errorf("expected %q, got %q", "system prompt", sys)
	}
}

func TestExtractAnthropicSystemNone(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
	}
	sys := extractAnthropicSystem(msgs)
	if sys != "" {
		t.Errorf("expected empty, got %q", sys)
	}
}

func TestToOpenAIToolsEmpty(t *testing.T) {
	tools := ToOpenAITools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestToAnthropicToolsEmpty(t *testing.T) {
	tools := ToAnthropicTools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestOpenAIMessageContentMarshalUnmarshal(t *testing.T) {
	text := newOpenAITextContent("hello")
	data, err := text.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `"hello"` {
		t.Errorf("expected %q, got %q", `"hello"`, string(data))
	}

	multi := newOpenAIMultiContent([]openaiContentPart{
		{Type: "text", Text: "hello"},
	})
	data, err = multi.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected data to contain hello, got %q", string(data))
	}

	if text.String() != "hello" {
		t.Errorf("expected %q, got %q", "hello", text.String())
	}
	if (*openaiMessageContent)(nil).String() != "" {
		t.Error("nil String should return empty")
	}

	var unmarshaled openaiMessageContent
	err = unmarshaled.UnmarshalJSON([]byte(`"world"`))
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}
	if unmarshaled.text != "world" {
		t.Errorf("expected text %q, got %q", "world", unmarshaled.text)
	}

	var nullMsg openaiMessageContent
	err = nullMsg.UnmarshalJSON([]byte(`null`))
	if err != nil {
		t.Fatalf("UnmarshalJSON null failed: %v", err)
	}
}

func TestMergeOrAppendToolCall(t *testing.T) {
	var calls []OpenAIToolCall

	mergeOrAppendToolCall(&calls, OpenAIToolCall{
		Index: 0,
		ID:    "call_1",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "get_weather", Arguments: `{"city":"`},
	})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	mergeOrAppendToolCall(&calls, OpenAIToolCall{
		Index: 0,
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `Beijing"}`},
	})
	if len(calls) != 1 {
		t.Fatalf("expected still 1 call, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `{"city":"Beijing"}` {
		t.Errorf("expected merged arguments, got %q", calls[0].Function.Arguments)
	}
}
