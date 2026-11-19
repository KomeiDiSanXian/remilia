package wechat

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// textCallback 构造一条文本回调。
func textCallback(chatType, userID, content string) *Callback {
	cb := &Callback{
		MsgID:    "msg-1",
		AIBotID:  "bot",
		ChatType: chatType,
		From:     From{UserID: userID},
		MsgType:  msgTypeText,
		Text:     &TextBody{Content: content},
	}
	if chatType == chatTypeGroup {
		cb.ChatID = "chat-1"
	}
	return cb
}

func TestNewEventPrivateMessage(t *testing.T) {
	evt := newEvent(textCallback(chatTypeSingle, "user-1", "hello"), "bot")
	if evt == nil {
		t.Fatal("单聊文本消息应产出事件")
	}
	if evt.Kind() != platform.EventKindPrivateMessage {
		t.Fatalf("Kind = %q", evt.Kind())
	}
	if evt.Chat().ID != "user-1" {
		t.Fatalf("单聊会话 ID 应为 userid，实际 %q", evt.Chat().ID)
	}
	if evt.Chat().IsGroup {
		t.Fatal("单聊不应标记 IsGroup")
	}
	if got := platform.Content(evt); got != "hello" {
		t.Fatalf("正文 = %q", got)
	}
	if evt.Sender().ID != "user-1" {
		t.Fatalf("发送者 = %q", evt.Sender().ID)
	}
}

func TestNewEventGroupStripsMention(t *testing.T) {
	evt := newEvent(textCallback(chatTypeGroup, "user-1", "@RobotA /ping"), "bot")
	if evt == nil {
		t.Fatal("群聊文本消息应产出事件")
	}
	if evt.Kind() != platform.EventKindGroupMessage {
		t.Fatalf("Kind = %q", evt.Kind())
	}
	if evt.Chat().ID != "chat-1" || !evt.Chat().IsGroup {
		t.Fatalf("群会话信息错误: %+v", evt.Chat())
	}
	// @前缀必须剥掉，否则 /ping 这类命令永远匹配不上。
	if got := platform.Content(evt); got != "/ping" {
		t.Fatalf("正文 = %q, want /ping", got)
	}
	mentions := platform.GetMentions(evt)
	if len(mentions) != 1 || !mentions[0].IsSelf {
		t.Fatalf("应有一个 @机器人 段: %+v", mentions)
	}
}

// TestNewEventGroupWithoutMentionPrefix 前提不成立时不得误吃正文。
func TestNewEventGroupWithoutMentionPrefix(t *testing.T) {
	evt := newEvent(textCallback(chatTypeGroup, "user-1", "hello"), "bot")
	if got := platform.Content(evt); got != "hello" {
		t.Fatalf("正文 = %q", got)
	}
	if m := platform.GetMentions(evt); len(m) != 0 {
		t.Fatalf("不应解析出 @: %+v", m)
	}
}

// TestNewEventGroupSoleMention 正文只有一个 "@RobotA"（无后续空白）时保持原样。
func TestNewEventGroupSoleMention(t *testing.T) {
	evt := newEvent(textCallback(chatTypeGroup, "user-1", "@RobotA"), "bot")
	if got := platform.Content(evt); got != "@RobotA" {
		t.Fatalf("正文 = %q, want @RobotA", got)
	}
}

// TestDirectedAtBot 智能机器人只收到 @ 它或单聊消息，因此恒为 true。
func TestDirectedAtBot(t *testing.T) {
	for _, ct := range []string{chatTypeSingle, chatTypeGroup} {
		evt := newEvent(textCallback(ct, "user-1", "hello"), "bot")
		de, ok := evt.(platform.DirectedAtBotEvent)
		if !ok {
			t.Fatalf("%s: 事件应实现 DirectedAtBotEvent", ct)
		}
		if !de.DirectedAtBot() {
			t.Fatalf("%s: DirectedAtBot 应为 true", ct)
		}
		if !platform.MentionedBot(evt) {
			t.Fatalf("%s: MentionedBot 应为 true", ct)
		}
	}
}

func TestNewEventVoiceUsesTranscript(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeVoice,
		Voice:    &VoiceBody{Content: "今天天气怎么样"},
	}
	evt := newEvent(cb, "bot")
	if got := platform.Content(evt); got != "今天天气怎么样" {
		t.Fatalf("语音应使用 ASR 转写文本作为正文，实际 %q", got)
	}
}

func TestNewEventImageCarriesAesKey(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeImage,
		Image:    &MediaBody{URL: "https://example.com/a.jpg", AesKey: "key-1"},
	}
	evt := newEvent(cb, "bot")
	atts := platform.Attachments(evt)
	if len(atts) != 1 {
		t.Fatalf("附件数量 = %d", len(atts))
	}
	if atts[0].Kind != platform.AttachmentKindImage {
		t.Fatalf("Kind = %q", atts[0].Kind)
	}
	// aeskey 必须随附件带出，否则下游拿到的是密文。
	if got, _ := atts[0].Extra[ExtraKeyAesKey].(string); got != "key-1" {
		t.Fatalf("aeskey = %q", got)
	}
}

func TestNewEventFileCarriesName(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeFile,
		File:     &FileBody{URL: "https://example.com/a.pdf", Name: "a.pdf", Size: 1024, AesKey: "k"},
	}
	evt := newEvent(cb, "bot")
	atts := platform.Attachments(evt)
	if len(atts) != 1 || atts[0].Name != "a.pdf" || atts[0].Size != 1024 {
		t.Fatalf("文件附件信息错误: %+v", atts)
	}
}

func TestNewEventMixed(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeMixed,
		Mixed: &MixedBody{MsgItem: []MixedItem{
			{MsgType: msgTypeText, Text: &TextBody{Content: "看这张图"}},
			{MsgType: msgTypeImage, Image: &MediaBody{URL: "https://example.com/a.jpg", AesKey: "k"}},
		}},
	}
	evt := newEvent(cb, "bot")
	if got := platform.Content(evt); got != "看这张图" {
		t.Fatalf("正文 = %q", got)
	}
	if atts := platform.Attachments(evt); len(atts) != 1 {
		t.Fatalf("附件数量 = %d", len(atts))
	}
}

func TestNewEventQuote(t *testing.T) {
	cb := textCallback(chatTypeGroup, "user-1", "@RobotA 这个怎么说")
	cb.Quote = &QuoteBody{
		MsgType: msgTypeText,
		Text:    &TextBody{Content: "这是今日的测试情况"},
	}
	evt := newEvent(cb, "bot")
	segs := evt.Segments()
	if len(segs) == 0 || segs[0].Type != platform.SegmentReply {
		t.Fatalf("首段应为 reply 段: %+v", segs)
	}
	if segs[0].Text != "这是今日的测试情况" {
		t.Fatalf("引用正文 = %q", segs[0].Text)
	}
}

func TestNewEventTemplateCard(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeEvent,
		Event:    &EventBody{EventType: eventTypeTemplateCard, EventKey: "retry", TaskID: "t1"},
	}
	evt := newEvent(cb, "bot")
	if evt.Kind() != platform.EventKindInteraction {
		t.Fatalf("Kind = %q, want INTERACTION", evt.Kind())
	}
	if got := platform.Content(evt); got != "retry" {
		t.Fatalf("按钮 key 应作为正文，实际 %q", got)
	}
	if platform.RawType(evt) != eventTypeTemplateCard {
		t.Fatalf("RawType = %q", platform.RawType(evt))
	}
}

func TestNewEventEnterChat(t *testing.T) {
	cb := &Callback{
		MsgID:    "m",
		ChatType: chatTypeSingle,
		From:     From{UserID: "u"},
		MsgType:  msgTypeEvent,
		Event:    &EventBody{EventType: eventTypeEnterChat},
	}
	evt := newEvent(cb, "bot")
	if evt == nil {
		t.Fatal("进入会话事件应产出事件")
	}
	if evt.Kind() != platform.EventKindNotice {
		t.Fatalf("Kind = %q", evt.Kind())
	}
}

func TestNewEventUnsupported(t *testing.T) {
	cb := &Callback{MsgType: "unknown", ChatType: chatTypeSingle, From: From{UserID: "u"}}
	if evt := newEvent(cb, "bot"); evt != nil {
		t.Fatalf("不支持的消息类型应返回 nil，实际 %+v", evt)
	}
	if evt := newEvent(&Callback{MsgType: msgTypeEvent}, "bot"); evt != nil {
		t.Fatal("缺少 event 体的回调应返回 nil")
	}
}

func TestSplitLeadingMention(t *testing.T) {
	cases := []struct {
		in      string
		mention string
		rest    string
		ok      bool
	}{
		{"@Bot hi", "@Bot", "hi", true},
		{"@Bot  hi", "@Bot", "hi", true},
		{"@Bot", "", "@Bot", false},
		{"@ hi", "", "@ hi", false},
		{"hi", "", "hi", false},
		{"@Bot\thi", "@Bot", "hi", true},
	}
	for _, tt := range cases {
		mention, rest, ok := splitLeadingMention(tt.in)
		if mention != tt.mention || rest != tt.rest || ok != tt.ok {
			t.Fatalf("splitLeadingMention(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tt.in, mention, rest, ok, tt.mention, tt.rest, tt.ok)
		}
	}
}
