package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// mockEvent 用于测试的简单 Event 实现
type mockEvent struct {
	platformID string
	kind       platform.EventKind
	rawType    string
	sender     platform.UserInfo
	chat       platform.ChatInfo
	content    string
}

func (e *mockEvent) Platform() string          { return e.platformID }
func (e *mockEvent) ID() string                { return "" }
func (e *mockEvent) Kind() platform.EventKind  { return e.kind }
func (e *mockEvent) RawType() string           { return e.rawType }
func (e *mockEvent) Sender() platform.UserInfo { return e.sender }
func (e *mockEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *mockEvent) Content() string           { return e.content }
func (e *mockEvent) Timestamp() time.Time      { return time.Time{} }
func (e *mockEvent) RawPayload() any           { return nil }

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

	// ImageMessage
	img := platform.ImageMessage("https://example.com/img.png")
	if img.ImageURL != "https://example.com/img.png" {
		t.Errorf("ImageMessage: got %q", img.ImageURL)
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
	// AudioMessage
	audio := platform.AudioMessage("https://example.com/a.mp3")
	if audio.AudioURL != "https://example.com/a.mp3" {
		t.Errorf("AudioMessage: got %q", audio.AudioURL)
	}

	// VideoMessage
	video := platform.VideoMessage("https://example.com/v.mp4")
	if video.VideoURL != "https://example.com/v.mp4" {
		t.Errorf("VideoMessage: got %q", video.VideoURL)
	}

	// FileMessage
	file := platform.FileMessage("https://example.com/doc.pdf", "doc.pdf")
	if file.FileURL != "https://example.com/doc.pdf" {
		t.Errorf("FileMessage URL: got %q", file.FileURL)
	}
	if file.FileName != "doc.pdf" {
		t.Errorf("FileMessage Name: got %q", file.FileName)
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
	err := s.Send(context.Background(), "chatid", platform.TextMessage("hello"))
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
	}
	for _, k := range kinds {
		if k == "" {
			t.Errorf("EventKind constant should not be empty")
		}
	}
}
