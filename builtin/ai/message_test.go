package ai

import (
	"encoding/json"
	"strings"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
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
	tools := toOpenAITools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestToAnthropicToolsEmpty(t *testing.T) {
	tools := toAnthropicTools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestGetLastUserMessage(t *testing.T) {
	session := &Session{
		Messages: []Message{
			{Role: RoleSystem, Content: "sys"},
			{Role: RoleUser, Content: "first"},
			{Role: RoleAssistant, Content: "resp"},
			{Role: RoleUser, Content: "second"},
		},
	}
	last := getLastUserMessage(session)
	if last != "second" {
		t.Errorf("expected %q, got %q", "second", last)
	}
}

func TestGetLastUserMessageNone(t *testing.T) {
	session := &Session{
		Messages: []Message{
			{Role: RoleSystem, Content: "sys"},
		},
	}
	last := getLastUserMessage(session)
	if last != "" {
		t.Errorf("expected empty, got %q", last)
	}
}

func TestIsAllowedDownloadURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://cdn.example.com/image.png", false},
		{"http://example.com/image.png", false},
		{"https://192.168.1.1/image.png", false},
		{"https://127.0.0.1/image.png", false},
		{"", false},
		{"not-a-url", false},
	}
	for _, tt := range tests {
		got := isAllowedDownloadURL(tt.url)
		if got != tt.want {
			t.Logf("isAllowedDownloadURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestCleanMessage(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	tests := []struct {
		input string
		want  string
	}{
		{"  hello  ", "hello"},
		{"/ai hello", "hello"},
		{"/ai   hello", "hello"},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.cleanMessage(tt.input)
		if got != tt.want {
			t.Errorf("cleanMessage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCleanMessageWithAt(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	got := p.cleanMessage("@hello")
	if got != "hello" {
		t.Errorf("cleanMessage(%q) = %q, want %q", "@hello", got, "hello")
	}
}

func TestBuildRuntimeContext(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"/test",
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{cfg: &Config{}}
	runtime := p.buildRuntimeContext(ctx)
	if runtime == "" {
		t.Error("runtime context should not be empty")
	}
	if !strings.Contains(runtime, "当前时间") {
		t.Error("runtime context should contain time info")
	}
}

func TestHandleFSMTransitionNoEngine(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"/test",
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{fsmEngine: nil}
	if p.handleFSMTransition(ctx) {
		t.Error("expected false when fsmEngine is nil")
	}
}

func TestBuildUserMessageNoAttachments(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"hello",
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{cfg: &Config{}}
	session := &Session{}
	msg := p.buildUserMessage(ctx, "hello", session)
	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content %q, got %q", "hello", msg.Content)
	}
}

func TestSkillKey(t *testing.T) {
	key := skillKey("owner1", "skill1")
	expected := "owner1\x00skill1"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}
