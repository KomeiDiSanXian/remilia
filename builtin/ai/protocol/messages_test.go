package protocol

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
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
	data, err := jsonx.Marshal(openaiMsgs[0].Content)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !jsonx.Valid(data) {
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
	text := &openaiMessageContent{text: "hello"}
	data, err := text.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `"hello"` {
		t.Errorf("expected %q, got %q", `"hello"`, string(data))
	}

	multi := &openaiMessageContent{parts: []openaiContentPart{{Type: "text", Text: "hello"}}}
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

// TestAnthropicMidArraySystemIsPreservedInPlace 冻结 Anthropic 适配器对
// **中途系统消息**的新行为：数组内的 system 不再被静默丢弃，而是原位保留。
//
// 旧实现 `case RoleSystem: continue` 把非首条 system 整条扔掉，等于"对话
// 中途追加的系统指令对 Claude 完全不可见"。Anthropic 自 2026 起正式支持
// mid-conversation system messages（见 docs/notes/33），因此这里断言的是
// "按角色状态机原样输出"：
//   - 只有首条 system 进入顶级 system 字段（由 extractAnthropicSystem 提取）；
//   - 其余 system 原位成条，不参与 user 合并，也不被丢弃。
func TestAnthropicMidArraySystemIsPreservedInPlace(t *testing.T) {
	const env = "<environment>\n当前日期: 2026-11-17 (星期二)\n</environment>"
	msgs := []Message{
		{Role: RoleSystem, Content: "stable system"},
		{Role: RoleUser, Content: "第一问"},
		{Role: RoleAssistant, Content: "第一答"},
		{Role: RoleSystem, Content: env},
		{Role: RoleUser, Content: "第二问"},
	}

	if got := extractAnthropicSystem(msgs); got != "stable system" {
		t.Errorf("only the first system message feeds the top-level field, got %q", got)
	}

	out := toAnthropicMessages(msgs)
	if len(out) != 4 {
		t.Fatalf("mid-array system must be kept as its own message, got %d: %+v", len(out), out)
	}
	if out[2].Role != anthropicRoleSystem {
		t.Errorf("third message must be the mid-conversation system, got %q", out[2].Role)
	}
	if len(out[2].Content) != 1 || !strings.Contains(out[2].Content[0].Text, "当前日期") {
		t.Errorf("system content must survive unchanged, got %+v", out[2].Content)
	}
	if out[3].Role != anthropicRoleUser || out[3].Content[0].Text != "第二问" {
		t.Errorf("the user turn after the system message must follow it, got %+v", out[3])
	}
	// 中途 system 不得被合并进相邻的 user 消息（合并会改变它的角色语义）。
	for _, m := range out[2].Content {
		if strings.Contains(m.Text, "第二问") {
			t.Errorf("mid-array system must not absorb user content, got %+v", m)
		}
	}
}

// TestAnthropicDropsFirstArraySystem 冻结"首条 system 只进顶级字段"：
// 它不得再作为数组内消息重复出现（否则模型会看到两份系统指令）。
func TestAnthropicDropsFirstArraySystem(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "stable system"},
		{Role: RoleUser, Content: "问题"},
	}
	out := toAnthropicMessages(msgs)
	if len(out) != 1 || out[0].Role != anthropicRoleUser {
		t.Fatalf("leading system must not appear in messages, got %+v", out)
	}
}

// TestAnthropicMergesConsecutiveUserTail 冻结连续 user 的合并语义：
// 尾部的 user 消息（如执行计划、反思指令）与相邻 user 轮合并为一条，
// 内容以 text block 完整送达——这是 API 硬性要求 roles 严格交替。
//
// 执行计划之所以用 user 角色注入（而非 system），原因见该用例：
// 同一条 user 消息里的附加文本对所有端点都成立，而 system 的位置约束
// 取决于端点能力（见 docs/notes/33）。
func TestAnthropicMergesConsecutiveUserTail(t *testing.T) {
	const plan = "===== 当前执行计划 =====\n- s1 收集资料"
	msgs := []Message{
		{Role: RoleSystem, Content: "stable system"},
		{Role: RoleUser, Content: "开始"},
		{Role: RoleUser, Content: plan},
	}

	out := toAnthropicMessages(msgs)
	if len(out) != 1 {
		t.Fatalf("consecutive user messages must merge into one (roles must alternate), got %d messages: %+v", len(out), out)
	}
	merged := out[0]
	if merged.Role != anthropicRoleUser {
		t.Errorf("merged message must keep the user role, got %q", merged.Role)
	}
	if len(merged.Content) != 2 {
		t.Fatalf("both text payloads must be preserved as separate blocks, got %+v", merged.Content)
	}
	if merged.Content[0].Type != anthropicBlockText || merged.Content[0].Text != "开始" {
		t.Errorf("first block must be the original user text, got %+v", merged.Content[0])
	}
	if merged.Content[1].Type != anthropicBlockText || !strings.Contains(merged.Content[1].Text, "当前执行计划") {
		t.Errorf("plan text must reach Anthropic unchanged, got %+v", merged.Content[1])
	}
}

// TestToAnthropicMessagesMergesParallelToolResults 冻结并行工具结果的合并行为：
// 同一 assistant 回合的全部 tool_result 必须放进紧随其后的同一条 user 消息
// （官方 SDK 同样如此合并）；拆成相邻多条 user 消息会被 API 以
// "roles must alternate" 400 拒绝。
func TestToAnthropicMessagesMergesParallelToolResults(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "查一下天气"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "t1", Name: "get_weather", Arguments: map[string]any{}},
			{ID: "t2", Name: "get_weather", Arguments: map[string]any{}},
		}},
		{Role: RoleTool, ToolCallID: "t1", Content: "北京 晴"},
		{Role: RoleTool, ToolCallID: "t2", Content: "上海 雨"},
	}

	out := toAnthropicMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (user, assistant, merged tool results), got %d: %+v", len(out), out)
	}
	last := out[2]
	if last.Role != "user" {
		t.Fatalf("tool results must ride in a user message, got %q", last.Role)
	}
	if len(last.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks in one message, got %d: %+v", len(last.Content), last.Content)
	}
	if last.Content[0].Type != "tool_result" || last.Content[0].ID != "t1" || last.Content[0].Text != "北京 晴" {
		t.Errorf("first tool_result wrong: %+v", last.Content[0])
	}
	if last.Content[1].Type != "tool_result" || last.Content[1].ID != "t2" || last.Content[1].Text != "上海 雨" {
		t.Errorf("second tool_result wrong: %+v", last.Content[1])
	}
}
