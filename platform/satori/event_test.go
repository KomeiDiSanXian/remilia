package satori

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// helpers to build test events

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

func makeEvent(typ string, channelType ChannelType) *Event {
	return &Event{
		SN:        42,
		Type:      typ,
		Timestamp: 1_700_000_000_000, // ms
		Channel: &Channel{
			ID:   "ch-1",
			Type: channelType,
			Name: new("test-channel"),
		},
		User: &User{
			ID:   "u-123",
			Name: new("Alice"),
		},
		Guild: &Guild{
			ID:   "guild-1",
			Name: new("Test Guild"),
		},
		Message: &Message{
			ID:        "msg-abc",
			Content:   new("hello world"),
			CreatedAt: new(int64(1_700_000_000_000)),
		},
	}
}

// ─── mapEventKind ─────────────────────────────────────────────────────────────

func TestMapEventKind_MessageCreatedGroup(t *testing.T) {
	ch := &Channel{Type: ChannelTypeText}
	got := mapEventKind(EventTypeMessageCreated, ch)
	if got != platform.EventKindGroupMessage {
		t.Errorf("group message: got %q, want %q", got, platform.EventKindGroupMessage)
	}
}

func TestMapEventKind_MessageCreatedDM(t *testing.T) {
	ch := &Channel{Type: ChannelTypeDirect}
	got := mapEventKind(EventTypeMessageCreated, ch)
	if got != platform.EventKindPrivateMessage {
		t.Errorf("DM: got %q, want %q", got, platform.EventKindPrivateMessage)
	}
}

func TestMapEventKind_MessageCreatedNilChannel(t *testing.T) {
	got := mapEventKind(EventTypeMessageCreated, nil)
	if got != platform.EventKindGroupMessage {
		t.Errorf("nil channel → should default to group: got %q", got)
	}
}

func TestMapEventKind_MessageUpdated(t *testing.T) {
	got := mapEventKind(EventTypeMessageUpdated, nil)
	if got != platform.EventKindMessageUpdate {
		t.Errorf("message updated: got %q", got)
	}
}

func TestMapEventKind_MessageDeleted(t *testing.T) {
	got := mapEventKind(EventTypeMessageDeleted, nil)
	if got != platform.EventKindMessageDelete {
		t.Errorf("message deleted: got %q", got)
	}
}

func TestMapEventKind_ChannelEvents(t *testing.T) {
	for _, typ := range []string{EventTypeChannelAdded, EventTypeChannelUpdated, EventTypeChannelRemoved} {
		got := mapEventKind(typ, nil)
		if got != platform.EventKindChannelChange {
			t.Errorf("%s: got %q, want EventKindChannelChange", typ, got)
		}
	}
}

func TestMapEventKind_GuildAdded(t *testing.T) {
	got := mapEventKind(EventTypeGuildAdded, nil)
	if got != platform.EventKindBotAdded {
		t.Errorf("guild-added: got %q", got)
	}
}

func TestMapEventKind_GuildRemoved(t *testing.T) {
	got := mapEventKind(EventTypeGuildRemoved, nil)
	if got != platform.EventKindBotRemoved {
		t.Errorf("guild-removed: got %q", got)
	}
}

func TestMapEventKind_GuildUpdated(t *testing.T) {
	got := mapEventKind(EventTypeGuildUpdated, nil)
	if got != platform.EventKindGuildChange {
		t.Errorf("guild-updated: got %q", got)
	}
}

func TestMapEventKind_GuildMemberEvents(t *testing.T) {
	cases := map[string]platform.EventKind{
		EventTypeGuildMemberAdded:   platform.EventKindMemberJoin,
		EventTypeGuildMemberRemoved: platform.EventKindMemberLeave,
		EventTypeGuildMemberUpdated: platform.EventKindMemberUpdate,
	}
	for typ, want := range cases {
		got := mapEventKind(typ, nil)
		if got != want {
			t.Errorf("%s: got %q, want %q", typ, got, want)
		}
	}
}

func TestMapEventKind_GuildRoleEvents(t *testing.T) {
	for _, typ := range []string{EventTypeGuildRoleCreated, EventTypeGuildRoleUpdated, EventTypeGuildRoleDeleted} {
		got := mapEventKind(typ, nil)
		if got != platform.EventKindNotice {
			t.Errorf("%s: got %q, want EventKindNotice", typ, got)
		}
	}
}

func TestMapEventKind_FriendRequest(t *testing.T) {
	got := mapEventKind(EventTypeFriendRequest, nil)
	if got != platform.EventKindRequest {
		t.Errorf("friend-request: got %q", got)
	}
}

func TestMapEventKind_ReactionEvents(t *testing.T) {
	for _, typ := range []string{EventTypeReactionAdded, EventTypeReactionRemoved} {
		got := mapEventKind(typ, nil)
		if got != platform.EventKindReaction {
			t.Errorf("%s: got %q, want EventKindReaction", typ, got)
		}
	}
}

func TestMapEventKind_InteractionEvents(t *testing.T) {
	for _, typ := range []string{EventTypeInteractionButton, EventTypeInteractionCommand} {
		got := mapEventKind(typ, nil)
		if got != platform.EventKindInteraction {
			t.Errorf("%s: got %q, want EventKindInteraction", typ, got)
		}
	}
}

func TestMapEventKind_LoginEvents(t *testing.T) {
	for _, typ := range []string{EventTypeLoginAdded, EventTypeLoginRemoved, EventTypeLoginUpdated} {
		got := mapEventKind(typ, nil)
		if got != platform.EventKindSystem {
			t.Errorf("%s: got %q, want EventKindSystem", typ, got)
		}
	}
}

func TestMapEventKind_Unknown(t *testing.T) {
	got := mapEventKind("some-unknown-event", nil)
	if got != platform.EventKindUnknown {
		t.Errorf("unknown type: got %q, want EventKindUnknown", got)
	}
}

// ─── convertEvent / satoriEvent interface ─────────────────────────────────────

func TestConvertEvent_Kind(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "test-platform")
	if se.Kind() != platform.EventKindGroupMessage {
		t.Errorf("Kind: got %q", se.Kind())
	}
}

func TestConvertEvent_Platform(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "myplatform")
	if se.Platform() != "myplatform" {
		t.Errorf("Platform: got %q", se.Platform())
	}
}

func TestConvertEvent_ID(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.SN = 99
	se := convertEvent(e, "p")
	if se.ID() != "99" {
		t.Errorf("ID: got %q, want '99'", se.ID())
	}
}

func TestConvertEvent_Sender(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "p")
	sender := se.Sender()
	if sender.ID != "u-123" {
		t.Errorf("Sender.ID: got %q", sender.ID)
	}
	if sender.DisplayName != "Alice" {
		t.Errorf("Sender.DisplayName: got %q", sender.DisplayName)
	}
}

func TestConvertEvent_SenderFromMember(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.User = nil
	e.Member = &GuildMember{
		User: &User{ID: "m-999", Name: new("Bob")},
		Nick: new("BobNick"),
	}
	se := convertEvent(e, "p")
	sender := se.Sender()
	if sender.ID != "m-999" {
		t.Errorf("Sender.ID from member: got %q", sender.ID)
	}
	if sender.DisplayName != "BobNick" {
		t.Errorf("Sender.DisplayName from member nick: got %q", sender.DisplayName)
	}
}

func TestConvertEvent_SenderBot(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.User.IsBot = new(true)
	se := convertEvent(e, "p")
	if !se.Sender().IsBot {
		t.Error("Sender.IsBot should be true")
	}
}

func TestConvertEvent_Chat(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "p")
	chat := se.Chat()
	if chat.ID != "ch-1" {
		t.Errorf("Chat.ID: got %q", chat.ID)
	}
	if chat.Name != "test-channel" {
		t.Errorf("Chat.Name: got %q", chat.Name)
	}
	if !chat.IsGroup {
		t.Error("Chat.IsGroup should be true for text channel")
	}
	if chat.IsDM {
		t.Error("Chat.IsDM should be false for text channel")
	}
	if chat.ParentID != "guild-1" {
		t.Errorf("Chat.ParentID: got %q", chat.ParentID)
	}
}

func TestConvertEvent_ChatDM(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeDirect)
	se := convertEvent(e, "p")
	chat := se.Chat()
	if !chat.IsDM {
		t.Error("Chat.IsDM should be true for direct channel")
	}
	if chat.IsGroup {
		t.Error("Chat.IsGroup should be false for direct channel")
	}
}

func TestConvertEvent_Content(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "p")
	if se.Content() != "hello world" {
		t.Errorf("Content: got %q", se.Content())
	}
}

func TestConvertEvent_ContentNilMessage(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.Message = nil
	se := convertEvent(e, "p")
	if se.Content() != "" {
		t.Errorf("nil message Content: got %q", se.Content())
	}
}

func TestConvertEvent_Timestamp(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "p")
	ts := se.Timestamp()
	expected := time.UnixMilli(1_700_000_000_000)
	if !ts.Equal(expected) {
		t.Errorf("Timestamp: got %v, want %v", ts, expected)
	}
}

func TestConvertEvent_Attachments(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.Message.Content = new(`<img src="https://example.com/img.png"/>`)
	se := convertEvent(e, "p")
	atts := se.Attachments()
	if len(atts) != 1 {
		t.Fatalf("Attachments: expected 1, got %d", len(atts))
	}
	if atts[0].URL != "https://example.com/img.png" {
		t.Errorf("Attachment URL: got %q", atts[0].URL)
	}
}

// ─── RawEvent interface ───────────────────────────────────────────────────────

func TestConvertEvent_RawType(t *testing.T) {
	e := makeEvent(EventTypeGuildAdded, ChannelTypeText)
	se := convertEvent(e, "p")
	if se.RawType() != EventTypeGuildAdded {
		t.Errorf("RawType: got %q, want %q", se.RawType(), EventTypeGuildAdded)
	}
}

func TestConvertEvent_RawPayload(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	se := convertEvent(e, "p")
	payload := se.RawPayload()
	if payload == nil {
		t.Fatal("RawPayload should not be nil")
	}
	raw, ok := payload.(*Event)
	if !ok {
		t.Fatalf("RawPayload should be *Event, got %T", payload)
	}
	if raw.SN != e.SN {
		t.Errorf("RawPayload SN: got %d, want %d", raw.SN, e.SN)
	}
}

// ─── nil-safety ───────────────────────────────────────────────────────────────

func TestSatoriEvent_NilRaw(t *testing.T) {
	se := &satoriEvent{}
	if se.ID() != "" {
		t.Errorf("nil raw ID: got %q", se.ID())
	}
	sender := se.Sender()
	if sender.ID != "" {
		t.Errorf("nil raw Sender.ID: got %q", sender.ID)
	}
	chat := se.Chat()
	if chat.ID != "" {
		t.Errorf("nil raw Chat.ID: got %q", chat.ID)
	}
	if !se.Timestamp().IsZero() {
		t.Error("nil raw Timestamp should be zero")
	}
	if se.RawType() != "" {
		t.Errorf("nil raw RawType: got %q", se.RawType())
	}
	// RawPayload returns e.raw (type *Event). When raw is nil, the any interface
	// wraps a (*Event)(nil) and is therefore non-nil by Go interface semantics.
	// What matters is that calling it does not panic.
	payload := se.RawPayload()
	if raw, ok := payload.(*Event); ok && raw != nil {
		t.Errorf("nil raw RawPayload: expected nil *Event, got %v", raw)
	}
}
