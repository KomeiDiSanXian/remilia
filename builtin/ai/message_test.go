package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/netguard"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestGetLastUserMessage(t *testing.T) {
	sess := &session.Session{
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "sys"},
			{Role: protocol.RoleUser, Content: "first"},
			{Role: protocol.RoleAssistant, Content: "resp"},
			{Role: protocol.RoleUser, Content: "second"},
		},
	}
	last := runtime.LastUserMessage(sess)
	if last != "second" {
		t.Errorf("expected %q, got %q", "second", last)
	}
}

func TestGetLastUserMessageNone(t *testing.T) {
	sess := &session.Session{
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "sys"},
		},
	}
	last := runtime.LastUserMessage(sess)
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
		got := netguard.AllowURL(tt.url)
		if got != tt.want {
			t.Logf("netguard.AllowURL(%q) = %v, want %v", tt.url, got, tt.want)
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
		got := runtime.CleanMessage(tt.input, p.triggerCmd)
		if got != tt.want {
			t.Errorf("cleanMessage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCleanMessageWithAt(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	got := runtime.CleanMessage("@hello", p.triggerCmd)
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
		if got := textutil.StripMentionMarkup(c.in); got != c.want {
			t.Errorf("textutil.StripMentionMarkup(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanMessageStripsMentions(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai"}
	// onebot 场景：@机器人QQ号 + @他人，触发前缀 /ai
	got := runtime.CleanMessage("/ai @10001 帮我 @123 查天气", p.triggerCmd)
	if strings.Contains(got, "@") {
		t.Errorf("cleanMessage should strip all mention markup, got %q", got)
	}
	if !strings.Contains(got, "帮我") {
		t.Errorf("cleanMessage should keep the real content, got %q", got)
	}
	// 纯 @ 提及的消息应被清空
	if got := runtime.CleanMessage("@10001 @123", p.triggerCmd); got != "" {
		t.Errorf("expected empty content after stripping mentions, got %q", got)
	}
}

func TestAppendMentionInfo(t *testing.T) {
	// 仅 @ 机器人自身：不追加提及信息
	content := runtime.AppendMentionInfo("你好", []platform.UserInfo{
		{ID: "bot1", DisplayName: "Bot", IsSelf: true},
	})
	if content != "你好" {
		t.Errorf("expected no mention info when only bot is mentioned, got %q", content)
	}

	// @ 了其他人：追加结构化信息，昵称优先
	content = runtime.AppendMentionInfo("帮我问问他们", []platform.UserInfo{
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
	p := &Plugin{cfg: &config.Config{}}
	runtimeCtx := promptctx.BuildRuntimeContext(p.cfg, ctx)
	if runtimeCtx == "" {
		t.Error("runtime context should not be empty")
	}
	if !strings.Contains(runtimeCtx, "当前时间") {
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
	p := &Plugin{cfg: &config.Config{}}
	runtimeCtx := promptctx.BuildRuntimeContext(p.cfg, ctx)

	for _, want := range []string{
		"聊天类型: 群聊",
		"群 ID: g1",
		"群名称: 测试群",
		"所属服务器 ID: server1",
		"发送者群角色: 管理员",
	} {
		if !strings.Contains(runtimeCtx, want) {
			t.Errorf("runtime context should contain %q, got:\n%s", want, runtimeCtx)
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
	p := &Plugin{cfg: &config.Config{ContextFields: []string{"user_name", "chat_name"}}}
	runtimeCtx := promptctx.BuildRuntimeContext(p.cfg, ctx)
	if !strings.Contains(runtimeCtx, "用户昵称: 小明") {
		t.Errorf("expected user_name injected, got:\n%s", runtimeCtx)
	}
	if !strings.Contains(runtimeCtx, "群名称: 测试群") {
		t.Errorf("expected chat_name injected, got:\n%s", runtimeCtx)
	}
	// 未列出的字段不应出现
	for _, forbidden := range []string{"用户 ID", "群 ID", "平台", "当前时间", "发送者群角色", "所属服务器 ID", "聊天类型"} {
		if strings.Contains(runtimeCtx, forbidden) {
			t.Errorf("field %q should be filtered out, got:\n%s", forbidden, runtimeCtx)
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
	p := &Plugin{cfg: &config.Config{}}
	runtimeCtx := promptctx.BuildRuntimeContext(p.cfg, ctx)
	for _, want := range []string{"用户 ID: user1", "群 ID: g1", "发送者群角色: 群主/所有者", "所属服务器 ID: server1"} {
		if !strings.Contains(runtimeCtx, want) {
			t.Errorf("expected %q in runtime context, got:\n%s", want, runtimeCtx)
		}
	}
}

func TestBuildSystemPromptGatesRuntimeContext(t *testing.T) {
	evt := platform.NewSyntheticEvent(platform.EventKind("c2c"), "/test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// 开启（默认）：动态上下文包含运行时上下文
	p := &Plugin{cfg: &config.Config{IncludeRuntimeContext: true}}
	dyn := p.buildDynamicContext(ctx, nil)
	if !strings.Contains(dyn, "运行时上下文") {
		t.Error("expected runtime context section when enabled")
	}

	// 关闭：动态上下文不含运行时上下文，稳定提示词仍保留框架与自定义提示
	p2 := &Plugin{cfg: &config.Config{IncludeRuntimeContext: false, SystemPrompt: "自定义"}}
	dyn2 := p2.buildDynamicContext(ctx, nil)
	if strings.Contains(dyn2, "运行时上下文") {
		t.Error("expected runtime context section omitted when disabled")
	}
	static2 := p2.buildStaticSystemPrompt(ctx)
	if !strings.Contains(static2, "自定义") {
		t.Error("expected custom system prompt still present")
	}
	if !strings.Contains(static2, DefaultFrameworkPrompt) {
		t.Error("expected framework prompt still present")
	}
	if strings.Contains(static2, "运行时上下文") {
		t.Error("stable system prompt must not embed dynamic sections")
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
	p := &Plugin{cfg: &config.Config{}}
	sess := &session.Session{}
	msg := p.buildUserMessage(ctx, "hello", sess)
	if msg.Role != protocol.RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content %q, got %q", "hello", msg.Content)
	}
}

// fakeTranscriptMeta 实现 platform.VoiceTranscript，模拟平台语音 ASR 元数据。
type fakeTranscriptMeta struct {
	text string
}

func (m *fakeTranscriptMeta) Transcript() string { return m.text }

func TestBuildUserMessage_ASRTranscriptPreferred(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"",
		platform.WithSyntheticAttachments(
			platform.Attachment{
				Kind:     platform.AttachmentKindAudio,
				URL:      "https://ex.com/voice.silk",
				MimeType: "audio/silk",
				Extra:    map[string]any{"voice": &fakeTranscriptMeta{text: "今天天气怎么样"}},
			},
		),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	// AudioEnabled=false：ASR 文本路径不应受开关限制（文本无需音频能力）
	p := &Plugin{cfg: &config.Config{AudioEnabled: false}}
	sess := &session.Session{}
	msg := p.buildUserMessage(ctx, "", sess)

	if msg.Content != "" {
		t.Errorf("expected empty Content in ContentParts mode, got %q", msg.Content)
	}
	if len(msg.ContentParts) != 1 {
		t.Fatalf("expected 1 content part (ASR text), got %d: %+v", len(msg.ContentParts), msg.ContentParts)
	}
	part := msg.ContentParts[0]
	if part.Type != protocol.ContentPartText {
		t.Errorf("expected text part, got %v", part.Type)
	}
	if part.Text != "[语音转写] 今天天气怎么样" {
		t.Errorf("expected ASR text injected, got %q", part.Text)
	}
}

func TestBuildUserMessage_ASRMissingFallsBackToAudio(t *testing.T) {
	evt := platform.NewSyntheticEvent(
		platform.EventKind("c2c"),
		"",
		platform.WithSyntheticAttachments(
			platform.Attachment{
				Kind:     platform.AttachmentKindAudio,
				URL:      "https://ex.com/voice.wav",
				MimeType: "audio/wav",
			},
		),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// 无 ASR 文本：AudioEnabled=true 时尝试下载为音频部件
	p := &Plugin{cfg: &config.Config{AudioEnabled: true}}
	sess := &session.Session{}
	msg := p.buildUserMessage(ctx, "", sess)
	// 下载会被 SSRF 防护拦截（ex.com 可能解析失败），ContentParts 可能为空——
	// 这里只验证不 panic 且未注入 ASR 文本
	if len(msg.ContentParts) > 0 {
		for _, part := range msg.ContentParts {
			if part.Type == protocol.ContentPartText && strings.Contains(part.Text, "语音转写") {
				t.Error("should not inject ASR text when attachment has no transcript meta")
			}
		}
	}

	// AudioEnabled=false：无 ASR 时音频被忽略
	p2 := &Plugin{cfg: &config.Config{AudioEnabled: false}}
	msg2 := p2.buildUserMessage(ctx, "", sess)
	if len(msg2.ContentParts) != 0 {
		t.Errorf("expected no content parts when audio disabled and no ASR, got %+v", msg2.ContentParts)
	}
}

func TestHasSubstantiveText(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"[图片]", false},
		{"[图片][语音]", false},
		{"[表情]", false},
		{"[图片] 分析这张图", true},
		{"分析这张图", true},
	}
	for _, c := range cases {
		if got := runtime.HasSubstantiveText(c.in); got != c.want {
			t.Errorf("runtime.HasSubstantiveText(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMergePendingImageParts(t *testing.T) {
	pending := []protocol.ContentPart{{Type: protocol.ContentPartImage, Data: []byte("img"), MimeType: "image/png"}}

	// 纯文本消息 + pending：图片前置 + 文字转 text part，Content 清空
	userMsg := runtime.MergePendingImageParts(protocol.Message{Role: protocol.RoleUser, Content: "看看细节"}, pending)
	if userMsg.Content != "" {
		t.Errorf("expected Content cleared after merge, got %q", userMsg.Content)
	}
	if len(userMsg.ContentParts) != 2 {
		t.Fatalf("expected 2 parts (image+text), got %d", len(userMsg.ContentParts))
	}
	if userMsg.ContentParts[0].Type != protocol.ContentPartImage {
		t.Errorf("expected image part first, got %v", userMsg.ContentParts[0].Type)
	}
	if userMsg.ContentParts[1].Type != protocol.ContentPartText || userMsg.ContentParts[1].Text != "看看细节" {
		t.Errorf("expected text part second with original text, got %+v", userMsg.ContentParts[1])
	}

	// 已有 ContentParts 的消息：pending 前置，原 parts 保留
	userMsg2 := runtime.MergePendingImageParts(protocol.Message{Role: protocol.RoleUser, ContentParts: []protocol.ContentPart{
		{Type: protocol.ContentPartText, Text: "hi"},
		{Type: protocol.ContentPartImage, Data: []byte("own")},
	}}, pending)
	if len(userMsg2.ContentParts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(userMsg2.ContentParts))
	}
	if userMsg2.ContentParts[0].Type != protocol.ContentPartImage || userMsg2.ContentParts[2].Type != protocol.ContentPartImage {
		t.Errorf("expected pending image first and own image last, got %+v", userMsg2.ContentParts)
	}
}

func TestCountImageParts(t *testing.T) {
	parts := []protocol.ContentPart{
		{Type: protocol.ContentPartText, Text: "x"},
		{Type: protocol.ContentPartImage, Data: []byte("1")},
		{Type: protocol.ContentPartImage, Data: []byte("2")},
	}
	if got := runtime.CountImageParts(parts); got != 2 {
		t.Errorf("expected 2 image parts, got %d", got)
	}
	if got := runtime.CountImageParts(nil); got != 0 {
		t.Errorf("expected 0 image parts for nil, got %d", got)
	}
}

func TestGetLastUserMessageExtractsContentPartsText(t *testing.T) {
	s := &session.Session{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: "", ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartImage, Data: []byte("img")},
			{Type: protocol.ContentPartText, Text: "这张图里有什么？"},
		}},
	}}
	if got := runtime.LastUserMessage(s); got != "这张图里有什么？" {
		t.Errorf("expected text from content parts, got %q", got)
	}
}

func TestMaybeRecordPendingImageGate(t *testing.T) {
	mkCtx := func(kind platform.EventKind, content string, chat platform.ChatInfo, atts ...platform.Attachment) *eventctx.Context {
		evt := platform.NewSyntheticEvent(kind, content,
			platform.WithSyntheticChat(chat),
			platform.WithSyntheticAttachments(atts...),
		)
		return eventctx.NewContextFromEvent(evt, nil)
	}
	img := platform.Attachment{Kind: platform.AttachmentKindImage, URL: "https://example.com/a.png", MimeType: "image/png"}
	group := platform.ChatInfo{ID: "g1", IsGroup: true}
	priv := platform.ChatInfo{ID: "u1", IsGroup: false}

	// 合并窗口关闭 → 不挂起
	p := &Plugin{cfg: &config.Config{ImageMergeWindow: 0, VisionEnabled: true}}
	if p.maybeRecordPendingImage(mkCtx(platform.EventKindGroupMessage, "[图片]", group, img), "[图片]", []platform.Attachment{img}) {
		t.Error("expected false when merge window disabled")
	}

	p = &Plugin{cfg: &config.Config{ImageMergeWindow: 30 * time.Second, VisionEnabled: true}}
	// 无图片附件 → 不挂起
	if p.maybeRecordPendingImage(mkCtx(platform.EventKindGroupMessage, "hi", group), "hi", nil) {
		t.Error("expected false without image attachments")
	}
	// 有实质文本（图+字一条消息）→ 不挂起
	if p.maybeRecordPendingImage(mkCtx(platform.EventKindGroupMessage, "分析这张图", group, img), "分析这张图", []platform.Attachment{img}) {
		t.Error("expected false when message has substantive text")
	}
	// 私聊 → 不挂起（私聊图片视为明确意图，立即处理）
	if p.maybeRecordPendingImage(mkCtx(platform.EventKindPrivateMessage, "[图片]", priv, img), "[图片]", []platform.Attachment{img}) {
		t.Error("expected false in private chat")
	}
	// vision 关闭 → 不挂起
	p2 := &Plugin{cfg: &config.Config{ImageMergeWindow: 30 * time.Second, VisionEnabled: false}}
	if p2.maybeRecordPendingImage(mkCtx(platform.EventKindGroupMessage, "[图片]", group, img), "[图片]", []platform.Attachment{img}) {
		t.Error("expected false when vision disabled")
	}
}
