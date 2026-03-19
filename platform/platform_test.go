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
