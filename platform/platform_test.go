package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// mockEvent 仅实现 platform.Event 基础接口，不实现任何可选接口。
// 用于测试帮助函数（RawType、GetReplyToID 等）在事件不支持可选接口时的 fallback 路径。
type mockEvent struct {
	platformID string
	kind       platform.EventKind
	sender     platform.UserInfo
	chat       platform.ChatInfo
	content    string
}

func (e *mockEvent) Platform() string                          { return e.platformID }
func (e *mockEvent) ID() string                                { return "" }
func (e *mockEvent) Kind() platform.EventKind                  { return e.kind }
func (e *mockEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *mockEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *mockEvent) Content() string                           { return e.content }
func (e *mockEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *mockEvent) Timestamp() time.Time                      { return time.Time{} }

// mockRawEvent 在 mockEvent 基础上额外实现 platform.RawEvent 可选接口。
// 用于测试帮助函数走"已实现可选接口"路径时的行为。
type mockRawEvent struct {
	mockEvent
	rawType    string
	rawPayload any
}

func (e *mockRawEvent) RawType() string { return e.rawType }
func (e *mockRawEvent) RawPayload() any { return e.rawPayload }

// mockReplyEvent 额外实现 platform.ReplyEvent 可选接口。
type mockReplyEvent struct {
	mockEvent
	replyToID string
}

func (e *mockReplyEvent) ReplyToID() string { return e.replyToID }

func TestOutboundMessage(t *testing.T) {
	// TextMessage
	msg := platform.TextMessage("hello")
	if msg.Text != "hello" {
		t.Errorf("TextMessage: got %q, want %q", msg.Text, "hello")
	}

	// MarkdownMessage
	md := platform.MarkdownMessage("# title")
	if md.Markdown != "# title" {
		t.Errorf("MarkdownMessage: got %q, want %q", md.Markdown, "# title")
	}

	// ImageMessage - now uses Attachments
	img := platform.ImageMessage("https://example.com/img.png")
	if len(img.Attachments) == 0 || img.Attachments[0].URL != "https://example.com/img.png" {
		t.Errorf("ImageMessage: got %+v", img.Attachments)
	}
	if img.Attachments[0].Kind != platform.AttachmentKindImage {
		t.Errorf("ImageMessage: wrong kind %q", img.Attachments[0].Kind)
	}

	// WithReply
	replied := msg.WithReply("msgid123")
	if replied.ReplyToID != "msgid123" {
		t.Errorf("WithReply: got %q, want %q", replied.ReplyToID, "msgid123")
	}
	// 原消息不被修改
	if msg.ReplyToID != "" {
		t.Error("WithReply should not modify original message")
	}

	// WithExtra
	extra := msg.WithExtra("msg_seq", uint64(42))
	if v, ok := extra.Extra["msg_seq"]; !ok || v != uint64(42) {
		t.Errorf("WithExtra: expected msg_seq=42, got %v", v)
	}
}

func TestOutboundMessage_RichMedia(t *testing.T) {
	// AudioMessage - uses Attachments
	audio := platform.AudioMessage("https://example.com/a.mp3")
	if len(audio.Attachments) == 0 || audio.Attachments[0].URL != "https://example.com/a.mp3" {
		t.Errorf("AudioMessage: got %+v", audio.Attachments)
	}
	if audio.Attachments[0].Kind != platform.AttachmentKindAudio {
		t.Errorf("AudioMessage: wrong kind %q", audio.Attachments[0].Kind)
	}

	// VideoMessage
	video := platform.VideoMessage("https://example.com/v.mp4")
	if len(video.Attachments) == 0 || video.Attachments[0].URL != "https://example.com/v.mp4" {
		t.Errorf("VideoMessage: got %+v", video.Attachments)
	}
	if video.Attachments[0].Kind != platform.AttachmentKindVideo {
		t.Errorf("VideoMessage: wrong kind %q", video.Attachments[0].Kind)
	}

	// FileMessage
	file := platform.FileMessage("https://example.com/doc.pdf", "doc.pdf")
	if len(file.Attachments) == 0 || file.Attachments[0].URL != "https://example.com/doc.pdf" {
		t.Errorf("FileMessage URL: got %+v", file.Attachments)
	}
	if file.Attachments[0].Name != "doc.pdf" {
		t.Errorf("FileMessage Name: got %q", file.Attachments[0].Name)
	}
	if file.Attachments[0].Kind != platform.AttachmentKindFile {
		t.Errorf("FileMessage Kind: got %q", file.Attachments[0].Kind)
	}

	// WithAttachments - multiple attachments
	base := platform.TextMessage("multi")
	multi := base.WithAttachments(
		platform.Attachment{Kind: platform.AttachmentKindImage, URL: "https://img1.com"},
		platform.Attachment{Kind: platform.AttachmentKindImage, URL: "https://img2.com"},
	)
	if len(multi.Attachments) != 2 {
		t.Errorf("WithAttachments: expected 2 attachments, got %d", len(multi.Attachments))
	}
	// original unmodified
	if len(base.Attachments) != 0 {
		t.Error("WithAttachments should not modify original message")
	}
}

func TestOutboundMessage_WithMentions(t *testing.T) {
	base := platform.TextMessage("hi")
	m1 := base.WithMentions("user1", "user2")
	if len(m1.Mentions) != 2 || m1.Mentions[0] != "user1" || m1.Mentions[1] != "user2" {
		t.Errorf("WithMentions: got %v", m1.Mentions)
	}
	// 链式追加
	m2 := m1.WithMentions("user3")
	if len(m2.Mentions) != 3 {
		t.Errorf("WithMentions chained: got %d mentions", len(m2.Mentions))
	}
	// 不修改原消息（切片独立）
	if len(base.Mentions) != 0 {
		t.Error("WithMentions should not modify original message")
	}
	if len(m1.Mentions) != 2 {
		t.Error("WithMentions chained should not mutate previous copy")
	}
}

func TestOutboundMessage_WithButtons(t *testing.T) {
	btn1 := platform.Button{ID: "btn1", Label: "OK", Style: platform.ButtonStylePrimary}
	btn2 := platform.Button{ID: "btn2", Label: "Cancel", Style: platform.ButtonStyleSecondary}
	btnLink := platform.Button{ID: "btn3", Label: "Docs", URL: "https://example.com", Style: platform.ButtonStyleLink}

	base := platform.TextMessage("choose")
	m1 := base.WithButtons(btn1, btn2)
	if len(m1.Buttons) != 2 {
		t.Errorf("WithButtons: got %d buttons", len(m1.Buttons))
	}
	if m1.Buttons[0].Style != platform.ButtonStylePrimary {
		t.Errorf("Button[0].Style: got %q", m1.Buttons[0].Style)
	}

	// 链式追加
	m2 := m1.WithButtons(btnLink)
	if len(m2.Buttons) != 3 {
		t.Errorf("WithButtons chained: got %d buttons", len(m2.Buttons))
	}
	if m2.Buttons[2].URL != "https://example.com" {
		t.Errorf("ButtonStyleLink URL: got %q", m2.Buttons[2].URL)
	}

	// 不修改原消息
	if len(base.Buttons) != 0 {
		t.Error("WithButtons should not modify original message")
	}
	if len(m1.Buttons) != 2 {
		t.Error("WithButtons chained should not mutate previous copy")
	}
}

func TestButtonStyleConstants(t *testing.T) {
	styles := []platform.ButtonStyle{
		platform.ButtonStylePrimary,
		platform.ButtonStyleSecondary,
		platform.ButtonStyleDanger,
		platform.ButtonStyleLink,
	}
	for _, s := range styles {
		if s == "" {
			t.Error("ButtonStyle constant should not be empty")
		}
	}
}

func TestNoopSender(t *testing.T) {
	s := &platform.NoopSender{}
	req := platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test-chat", IsGroup: false},
		Message: platform.TextMessage("hello"),
	}
	err := s.Send(context.Background(), req)
	if err != nil {
		t.Errorf("NoopSender.Send should return nil, got %v", err)
	}
}

func TestRegistry(t *testing.T) {
	reg := platform.NewRegistry()

	// 空注册表
	if len(reg.All()) != 0 {
		t.Error("empty registry should have no adapters")
	}

	// 停止空注册表不报错
	err := reg.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll on empty registry: %v", err)
	}
}

func TestEventKindConstants(t *testing.T) {
	kinds := []platform.EventKind{
		platform.EventKindUnknown,
		platform.EventKindPrivateMessage,
		platform.EventKindGroupMessage,
		platform.EventKindGuildMessage,
		platform.EventKindNotice,
		platform.EventKindRequest,
		platform.EventKindSystem,
		platform.EventKindInteraction,
		platform.EventKindReaction,
		platform.EventKindMemberJoin,
		platform.EventKindMemberLeave,
		platform.EventKindMessageUpdate,
		platform.EventKindMessageDelete,
	}
	for _, k := range kinds {
		if k == "" {
			t.Errorf("EventKind constant should not be empty")
		}
	}
}

// ---- Registry.StartAll 并发行为 & ctx 取消 -------------------------------------------

// mockCancelableAdapter 等待 ctx 取消后退出（模拟正常平台适配器）
type mockCancelableAdapter struct {
	platformID string
	started    chan struct{}
}

func (a *mockCancelableAdapter) Platform() string { return a.platformID }
func (a *mockCancelableAdapter) Sender() platform.Sender {
	return &platform.NoopSender{}
}
func (a *mockCancelableAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
func (a *mockCancelableAdapter) Start(ctx context.Context, _ func(platform.Event)) error {
	close(a.started)
	<-ctx.Done()
	return ctx.Err()
}
func (a *mockCancelableAdapter) Stop(_ context.Context) error { return nil }
func (a *mockCancelableAdapter) IsRunning() bool              { return false }

func TestRegistry_StartAll_CtxCancel(t *testing.T) {
	reg := platform.NewRegistry()

	a1 := &mockCancelableAdapter{platformID: "p1", started: make(chan struct{})}
	a2 := &mockCancelableAdapter{platformID: "p2", started: make(chan struct{})}
	reg.Register(a1)
	reg.Register(a2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- reg.StartAll(ctx, func(platform.Event) {})
	}()

	// 等待两个适配器都已启动
	select {
	case <-a1.started:
	case <-time.After(3 * time.Second):
		t.Fatal("adapter p1 did not start in time")
	}
	select {
	case <-a2.started:
	case <-time.After(3 * time.Second):
		t.Fatal("adapter p2 did not start in time")
	}

	cancel()

	select {
	case err := <-done:
		// context.Canceled 应被过滤，返回 nil
		if err != nil {
			t.Errorf("StartAll after ctx cancel: expected nil, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StartAll did not return in time after ctx cancel")
	}
}

func TestRegistry_StartAll_NoAdapters(t *testing.T) {
	reg := platform.NewRegistry()
	err := reg.StartAll(context.Background(), func(platform.Event) {})
	if err == nil {
		t.Error("StartAll on empty registry: expected error, got nil")
	}
}

// mockFatalAdapter 立即以非 ctx 错误退出（模拟适配器启动失败）
type mockFatalAdapter struct {
	platformID string
	startErr   error
}

func (a *mockFatalAdapter) Platform() string { return a.platformID }
func (a *mockFatalAdapter) Sender() platform.Sender {
	return &platform.NoopSender{}
}
func (a *mockFatalAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
func (a *mockFatalAdapter) Start(_ context.Context, _ func(platform.Event)) error {
	return a.startErr
}
func (a *mockFatalAdapter) Stop(_ context.Context) error { return nil }
func (a *mockFatalAdapter) IsRunning() bool              { return false }

func TestRegistry_StartAll_AdapterFatalError(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(&mockFatalAdapter{platformID: "fatal", startErr: errors.New("fatal adapter error")})

	err := reg.StartAll(context.Background(), func(platform.Event) {})
	if err == nil {
		t.Fatal("StartAll: expected error from fatal adapter, got nil")
	}
	if !strings.Contains(err.Error(), "fatal adapter error") {
		t.Errorf("StartAll: error should contain 'fatal adapter error', got: %v", err)
	}
}

// ---- Registry.StopAll 错误聚合 -------------------------------------------------------

// mockErrorStopAdapter Stop 时返回指定错误
type mockErrorStopAdapter struct {
	platformID string
	stopErr    error
}

func (a *mockErrorStopAdapter) Platform() string { return a.platformID }
func (a *mockErrorStopAdapter) Sender() platform.Sender {
	return &platform.NoopSender{}
}
func (a *mockErrorStopAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
func (a *mockErrorStopAdapter) Start(ctx context.Context, _ func(platform.Event)) error {
	<-ctx.Done()
	return ctx.Err()
}
func (a *mockErrorStopAdapter) Stop(_ context.Context) error { return a.stopErr }
func (a *mockErrorStopAdapter) IsRunning() bool              { return false }

func TestRegistry_StopAll_Errors(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(&mockErrorStopAdapter{platformID: "ok-platform", stopErr: nil})
	reg.Register(&mockErrorStopAdapter{platformID: "bad-platform", stopErr: errors.New("stop failed")})

	err := reg.StopAll(context.Background())
	if err == nil {
		t.Fatal("StopAll: expected error when an adapter fails, got nil")
	}
	if !strings.Contains(err.Error(), "stop failed") {
		t.Errorf("StopAll: error should contain 'stop failed', got: %v", err)
	}
}

func TestRegistry_StopAll_AllOK(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(&mockErrorStopAdapter{platformID: "p1", stopErr: nil})
	reg.Register(&mockErrorStopAdapter{platformID: "p2", stopErr: nil})

	err := reg.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll with no errors: expected nil, got %v", err)
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := platform.NewRegistry()
	adapter := &mockCancelableAdapter{platformID: "myplatform", started: make(chan struct{})}
	reg.Register(adapter)

	got, ok := reg.Get("myplatform")
	if !ok {
		t.Fatal("Registry.Get: expected ok=true for registered platform")
	}
	if got.Platform() != "myplatform" {
		t.Errorf("Registry.Get: got platform %q, want %q", got.Platform(), "myplatform")
	}

	_, ok2 := reg.Get("nonexistent")
	if ok2 {
		t.Error("Registry.Get: expected ok=false for nonexistent platform")
	}
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	reg := platform.NewRegistry()
	old := &mockCancelableAdapter{platformID: "p", started: make(chan struct{})}
	new_ := &mockFatalAdapter{platformID: "p", startErr: nil}

	reg.Register(old)
	reg.Register(new_) // 覆盖

	got, ok := reg.Get("p")
	if !ok {
		t.Fatal("Registry.Get after overwrite: expected ok=true")
	}
	// 应该是新适配器（mockFatalAdapter）
	if _, isFatal := got.(*mockFatalAdapter); !isFatal {
		t.Error("Registry.Register overwrite: expected new adapter to replace old one")
	}
}

// ── 可选接口帮助函数（fallback 路径与实现路径） ────────────────────────────────

func TestRawEventHelpers_Fallback(t *testing.T) {
	// mockEvent 不实现 RawEvent → 帮助函数应走 fallback 返回零值
	e := &mockEvent{platformID: "test"}

	if got := platform.RawType(e); got != "" {
		t.Errorf("RawType on non-RawEvent: want \"\", got %q", got)
	}
	if got := platform.RawPayload(e); got != nil {
		t.Errorf("RawPayload on non-RawEvent: want nil, got %v", got)
	}
}

func TestRawEventHelpers_Implemented(t *testing.T) {
	// mockRawEvent 实现 RawEvent → 帮助函数应返回真实值
	sentinel := struct{ v int }{v: 42}
	e := &mockRawEvent{
		mockEvent:  mockEvent{platformID: "test"},
		rawType:    "MY_EVENT_TYPE",
		rawPayload: sentinel,
	}

	if got := platform.RawType(e); got != "MY_EVENT_TYPE" {
		t.Errorf("RawType on RawEvent: want %q, got %q", "MY_EVENT_TYPE", got)
	}
	if got := platform.RawPayload(e); got != sentinel {
		t.Errorf("RawPayload on RawEvent: want sentinel, got %v", got)
	}
}

func TestGetReplyToIDHelper(t *testing.T) {
	// mockEvent 不实现 ReplyEvent → 返回 ""
	plain := &mockEvent{}
	if id := platform.GetReplyToID(plain); id != "" {
		t.Errorf("GetReplyToID on non-ReplyEvent: want \"\", got %q", id)
	}

	// mockReplyEvent 实现 ReplyEvent → 返回真实值
	reply := &mockReplyEvent{replyToID: "msg-xyz"}
	if id := platform.GetReplyToID(reply); id != "msg-xyz" {
		t.Errorf("GetReplyToID on ReplyEvent: want \"msg-xyz\", got %q", id)
	}
}

// ── IsEmpty ──────────────────────────────────────────────────────────────────

func TestOutboundMessage_IsEmpty(t *testing.T) {
	cases := []struct {
		name  string
		msg   platform.OutboundMessage
		empty bool
	}{
		{"zero value", platform.OutboundMessage{}, true},
		{"text only", platform.TextMessage("hi"), false},
		{"markdown only", platform.MarkdownMessage("# h"), false},
		{"image only", platform.ImageMessage("https://img"), false},
		{"embed only", platform.OutboundMessage{Embeds: []platform.Embed{{Title: "t"}}}, false},
		{"mentions only", platform.OutboundMessage{Mentions: []string{"u1"}}, true},
		{"buttons only", platform.OutboundMessage{Buttons: []platform.Button{{ID: "b"}}}, true},
		{"extra only", platform.OutboundMessage{}.WithExtra("k", "v"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.IsEmpty(); got != tc.empty {
				t.Errorf("IsEmpty() = %v, want %v", got, tc.empty)
			}
		})
	}
}

// ── Button.Row 语义 ──────────────────────────────────────────────────────────

func TestButtonRowAuto(t *testing.T) {
	// ButtonRowAuto 必须是整数零值，保证 Button{} 默认为自动排列
	if platform.ButtonRowAuto != 0 {
		t.Errorf("ButtonRowAuto should be 0 (zero value), got %d", platform.ButtonRowAuto)
	}
	btn := platform.Button{Label: "click"}
	if btn.Row != platform.ButtonRowAuto {
		t.Error("Default Button.Row should equal ButtonRowAuto")
	}
}

// ── Embed.Color uint32 ───────────────────────────────────────────────────────

func TestEmbedColor_IsUint32(t *testing.T) {
	e := platform.Embed{Color: 0x5865F2}
	if e.Color != 0x5865F2 {
		t.Errorf("Embed.Color: want 0x5865F2, got %d", e.Color)
	}
	// 确认无法赋负值（uint32 不接受负数字面量，编译期保证）
	var c platform.Embed
	c.Color = 0xFFFFFF
	if c.Color != 0xFFFFFF {
		t.Errorf("Embed.Color max white: want 0xFFFFFF, got %d", c.Color)
	}
}

// ── Registry.All() 始终非 nil ────────────────────────────────────────────────

func TestRegistry_All_NeverNil(t *testing.T) {
	reg := platform.NewRegistry()
	got := reg.All()
	if got == nil {
		t.Error("Registry.All() on empty registry: should return non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("Registry.All() on empty registry: want len=0, got len=%d", len(got))
	}
}

// ── AdapterObserver / WithObserver ───────────────────────────────────────────

// mockObserver 记录收到的 observer 调用，用于断言
type mockObserver struct {
	started     []string
	stopped     []string
	errors      [][2]string // [platform, errMsg]
	disconnects []string
}

func (o *mockObserver) OnAdapterStarted(p string)    { o.started = append(o.started, p) }
func (o *mockObserver) OnAdapterStopped(p string)    { o.stopped = append(o.stopped, p) }
func (o *mockObserver) OnAdapterError(p, msg string) { o.errors = append(o.errors, [2]string{p, msg}) }
func (o *mockObserver) OnAdapterDisconnect(p string, _ error) {
	o.disconnects = append(o.disconnects, p)
}

func TestRegistry_WithObserver_StartStop(t *testing.T) {
	obs := &mockObserver{}
	reg := platform.NewRegistry().WithObserver(obs)

	a := &mockFatalAdapter{platformID: "obs-platform", startErr: nil}
	reg.Register(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消，让 Start 因 ctx 退出
	_ = reg.StartAll(ctx, func(platform.Event) {})

	if len(obs.started) != 1 || obs.started[0] != "obs-platform" {
		t.Errorf("OnAdapterStarted: want [obs-platform], got %v", obs.started)
	}
	if len(obs.stopped) != 1 || obs.stopped[0] != "obs-platform" {
		t.Errorf("OnAdapterStopped: want [obs-platform], got %v", obs.stopped)
	}
}

func TestRegistry_WithObserver_FatalError(t *testing.T) {
	obs := &mockObserver{}
	reg := platform.NewRegistry().WithObserver(obs)
	reg.Register(&mockFatalAdapter{platformID: "bad", startErr: errors.New("boom")})

	_ = reg.StartAll(context.Background(), func(platform.Event) {})

	if len(obs.errors) != 1 || obs.errors[0][0] != "bad" {
		t.Errorf("OnAdapterError: want [{bad,...}], got %v", obs.errors)
	}
}

func TestRegistry_WithObserver_Nil(t *testing.T) {
	// 传入 nil observer 不应 panic
	reg := platform.NewRegistry().WithObserver(nil)
	reg.Register(&mockFatalAdapter{platformID: "p", startErr: nil})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reg.StartAll(ctx, func(platform.Event) {}); err != nil {
		t.Errorf("nil observer should not cause error: %v", err)
	}
}
