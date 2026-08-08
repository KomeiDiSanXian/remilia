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

func TestStripMentionMarkup(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"@123456 帮我看看", " 帮我看看"},            // onebot/QQ 群
		{"<@123> 帮我看看", " 帮我看看"},             // Discord 用户
		{"<@!123> hi", " hi"},                // Discord 移动端用户
		{"<@&456> 公告", " 公告"},                // Discord 角色
		{"@everyone 开会", " 开会"},              // Discord @全体
		{"@all 注意", " 注意"},                   // onebot @全体
		{"帮我查 @123 的天气", "帮我查  的天气"},         // 中间位置的提及（留双空格，不影响语义）
		{"<#789> 频道消息", " 频道消息"},             // Discord 频道引用
		{"@username 保留", "@username 保留"},     // Telegram 昵称（字母）不误伤
		{"联系我 abc@qq.com", "联系我 abc@qq.com"}, // 邮箱不被误伤
	}
	for _, c := range cases {
		if got := stripMentionMarkup(c.in); got != c.want {
			t.Errorf("stripMentionMarkup(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanMessageStripsMentions(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	// onebot 场景：@机器人QQ号 + @他人，触发前缀 /ai
	got := p.cleanMessage("/ai @10001 帮我 @123 查天气")
	if strings.Contains(got, "@") {
		t.Errorf("cleanMessage should strip all mention markup, got %q", got)
	}
	if !strings.Contains(got, "帮我") {
		t.Errorf("cleanMessage should keep the real content, got %q", got)
	}
	// 纯 @ 提及的消息应被清空
	if got := p.cleanMessage("@10001 @123"); got != "" {
		t.Errorf("expected empty content after stripping mentions, got %q", got)
	}
}

func TestAppendMentionInfo(t *testing.T) {
	// 仅 @ 机器人自身：不追加提及信息
	content := appendMentionInfo("你好", []platform.UserInfo{
		{ID: "bot1", DisplayName: "Bot", IsSelf: true},
	})
	if content != "你好" {
		t.Errorf("expected no mention info when only bot is mentioned, got %q", content)
	}

	// @ 了其他人：追加结构化信息，昵称优先
	content = appendMentionInfo("帮我问问他们", []platform.UserInfo{
		{ID: "bot1", DisplayName: "Bot", IsSelf: true},
		{ID: "u1", DisplayName: "小明"},
		{ID: "u2"}, // 无昵称，回退 ID
	})
	if !strings.Contains(content, "小明") || !strings.Contains(content, "u2") {
		t.Errorf("expected mentioned users in content, got %q", content)
	}
	if strings.Contains(content, "Bot") {
		t.Errorf("bot self should be excluded from mention info, got %q", content)
	}
	if !strings.Contains(content, "本条消息 @ 提及了") {
		t.Errorf("expected mention info marker, got %q", content)
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

func TestBuildRuntimeContextGroupInfo(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"/test",
		platform.WithSyntheticSender(platform.UserInfo{
			ID: "user1", DisplayName: "小明", GroupRole: platform.GroupRoleAdmin,
		}),
		platform.WithSyntheticChat(platform.ChatInfo{
			ID: "g1", Name: "测试群", IsGroup: true, ParentID: "server1",
		}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{cfg: &Config{}}
	runtime := p.buildRuntimeContext(ctx)

	for _, want := range []string{
		"聊天类型: 群聊",
		"群 ID: g1",
		"群名称: 测试群",
		"所属服务器 ID: server1",
		"发送者群角色: 管理员",
	} {
		if !strings.Contains(runtime, want) {
			t.Errorf("runtime context should contain %q, got:\n%s", want, runtime)
		}
	}
}

func TestGroupRoleName(t *testing.T) {
	cases := []struct {
		role platform.GroupRole
		want string
	}{
		{platform.GroupRoleOwner, "群主/所有者"},
		{platform.GroupRoleAdmin, "管理员"},
		{platform.GroupRoleMember, "普通成员"},
		{platform.GroupRoleUnknown, "未知"},
		{platform.GroupRole(99), "未知"},
	}
	for _, c := range cases {
		if got := groupRoleName(c.role); got != c.want {
			t.Errorf("groupRoleName(%v) = %q, want %q", c.role, got, c.want)
		}
	}
}

func TestBuildRuntimeContextFieldFilter(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"/test",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user1", DisplayName: "小明", GroupRole: platform.GroupRoleAdmin}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", Name: "测试群", IsGroup: true, ParentID: "server1"}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// 只注入用户昵称 + 群名称
	p := &Plugin{cfg: &Config{ContextFields: []string{"user_name", "chat_name"}}}
	runtime := p.buildRuntimeContext(ctx)
	if !strings.Contains(runtime, "用户昵称: 小明") {
		t.Errorf("expected user_name injected, got:\n%s", runtime)
	}
	if !strings.Contains(runtime, "群名称: 测试群") {
		t.Errorf("expected chat_name injected, got:\n%s", runtime)
	}
	// 未列出的字段不应出现
	for _, forbidden := range []string{"用户 ID", "群 ID", "平台", "当前时间", "发送者群角色", "所属服务器 ID", "聊天类型"} {
		if strings.Contains(runtime, forbidden) {
			t.Errorf("field %q should be filtered out, got:\n%s", forbidden, runtime)
		}
	}
}

func TestBuildRuntimeContextAllFieldsDefault(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"/test",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user1", DisplayName: "小明", GroupRole: platform.GroupRoleOwner}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", Name: "测试群", IsGroup: true, ParentID: "server1"}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// ContextFields 为空 = 注入全部字段（默认行为）
	p := &Plugin{cfg: &Config{}}
	runtime := p.buildRuntimeContext(ctx)
	for _, want := range []string{"用户 ID: user1", "群 ID: g1", "发送者群角色: 群主/所有者", "所属服务器 ID: server1"} {
		if !strings.Contains(runtime, want) {
			t.Errorf("expected %q in runtime context, got:\n%s", want, runtime)
		}
	}
}

func TestBuildSystemPromptGatesRuntimeContext(t *testing.T) {
	evt := platform.NewSyntheticEvent(platform.EventKind("c2c"), "/test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// 开启（默认）：包含运行时上下文
	p := &Plugin{cfg: &Config{IncludeRuntimeContext: true}}
	prompt := p.buildSystemPrompt(ctx, nil)
	if !strings.Contains(prompt, "运行时上下文") {
		t.Error("expected runtime context section when enabled")
	}

	// 关闭：不包含运行时上下文，但保留框架与自定义提示
	p2 := &Plugin{cfg: &Config{IncludeRuntimeContext: false, SystemPrompt: "自定义"}}
	prompt2 := p2.buildSystemPrompt(ctx, nil)
	if strings.Contains(prompt2, "运行时上下文") {
		t.Error("expected runtime context section omitted when disabled")
	}
	if !strings.Contains(prompt2, "自定义") {
		t.Error("expected custom system prompt still present")
	}
	if !strings.Contains(prompt2, DefaultFrameworkPrompt) {
		t.Error("expected framework prompt still present")
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
