package platform_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
)

// ── Registry.Replace ─────────────────────────────────────────────────────────

func TestRegistry_Replace(t *testing.T) {
	reg := platform.NewRegistry()
	old := mock.NewAdapter(mock.WithPlatform("p"))
	reg.Register(old)

	ctx, cancel := context.WithCancel(context.Background())
	go reg.StartAll(ctx, func(platform.Event) {})
	time.Sleep(5 * time.Millisecond)

	new := mock.NewAdapter(mock.WithPlatform("p"))
	replaced, ok := reg.Replace(new)
	if !ok {
		t.Fatal("Replace should succeed")
	}
	if replaced == nil {
		t.Fatal("Replace: expected old adapter returned")
	}
	if replaced.Platform() != "p" {
		t.Errorf("Replace: expected old adapter platform %q, got %q", "p", replaced.Platform())
	}
	cancel()
}

func TestRegistry_Replace_NotFound(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(mock.NewAdapter(mock.WithPlatform("p")))
	other := mock.NewAdapter(mock.WithPlatform("other"))
	_, ok := reg.Replace(other)
	if ok {
		t.Error("Replace should return false for non-registered platform")
	}
}

// ── Registry.Remove ──────────────────────────────────────────────────────────

func TestRegistry_Remove(t *testing.T) {
	reg := platform.NewRegistry()
	a := mock.NewAdapter(mock.WithPlatform("removable"))
	reg.Register(a)

	removed := reg.Remove("removable")
	if !removed {
		t.Error("Remove should return true for existing adapter")
	}

	_, ok := reg.Get("removable")
	if ok {
		t.Error("adapter should not exist after Remove")
	}
}

func TestRegistry_Remove_NonExistent(t *testing.T) {
	reg := platform.NewRegistry()
	if reg.Remove("nonexistent") {
		t.Error("Remove should return false for nonexistent adapter")
	}
}

// ── Registry StartAll edge cases ─────────────────────────────────────────────

func TestRegistry_StartAll_AdapterError(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(mock.NewAdapter(
		mock.WithPlatform("failing"),
		mock.WithStartError(errors.New("start failed")),
	))

	err := reg.StartAll(context.Background(), func(platform.Event) {})
	if err == nil {
		t.Error("StartAll should return error when an adapter fails")
	}
}

func TestRegistry_StopAll(t *testing.T) {
	reg := platform.NewRegistry()
	a := mock.NewAdapter(mock.WithPlatform("stoppable"))
	reg.Register(a)

	ctx, cancel := context.WithCancel(context.Background())
	go reg.StartAll(ctx, func(platform.Event) {})
	time.Sleep(5 * time.Millisecond)
	cancel()
	time.Sleep(5 * time.Millisecond)

	err := reg.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}
}

func TestRegistry_StopAll_NoAdapters(t *testing.T) {
	reg := platform.NewRegistry()
	err := reg.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll with no adapters should not error: %v", err)
	}
}

func TestRegistry_StopAll_Error(t *testing.T) {
	reg := platform.NewRegistry()
	reg.Register(mock.NewAdapter(
		mock.WithPlatform("bad-stop"),
		mock.WithStopError(errors.New("stop failed")),
	))

	ctx, cancel := context.WithCancel(context.Background())
	go reg.StartAll(ctx, func(platform.Event) {})
	time.Sleep(5 * time.Millisecond)
	cancel()
	time.Sleep(5 * time.Millisecond)

	err := reg.StopAll(context.Background())
	if err == nil {
		t.Error("StopAll should return error when an adapter fails to stop")
	}
}

// ── SyntheticEvent construction ──────────────────────────────────────────────

func TestSyntheticEvent_Basic(t *testing.T) {
	e := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "hello")
	if e.Kind() != platform.EventKindPrivateMessage {
		t.Errorf("expected PRIVATE_MESSAGE, got %v", e.Kind())
	}
	if e.Content() != "hello" {
		t.Errorf("expected 'hello', got %q", e.Content())
	}
}

func TestSyntheticEvent_WithOptions(t *testing.T) {
	e := platform.NewSyntheticEvent(
		"GROUP_MESSAGE",
		"/ping",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user1", DisplayName: "User1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "chat1", IsGroup: true}),
		platform.WithSyntheticPlatform("discord"),
		platform.WithSyntheticID("evt_123"),
	)
	if e.Sender().ID != "user1" {
		t.Errorf("expected sender ID user1, got %q", e.Sender().ID)
	}
	if e.Chat().ID != "chat1" {
		t.Errorf("expected chat ID chat1, got %q", e.Chat().ID)
	}
	if e.Platform() != "discord" {
		t.Errorf("expected platform discord, got %q", e.Platform())
	}
	if e.ID() != "evt_123" {
		t.Errorf("expected event ID evt_123, got %q", e.ID())
	}
}

func TestSyntheticEvent_WithAttachments(t *testing.T) {
	e := platform.NewSyntheticEvent(
		"GROUP_MESSAGE",
		"check this",
		platform.WithSyntheticAttachments(
			platform.InboundAttachment{URL: "https://example.com/img.png", MimeType: "image/png"},
		),
	)
	atts := e.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].URL != "https://example.com/img.png" {
		t.Errorf("expected URL, got %q", atts[0].URL)
	}
}

// ── Event helper functions with mock ─────────────────────────────────────────

func TestGetEditorWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	editor, ok := platform.GetEditor(a)
	if !ok {
		t.Fatal("expected GetEditor to succeed with MockSender")
	}
	err := editor.Edit(context.Background(), "chat1", "msg1", platform.OutboundMessage{Text: "edited"})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
}

func TestGetDeleterWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	deleter, ok := platform.GetDeleter(a)
	if !ok {
		t.Fatal("expected GetDeleter to succeed")
	}
	err := deleter.Delete(context.Background(), "chat1", "msg1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestGetReactionSenderWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	rs, ok := platform.GetReactionSender(a)
	if !ok {
		t.Fatal("expected GetReactionSender to succeed")
	}
	err := rs.AddReaction(context.Background(), "chat1", "msg1", platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"})
	if err != nil {
		t.Fatalf("AddReaction failed: %v", err)
	}
}

func TestGetTypingNotifierWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	tn, ok := platform.GetTypingNotifier(a)
	if !ok {
		t.Fatal("expected GetTypingNotifier to succeed")
	}
	err := tn.SendTyping(context.Background(), "chat1")
	if err != nil {
		t.Fatalf("SendTyping failed: %v", err)
	}
}

func TestGetGroupManagerWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	gm, ok := platform.GetGroupManager(a)
	if !ok {
		t.Fatal("expected GetGroupManager to succeed")
	}
	if err := gm.KickMember(context.Background(), "g1", "u1", false); err != nil {
		t.Fatalf("KickMember failed: %v", err)
	}
}

func TestGetGroupInfoProviderWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	gip, ok := platform.GetGroupInfoProvider(a)
	if !ok {
		t.Fatal("expected GetGroupInfoProvider to succeed")
	}
	info, err := gip.GetGroupInfo(context.Background(), "g1")
	if err != nil {
		t.Fatalf("GetGroupInfo failed: %v", err)
	}
	if info.Name != "mock group" {
		t.Errorf("expected mock group name, got %q", info.Name)
	}
}

func TestGetAutoModeratorWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	am, ok := platform.GetAutoModerator(a)
	if !ok {
		t.Fatal("expected GetAutoModerator to succeed")
	}
	if err := am.DeleteMemberMessage(context.Background(), "g1", "msg1"); err != nil {
		t.Fatalf("DeleteMemberMessage failed: %v", err)
	}
}

func TestGetSessionNotifierWithMock(t *testing.T) {
	s := mock.NewSender()
	a := mock.NewAdapter(mock.WithSender(s))
	sn, ok := platform.GetSessionNotifier(a)
	if !ok {
		t.Fatal("expected GetSessionNotifier to succeed")
	}
	if err := sn.NotifyUser(context.Background(), "user1", platform.OutboundMessage{Text: "hi"}); err != nil {
		t.Fatalf("NotifyUser failed: %v", err)
	}
}

// ── BotIdentity helper ───────────────────────────────────────────────────────

func TestGetBotID_BotName(t *testing.T) {
	a := mock.NewAdapter(mock.WithBotID("bot123"), mock.WithBotName("MyBot"))
	if id := platform.GetBotID(a); id != "bot123" {
		t.Errorf("expected bot ID bot123, got %q", id)
	}
	if name := platform.GetBotName(a); name != "MyBot" {
		t.Errorf("expected bot name MyBot, got %q", name)
	}
}

func TestGetBotID_NoBotIdentity(t *testing.T) {
	a := mock.NewAdapter()
	if id := platform.GetBotID(a); id != "" {
		t.Errorf("expected empty bot ID, got %q", id)
	}
}

// ── NoopSender ───────────────────────────────────────────────────────────────

func TestNoopSender_Send(t *testing.T) {
	s := &platform.NoopSender{}
	result, err := s.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "x"},
		Message: platform.OutboundMessage{Text: "test"},
	})
	if err != nil {
		t.Fatalf("NoopSender.Send failed: %v", err)
	}
	if result.MessageID != "" {
		t.Errorf("expected empty MessageID from NoopSender, got %q", result.MessageID)
	}
}

// ── SafeDispatch ─────────────────────────────────────────────────────────────

func TestSafeDispatch_Normal(t *testing.T) {
	called := false
	handler := func(e platform.Event) {
		called = true
	}
	e := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "test")
	platform.SafeDispatch(handler, e)
	if !called {
		t.Error("SafeDispatch should call the handler")
	}
}

func TestSafeDispatch_Panic(t *testing.T) {
	handler := func(e platform.Event) {
		panic("test panic")
	}
	e := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "test")
	platform.SafeDispatch(handler, e)
}

// ── DisconnectNotifier ───────────────────────────────────────────────────────

func TestDisconnectNotifier_MultipleCallbacks(t *testing.T) {
	a := mock.NewAdapter()
	count := 0
	a.OnDisconnect(func(err error) { count++ })
	a.OnDisconnect(func(err error) { count++ })
	a.NotifyDisconnect(errors.New("test"))
	if count != 2 {
		t.Errorf("expected 2 callbacks, got %d", count)
	}
}

func TestDisconnectNotifier_Unregister(t *testing.T) {
	a := mock.NewAdapter()
	count := 0
	unreg := a.OnDisconnect(func(err error) { count++ })
	a.OnDisconnect(func(err error) { count++ })
	unreg()
	a.NotifyDisconnect(errors.New("test"))
	if count != 1 {
		t.Errorf("expected 1 callback after unregister, got %d", count)
	}
}

func TestDisconnectNotifier_NoCallbacks(t *testing.T) {
	a := mock.NewAdapter()
	a.NotifyDisconnect(errors.New("test")) // should not panic
}

// ── Event optional interface helpers ─────────────────────────────────────────

func TestEditableEventHelpers(t *testing.T) {
	plain := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "hi")
	if platform.IsEdited(plain) {
		t.Error("plain event should not be edited")
	}
}

func TestMentionsEventHelper(t *testing.T) {
	e := platform.NewSyntheticEvent("GROUP_MESSAGE", "hello")
	mentions := platform.GetMentions(e)
	if mentions != nil {
		t.Error("expected nil mentions for plain event")
	}
}

// TestTruncateText_SuffixCountsTowardBudget 固定"省略号计入 maxRunes 预算"的语义。
//
// 该函数的用途就是把文本压到平台长度上限以内（Capabilities.MaxTextLength、
// Telegram 的 200 字符 callback 提示等）。若省略号不计入预算，返回值恰好
// 超出上限一个字符，请求照样被平台拒绝——正好在这个函数存在的唯一场景里失效。
func TestTruncateText_SuffixCountsTowardBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		max      int
		want     string
		wantRune int
	}{
		{"未超限时原样返回", "hello", 10, "hello", 5},
		{"恰好等于上限时原样返回", "hello", 5, "hello", 5},
		{"超限时总长度不超过 max", "hello world", 5, "hell…", 5},
		{"max 为 1 时只返回省略号", "hello", 1, "…", 1},
		{"max 非正数返回空串", "hello", 0, "", 0},
		{"按 rune 而非字节计算", "你好世界啊", 3, "你好…", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := platform.TruncateText(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("TruncateText(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
			if n := len([]rune(got)); n != tt.wantRune {
				t.Errorf("TruncateText(%q, %d) 长度 %d rune，want %d", tt.in, tt.max, n, tt.wantRune)
			}
			if tt.max > 0 && len([]rune(got)) > tt.max {
				t.Errorf("结果超出预算: %d > %d", len([]rune(got)), tt.max)
			}
		})
	}
}
