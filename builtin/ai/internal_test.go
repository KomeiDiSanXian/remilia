package ai

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestIsCommandSafeForAI(t *testing.T) {
	tests := []struct {
		name string
		cmd  engine.CommandInfo
		want bool
	}{
		{
			name: "safe command",
			cmd:  engine.CommandInfo{Command: "/ping"},
			want: true,
		},
		{
			name: "ai command",
			cmd:  engine.CommandInfo{Command: "/ai"},
			want: false,
		},
		{
			name: "empty name after trim",
			cmd:  engine.CommandInfo{Command: "/"},
			want: false,
		},
		{
			name: "with permissions",
			cmd:  engine.CommandInfo{Command: "/admin", Permissions: []string{"admin"}},
			want: false,
		},
		{
			name: "with definition permissions",
			cmd: engine.CommandInfo{
				Command:    "/secret",
				Definition: &command.Definition{Permissions: []string{"secret"}},
			},
			want: false,
		},
		{
			name: "hidden command",
			cmd: engine.CommandInfo{
				Command:    "/hidden",
				Definition: &command.Definition{Hidden: true},
			},
			want: true, // isCommandSafeForAI doesn't check Hidden
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCommandSafeForAI(tt.cmd)
			if got != tt.want {
				t.Errorf("isCommandSafeForAI(%+v) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestBuildToolFromCommand(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/test_cmd", Description: "A test command"}
	tool := buildToolFromCommand(cmd)
	if tool == nil {
		t.Fatal("buildToolFromCommand returned nil")
	}
	if tool.Name != "test_cmd" {
		t.Errorf("expected name %q, got %q", "test_cmd", tool.Name)
	}
	if tool.Description != "A test command" {
		t.Errorf("expected description %q, got %q", "A test command", tool.Description)
	}
	if len(tool.Categories) != 1 || tool.Categories[0] != CategoryGeneral {
		t.Errorf("expected [general] categories, got %v", tool.Categories)
	}

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "/test_cmd") {
		t.Errorf("expected result to contain command name, got %q", result)
	}
}

func TestBuildToolFromCommandEmpty(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/"}
	tool := buildToolFromCommand(cmd)
	if tool != nil {
		t.Error("expected nil for empty command")
	}
}

func TestBuildToolFromCommandNoDescription(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/no_desc"}
	tool := buildToolFromCommand(cmd)
	if tool == nil {
		t.Fatal("buildToolFromCommand returned nil")
	}
	if !strings.Contains(tool.Description, "/no_desc") {
		t.Errorf("expected description to contain command, got %q", tool.Description)
	}
}

func TestIsSafeCommandArg(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"hello world", true},
		{"", true},
		{"abc123", true},
		{"\t", true},
		{"\n", false},
		{"\x00", false},
		{"\x1b", false},
		{"\x7F", false},
		{"中文", false},
		{string(make([]byte, 4097)), false},
	}
	for _, tt := range tests {
		got := isSafeCommandArg(tt.arg)
		if got != tt.want {
			t.Errorf("isSafeCommandArg(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}

func TestIsCommandMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"/ping", true},
		{"!cmd", true},
		{"!!cmd", true},
		{"hello", false},
		{"", false},
		{"   ", false},
		{"/", true},
	}
	for _, tt := range tests {
		got := isCommandMessage(tt.msg)
		if got != tt.want {
			t.Errorf("isCommandMessage(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestFormatAIError(t *testing.T) {
	tests := []struct {
		err  string
		want string
	}{
		{"API returned 401", "API 认证失败，请检查 api_key 配置"},
		{"error 401", "API 认证失败，请检查 api_key 配置"},
		{"API returned 404", "API 地址或模型名称错误，请检查 base_url 和 model 配置"},
		{"error 429", "请求过于频繁，请稍后再试"},
		{"context deadline exceeded", "请求超时，请检查网络连接或增大超时配置"},
		{"connection refused", "无法连接 API 服务器，请检查 base_url 配置"},
		{"no such host", "API 域名解析失败，请检查 base_url 配置"},
		{"unknown error", "AI 处理出错，请稍后再试"},
	}
	for _, tt := range tests {
		err := formatAIError(errFromString(tt.err))
		if err != tt.want {
			t.Errorf("formatAIError(%q) = %q, want %q", tt.err, err, tt.want)
		}
	}
}

func TestCollectToolCategories(t *testing.T) {
	tools := []Tool{
		{Categories: []string{"general"}},
		{Categories: []string{"weather"}},
		{Categories: []string{"weather", "science"}},
		{},
	}
	cats := collectToolCategories(tools)
	if len(cats) != 3 {
		t.Errorf("expected 3 unique categories, got %d: %v", len(cats), cats)
	}
}

func TestCollectToolCategoriesAllEmpty(t *testing.T) {
	tools := []Tool{{}, {}, {}}
	cats := collectToolCategories(tools)
	if len(cats) != 1 || cats[0] != CategoryGeneral {
		t.Errorf("expected [general], got %v", cats)
	}
}

func TestContainsCategory(t *testing.T) {
	cats := []string{"general", "weather", "admin"}
	if !containsCategory(cats, "general") {
		t.Error("expected to find general")
	}
	if containsCategory(cats, "nonexistent") {
		t.Error("should not find nonexistent category")
	}
}

func TestFilterToolsByCategory(t *testing.T) {
	tools := []Tool{
		{Name: "a", Categories: []string{"general"}},
		{Name: "b", Categories: []string{"weather"}},
		{Name: "c", Categories: []string{"general", "weather"}},
		{Name: "d"},
	}
	filtered := filterToolsByCategory(tools, "weather")
	// weather 分类工具 (b, c) + 通用工具始终保留 (a, d)
	if len(filtered) != 4 {
		t.Errorf("expected 4 tools (weather + general fallback), got %d", len(filtered))
	}
}

func TestFilterToolsByCategoryGeneral(t *testing.T) {
	tools := []Tool{
		{Name: "a", Categories: []string{"general"}},
		{Name: "b"},
		{Name: "c", Categories: []string{"admin"}},
	}
	filtered := filterToolsByCategory(tools, "general")
	if len(filtered) != 2 {
		t.Errorf("expected 2 tools in general, got %d", len(filtered))
	}
}

func TestToolHasCategory(t *testing.T) {
	if !toolHasCategory(Tool{Categories: []string{"weather"}}, "weather") {
		t.Error("should match explicit category")
	}
	if toolHasCategory(Tool{Categories: []string{"admin"}}, "weather") {
		t.Error("should not match wrong category")
	}
	if !toolHasCategory(Tool{}, "general") {
		t.Error("empty categories should match general")
	}
	if toolHasCategory(Tool{}, "weather") {
		t.Error("empty categories should not match non-general")
	}
	if !toolHasCategory(Tool{Categories: []string{"general"}}, "general") {
		t.Error("general should match general")
	}
}

func TestBuildCategorySelectTool(t *testing.T) {
	cats := []string{"general", "weather", "admin"}
	tool := buildCategorySelectTool(cats)
	if tool.Name != categorySelectToolName {
		t.Errorf("expected name %q, got %q", categorySelectToolName, tool.Name)
	}
	if tool.Parameters.Properties["category"].Type != "string" {
		t.Errorf("expected category param type string")
	}
	if len(tool.Parameters.Properties["category"].Enum) != 3 {
		t.Errorf("expected 3 enum values, got %d", len(tool.Parameters.Properties["category"].Enum))
	}

	result, err := tool.Execute(context.Background(), map[string]any{"category": "weather"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "weather" {
		t.Errorf("expected %q, got %q", "weather", result)
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"192.168.1.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"169.254.1.1", false},
		{"224.0.0.1", false},
		{"::1", false},
		{"fe80::1", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := isPublicIP(ip)
		if got != tt.want {
			t.Errorf("isPublicIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestIsPublicIPNil(t *testing.T) {
	if isPublicIP(nil) {
		t.Error("nil IP should not be public")
	}
}

func TestMakeSessionID(t *testing.T) {
	id := makeSessionID("discord", "123", "456")
	if id != "discord:123:456" {
		t.Errorf("expected %q, got %q", "discord:123:456", id)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h 30m"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
		{0, "0m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestExtractSkillDescription(t *testing.T) {
	tests := []struct {
		prompt string
		want   string
	}{
		{"First line\nSecond line", "First line"},
		{"Single line", "Single line"},
		{"", ""},
		{"   Trimmed   ", "Trimmed"},
	}
	for _, tt := range tests {
		got := extractSkillDescription(tt.prompt)
		if got != tt.want {
			t.Errorf("extractSkillDescription(%q) = %q, want %q", tt.prompt, got, tt.want)
		}
	}
}

func TestExtractSkillDescriptionLong(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := extractSkillDescription(long)
	if len(got) > 203 {
		t.Errorf("description too long: %d chars", len(got))
	}
}

func TestCaptureSender(t *testing.T) {
	cs := &captureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Text: "hello world",
		},
	}
	_, err := cs.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if cs.capturedText != "hello world" {
		t.Errorf("expected captured text %q, got %q", "hello world", cs.capturedText)
	}
}

func TestCaptureSenderAttachments(t *testing.T) {
	cs := &captureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Text: "with attachment",
			Attachments: []platform.Attachment{
				{URL: "http://example.com/img.png"},
			},
		},
	}
	cs.Send(context.Background(), req)
	if len(cs.capturedAttachments) != 1 {
		t.Errorf("expected 1 captured attachment, got %d", len(cs.capturedAttachments))
	}
}

func TestCaptureSenderMarkdownFallback(t *testing.T) {
	cs := &captureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Markdown: "**markdown**",
		},
	}
	cs.Send(context.Background(), req)
	if cs.capturedText != "**markdown**" {
		t.Errorf("expected captured markdown %q, got %q", "**markdown**", cs.capturedText)
	}
}

func TestInferPartType(t *testing.T) {
	tests := []struct {
		mime string
		want ContentPartType
	}{
		{"image/jpeg", ContentPartImage},
		{"image/png", ContentPartImage},
		{"audio/wav", ContentPartAudio},
		{"audio/mpeg", ContentPartAudio},
		{"text/plain", ""},
		{"application/json", ""},
	}
	for _, tt := range tests {
		got := inferPartType(tt.mime)
		if got != tt.want {
			t.Errorf("inferPartType(%q) = %q, want %q", tt.mime, got, tt.want)
		}
	}
}

func TestInferAudioFormat(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"audio/wav", "wav"},
		{"audio/wave", "wav"},
		{"audio/x-wav", "wav"},
		{"audio/mpeg", "mp3"},
		{"audio/mp3", "mp3"},
		{"audio/L16", "pcm"},
		{"audio/l16", "pcm"},
		{"audio/ogg", ""},
		{"audio/flac", ""},
	}
	for _, tt := range tests {
		got := inferAudioFormat(tt.mime)
		if got != tt.want {
			t.Errorf("inferAudioFormat(%q) = %q, want %q", tt.mime, got, tt.want)
		}
	}
}

func TestBuildHealthProbeURL(t *testing.T) {
	tests := []struct {
		cfg  Config
		want string
	}{
		{Config{Provider: "openai", BaseURL: "https://api.openai.com/v1"}, "https://api.openai.com/v1/models"},
		{Config{Provider: "anthropic", BaseURL: "https://api.anthropic.com"}, "https://api.anthropic.com/v1/messages"},
		{Config{Provider: "anthropic", BaseURL: "https://api.anthropic.com/v1"}, "https://api.anthropic.com/v1/messages"},
	}
	for _, tt := range tests {
		got := buildHealthProbeURL(&tt.cfg)
		if got != tt.want {
			t.Errorf("buildHealthProbeURL(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}

func TestUserSkillNamePattern(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"my_skill", true},
		{"skill-123", true},
		{"a", true},
		{"", false},
		{"a b", false},
		{"abc!", false},
		{"中文", false},
	}
	for _, tt := range tests {
		got := userSkillNamePattern.MatchString(tt.name)
		if got != tt.want {
			t.Errorf("userSkillNamePattern.MatchString(%q) = %v, want %v", tt.name, got, tt.want)
		}
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
	var calls []openaiToolCall

	mergeOrAppendToolCall(&calls, openaiToolCall{
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

	mergeOrAppendToolCall(&calls, openaiToolCall{
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

func TestApplyDefaultParamSchema(t *testing.T) {
	p := &Plugin{
		skillReg: NewSkillRegistry(),
	}

	skill := Skill{
		Name:        "test",
		OwnerID:     OwnerSystem,
		Description: "test",
	}
	p.applyDefaultParamSchema(&skill)
	if len(skill.Parameters.Properties) == 0 {
		t.Error("expected default parameters to be applied")
	}
	if _, ok := skill.Parameters.Properties["query"]; !ok {
		t.Error("expected query parameter in default schema")
	}
}

func TestApplyDefaultParamSchemaExisting(t *testing.T) {
	p := &Plugin{
		skillReg: NewSkillRegistry(),
	}

	skill := Skill{
		Name:        "test",
		OwnerID:     OwnerSystem,
		Description: "test",
		Parameters: ToolParamSchema{
			Type: "object",
			Properties: map[string]ToolParamSchema{
				"custom": {Type: "string"},
			},
		},
	}
	p.applyDefaultParamSchema(&skill)
	if _, ok := skill.Parameters.Properties["query"]; ok {
		t.Error("should not add default when custom params exist")
	}
}

// Helpers

type errString string

func (e errString) Error() string { return string(e) }

func errFromString(s string) error { return errString(s) }
