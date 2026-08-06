package satori

// 本文档核验说明
//
// 本文件中的事件类型、消息元素断言均对照 Satori 协议官方规范
// （2026-07 核验）：https://satori.chat/zh-CN/protocol/
//
//	- API（{resource}.{method} 形式）：/zh-CN/protocol/api.html
//	- 事件（message-created、guild-member-added、reaction-added 等）：
//	  /zh-CN/protocol/events.html
//	- 标准元素（at、image、button、quote 等）：/zh-CN/protocol/elements.html
//	- 各资源 API 参数：/zh-CN/resources/{channel,message,reaction,...}.html
//
// 注意：reaction.create/delete/list 的请求参数为 emoji_id（而非 emoji），
// 已在 client.go 中按规范修正，参见 /zh-CN/resources/reaction.html。

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

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

// ─── MentionsEvent / ReplyEvent ───────────────────────────────────────────────

// TestConvertEvent_Mentions 固定 <at> 元素还原为结构化 @ 列表。
//
// 框架的 OnMentionedBot() 走的是 platform.GetMentions 返回的结构化列表，
// 而不是对正文做文本匹配。此前 satoriEvent 未实现 MentionsEvent，
// GetMentions 恒返回 nil，该规则在 Satori 上永不命中。
func TestConvertEvent_Mentions(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	content := `<at id="10001" name="Bot"/> 你好`
	e.Message = &Message{ID: "m1", Content: &content}

	se := convertEventWithBot(e, "p", "10001")

	mentions := se.Mentions()
	if len(mentions) != 1 {
		t.Fatalf("Mentions 数量: got %d, want 1", len(mentions))
	}
	if mentions[0].ID != "10001" {
		t.Errorf("Mentions[0].ID: got %q, want %q", mentions[0].ID, "10001")
	}
	if !mentions[0].IsSelf {
		t.Error("被 @ 的是机器人自身，IsSelf 应为 true，否则 OnMentionedBot 不会命中")
	}
	// at 剥离：@ 不进入 Content，命令可直接 OnCommand 匹配
	if se.Content() != "你好" {
		t.Errorf("Content: got %q, want %q（at 已剥离）", se.Content(), "你好")
	}
	// 段保真：at 段保留 UserID/显示名
	segs := se.Segments()
	if len(segs) != 2 {
		t.Fatalf("Segments: got %d, want 2", len(segs))
	}
	if segs[0].Type != platform.SegmentAt || segs[0].UserID != "10001" || segs[0].Text != "Bot" {
		t.Errorf("Segments[0]: got %+v, want at(10001, Bot)", segs[0])
	}
	if segs[1].Type != platform.SegmentText || segs[1].Text != " 你好" {
		t.Errorf("Segments[1]: got %+v, want text(\" 你好\")", segs[1])
	}
}

// TestConvertEvent_MentionsNotSelf 他人被 @ 时不应标记 IsSelf。
func TestConvertEvent_MentionsNotSelf(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	content := `<at id="20002"/> hi`
	e.Message = &Message{ID: "m1", Content: &content}

	se := convertEventWithBot(e, "p", "10001")
	mentions := se.Mentions()
	if len(mentions) != 1 || mentions[0].IsSelf {
		t.Errorf("他人被 @ 时 IsSelf 应为 false: %+v", mentions)
	}
}

// TestConvertEvent_ReplyToID 固定 <quote id="..."/> 还原为回复关系。
//
// quote 元素在解析正文前会被整体剥离（HTML 解析器不认识它），
// 此前连同 id 一起丢弃，导致 ReplyToID 恒为空。
func TestConvertEvent_ReplyToID(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	content := `<quote id="msg-123"/>这是回复`
	e.Message = &Message{ID: "m2", Content: &content}

	se := convertEvent(e, "p")
	if se.ReplyToID() != "msg-123" {
		t.Errorf("ReplyToID: got %q, want %q", se.ReplyToID(), "msg-123")
	}
	// 剥离 quote 后正文必须完整保留。
	if se.Content() != "这是回复" {
		t.Errorf("Content: got %q", se.Content())
	}
}

// TestConvertEvent_NoMentionsReturnsNil 无 @ 时应返回 nil 而非空切片。
func TestConvertEvent_NoMentionsReturnsNil(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	content := "纯文本"
	e.Message = &Message{ID: "m3", Content: &content}

	se := convertEvent(e, "p")
	if se.Mentions() != nil {
		t.Errorf("无 @ 时 Mentions 应为 nil: %+v", se.Mentions())
	}
	if se.ReplyToID() != "" {
		t.Errorf("非回复时 ReplyToID 应为空: %q", se.ReplyToID())
	}
}

// TestConvertEvent_SenderUserNick 固定 User.Nick 优先于 User.Name。
//
// types.go 中该字段的文档写明"优先级高于 Name"，但此前代码从未读取它：
// 私聊没有 Member 对象，于是显示的是 "qq_user_10001" 这类原始账号名
// 而不是好友备注。
func TestConvertEvent_SenderUserNick(t *testing.T) {
	e := makeEvent(EventTypeMessageCreated, ChannelTypeText)
	e.Member = nil
	e.User = &User{ID: "10001", Name: new("qq_user_10001"), Nick: new("张三")}

	se := convertEvent(e, "p")
	if got := se.Sender().DisplayName; got != "张三" {
		t.Errorf("Sender.DisplayName: got %q, want %q", got, "张三")
	}
}

// ─── httpBaseURL ──────────────────────────────────────────────────────────────

// TestHTTPBaseURL 固定 ws:// 归一化。
//
// ServerURL 的字段文档明确支持 ws:// 写法，但 http.Transport 只认 http/https。
// 不归一化的话，WebSocket 一切正常而每一条出站消息都会以
// "unsupported protocol scheme" 失败。
func TestHTTPBaseURL(t *testing.T) {
	cases := map[string]string{
		"ws://localhost:5140":   "http://localhost:5140",
		"wss://example.com":     "https://example.com",
		"http://localhost:5140": "http://localhost:5140",
		"https://example.com/":  "https://example.com",
	}
	for in, want := range cases {
		if got := httpBaseURL(in); got != want {
			t.Errorf("httpBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
