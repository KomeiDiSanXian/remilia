package ai

import (
	stdctx "context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// replyEvent 包装平台事件并附加 ReplyToID，用于构造"回复某条消息"的测试事件。
type replyEvent struct {
	platform.Event
	replyID string
}

func (e *replyEvent) ReplyToID() string { return e.replyID }

// fixedIDSender 返回固定 MessageID 的假发送器。
type fixedIDSender struct{}

func (fixedIDSender) Send(_ stdctx.Context, _ platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{MessageID: "msg-42"}, nil
}

// runTaskDispatcher 异步执行 dispatcher 提交的任务，模拟真实出站调度器。
type runTaskDispatcher struct{}

func (runTaskDispatcher) Submit(_ string, task func(stdctx.Context) error) error {
	go func() { _ = task(stdctx.Background()) }()
	return nil
}

func TestReplyAndRecordRecordsOutbound(t *testing.T) {
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hello",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, fixedIDSender{})
	ctx.SetDispatcher(runTaskDispatcher{})

	p := &Plugin{
		cfg:          &Config{IncludeReplyContext: true},
		history:      messagelog.New(10),
		lifecycleCtx: stdctx.Background(),
	}

	f := p.replyAndRecord(ctx, platform.TextMessage("bot reply"))
	if f == nil {
		t.Fatal("replyAndRecord returned nil future")
	}

	// 等待异步记录完成（真实场景中发送与用户回复之间有足够时间差）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := p.history.QueryByEventID("g1", "msg-42"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	e, ok := p.history.QueryByEventID("g1", "msg-42")
	if !ok {
		t.Fatal("expected outbound message recorded after send")
	}
	if e.Content != "bot reply" || !e.IsOutbound {
		t.Errorf("unexpected outbound entry: %+v", e)
	}
}

func TestReplyAndRecordNotGatedByIncludeReplyContext(t *testing.T) {
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hello",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, fixedIDSender{})
	ctx.SetDispatcher(runTaskDispatcher{})

	p := &Plugin{
		cfg:          &Config{IncludeReplyContext: false},
		history:      messagelog.New(10),
		lifecycleCtx: stdctx.Background(),
	}

	p.replyAndRecord(ctx, platform.TextMessage("bot reply"))

	// 记录是 messagelog 级行为，不受 include_reply_context 控制
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := p.history.QueryByEventID("g1", "msg-42"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := p.history.QueryByEventID("g1", "msg-42"); !ok {
		t.Error("outbound recording should not be gated by include_reply_context")
	}
}

func TestPrependReplyContextInbound(t *testing.T) {
	l := messagelog.New(10)
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserID: "u1", UserName: "小明",
		Content: "今天天气怎么样？", EventID: "in-1", Timestamp: time.Now(),
	})
	p := &Plugin{cfg: &Config{}, history: l}

	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "帮我看看",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
			platform.WithSyntheticSender(platform.UserInfo{ID: "u2", DisplayName: "小红"})),
		replyID: "in-1",
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.prependReplyContext(ctx, "帮我看看")
	if !strings.Contains(got, "小明") || !strings.Contains(got, "今天天气怎么样？") {
		t.Errorf("expected reply context with user message, got %q", got)
	}
	if !strings.Contains(got, "[你正在回复 小明 的消息]") {
		t.Errorf("expected reply context marker, got %q", got)
	}
}

func TestPrependReplyContextBotMessage(t *testing.T) {
	l := messagelog.New(10)
	l.RecordOutboundSent("g1", "out-1", "这是机器人的回复", time.Now())
	p := &Plugin{cfg: &Config{}, history: l}

	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "这个不太明白",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})),
		replyID: "out-1",
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.prependReplyContext(ctx, "这个不太明白")
	if !strings.Contains(got, "机器人") || !strings.Contains(got, "这是机器人的回复") {
		t.Errorf("expected reply context with bot message, got %q", got)
	}
}

// TestPrependReplyContextReplyChain 回复的回复：沿 ReplyToMessageID 向上追溯。
func TestPrependReplyContextReplyChain(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	// 小明回复小红 → 当前消息再回复小明
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserID: "u1", UserName: "小明", Content: "你说得对",
		EventID: "e1", Timestamp: now, ReplyToMessageID: "e2",
	})
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserID: "u2", UserName: "小红", Content: "我觉得不行",
		EventID: "e2", Timestamp: now.Add(-time.Second),
	})
	p := &Plugin{cfg: &Config{}, history: l}

	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "详细说说",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
			platform.WithSyntheticSender(platform.UserInfo{ID: "u3", DisplayName: "阿伟"})),
		replyID: "e1",
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.prependReplyContext(ctx, "详细说说")
	if !strings.Contains(got, "[你正在回复 小明 的消息]") {
		t.Errorf("expected first-level marker, got %q", got)
	}
	if !strings.Contains(got, "小明: 你说得对") {
		t.Errorf("expected first-level content, got %q", got)
	}
	if !strings.Contains(got, "[小明 在回复 小红 的消息]") {
		t.Errorf("expected second-level marker, got %q", got)
	}
	if !strings.Contains(got, "小红: 我觉得不行") {
		t.Errorf("expected second-level content, got %q", got)
	}
}

func TestPrependReplyContextMiss(t *testing.T) {
	p := &Plugin{cfg: &Config{}, history: messagelog.New(10)}

	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hello",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})),
		replyID: "missing",
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if got := p.prependReplyContext(ctx, "原内容"); got != "原内容" {
		t.Errorf("expected content unchanged on miss, got %q", got)
	}

	// 非回复消息
	ctx2 := eventctx.NewContextFromEvent(
		platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hello",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})), nil)
	if got := p.prependReplyContext(ctx2, "原内容"); got != "原内容" {
		t.Errorf("expected content unchanged without reply, got %q", got)
	}
}

// TestPrependReplyContextQQQuoteFallback 覆盖 QQ 引用消息的段兜底：
// 回复标识是 ref_msg_idx（与 messagelog 事件 ID 不对应，查不到），
// 从 reply 段 Extra["parallel_message"] 提取被引用内容。
func TestPrependReplyContextQQQuoteFallback(t *testing.T) {
	p := &Plugin{cfg: &Config{}, history: messagelog.New(10)}

	// SyntheticEvent 无段注入接口：用 segmentsReplyEvent 包装注入 reply 段
	// （QQ 引用消息：回复标识 ref_msg_idx 与 messagelog 事件 ID 不对应，
	// 被引用内容在 reply 段 Extra["parallel_message"]）。
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "789",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})),
		segs: []platform.Segment{{
			Type:      platform.SegmentReply,
			ReplyToID: "REFIDX_y8cQLJYVRPp/g5f0s6c0hstG81ovPjw88HwjHppK6Gc=",
			Extra: map[string]any{
				"parallel_message": `{"msg_nodes":[{"message_type":0,"content":"该命令仅支持群聊"}]}`,
			},
		}},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	got := p.prependReplyContext(ctx, "789")
	if !strings.Contains(got, "该命令仅支持群聊") {
		t.Errorf("expected QQ quote context from parallel_message fallback, got %q", got)
	}
	if !strings.Contains(got, "[你正在回复 对方 的消息]") {
		t.Errorf("expected reply context marker with 对方, got %q", got)
	}
}

// segmentsReplyEvent 包装平台事件并注入有序段（SyntheticEvent 无段注入接口）。
type segmentsReplyEvent struct {
	platform.Event
	segs []platform.Segment
}

func (e *segmentsReplyEvent) Segments() []platform.Segment { return e.segs }

// TestPrependReplyContextQuotedForward 引用的消息是合并转发（QQ 103 引 102）：
// parallel_message 只有 "[聊天记录]" 占位符，被引用记录经 reply 段 Extra 的
// 结构化载荷渲染为可读文本注入回复上下文（优先于占位符兜底）。
func TestPrependReplyContextQuotedForward(t *testing.T) {
	p := &Plugin{cfg: &Config{}, history: messagelog.New(10)}

	rec := &platform.ForwardRecord{
		Title: "月莫法师和蕾米莉亚的聊天记录",
		Nodes: []platform.ForwardNode{
			{Sender: platform.UserInfo{DisplayName: "月莫法师"},
				Segments: []platform.Segment{{Type: platform.SegmentText, Text: "/update now"}}},
			{Sender: platform.UserInfo{DisplayName: "蕾米莉亚"},
				Segments: []platform.Segment{{Type: platform.SegmentImage, Attachment: platform.Attachment{URL: "https://ex/a.png"}}}},
		},
	}
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "能读到这个聊天记录吗？",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})),
		segs: []platform.Segment{
			{
				Type:      platform.SegmentReply,
				ReplyToID: "TMP_93b5da4d",
				Extra: map[string]any{
					platform.SegmentExtraQuotedForward: rec,
					// 占位符并行视图：不应作为上下文内容
					"parallel_message": `{"msg_nodes":[{"message_type":0,"content":"[聊天记录]"}]}`,
				},
			},
			{Type: platform.SegmentText, Text: "能读到这个聊天记录吗？"},
		},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	got := p.prependReplyContext(ctx, "能读到这个聊天记录吗？")
	if !strings.Contains(got, "[你正在回复 对方 的消息]") {
		t.Errorf("expected reply context marker, got %q", got)
	}
	if !strings.Contains(got, "【合并转发聊天记录】月莫法师和蕾米莉亚的聊天记录（共 2 条消息）") {
		t.Errorf("expected forward record rendered, got %q", got)
	}
	if !strings.Contains(got, "1. 月莫法师: /update now") || !strings.Contains(got, "2. 蕾米莉亚: [图片]") {
		t.Errorf("expected record messages rendered, got %q", got)
	}
	if strings.Contains(got, "[聊天记录]\n") {
		t.Errorf("placeholder should not be injected, got %q", got)
	}
}

// TestForwardRecordFromEvent 直发合并转发消息（SegmentForward 段）的
// 结构化载荷提取：命中 / 无段 / 无载荷。
func TestForwardRecordFromEvent(t *testing.T) {
	rec := &platform.ForwardRecord{Nodes: []platform.ForwardNode{{Segments: []platform.Segment{{Type: platform.SegmentText, Text: "hi"}}}}}

	hit := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindPrivateMessage, ""),
		segs: []platform.Segment{{
			Type:  platform.SegmentForward,
			Extra: map[string]any{platform.SegmentExtraForwardNodes: rec},
		}},
	}
	if got := forwardRecordFromEvent(hit); got != rec {
		t.Errorf("expected record from forward segment, got %v", got)
	}

	if got := forwardRecordFromEvent(platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "hello")); got != nil {
		t.Errorf("expected nil without forward segment, got %v", got)
	}
	if got := forwardRecordFromEvent(nil); got != nil {
		t.Errorf("expected nil for nil event, got %v", got)
	}
}

// TestForwardTriggerContent 直发合并转发的触发决策：私聊触发（渲染记录
// 文本），群聊不触发（QQ 转发无法携带 @，不存在显式触发意图）。
func TestForwardTriggerContent(t *testing.T) {
	rec := &platform.ForwardRecord{Title: "T", Nodes: []platform.ForwardNode{
		{Sender: platform.UserInfo{DisplayName: "A"},
			Segments: []platform.Segment{{Type: platform.SegmentText, Text: "hi"}}},
	}}

	text, trigger := forwardTriggerContent(platform.ChatInfo{ID: "u1", IsGroup: false}, rec)
	if !trigger || !strings.Contains(text, "【合并转发聊天记录】T") || !strings.Contains(text, "1. A: hi") {
		t.Errorf("private chat should trigger with rendered record, got trigger=%v text=%q", trigger, text)
	}

	if text, trigger = forwardTriggerContent(platform.ChatInfo{ID: "g1", IsGroup: true}, rec); trigger || text != "" {
		t.Errorf("group chat should not trigger, got trigger=%v text=%q", trigger, text)
	}
}

// TestForwardRecordImageAtts 覆盖记录图片提取：节点顺序、递归嵌套、
// 上限截断。
func TestForwardRecordImageAtts(t *testing.T) {
	rec := &platform.ForwardRecord{Nodes: []platform.ForwardNode{
		{Segments: []platform.Segment{{Type: platform.SegmentText, Text: "纯文本"}}},
		{Segments: []platform.Segment{{Type: platform.SegmentImage,
			Attachment: platform.Attachment{URL: "https://ex/1.png", Kind: platform.AttachmentKindImage}}}},
		{Kind: platform.ForwardKindRecord, Related: []platform.ForwardNode{
			{Segments: []platform.Segment{{Type: platform.SegmentImage,
				Attachment: platform.Attachment{URL: "https://ex/2.png", Kind: platform.AttachmentKindImage}}}},
		}},
		{Segments: []platform.Segment{{Type: platform.SegmentImage,
			Attachment: platform.Attachment{URL: "https://ex/3.png", Kind: platform.AttachmentKindImage}}}},
	}}
	evt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindPrivateMessage, ""),
		segs: []platform.Segment{{
			Type:  platform.SegmentForward,
			Extra: map[string]any{platform.SegmentExtraForwardNodes: rec},
		}},
	}

	atts := forwardRecordImageAtts(evt, 0)
	if len(atts) != 3 || atts[0].URL != "https://ex/1.png" || atts[1].URL != "https://ex/2.png" || atts[2].URL != "https://ex/3.png" {
		t.Errorf("expected 3 images in order (nested included), got %+v", atts)
	}

	atts = forwardRecordImageAtts(evt, 2)
	if len(atts) != 2 || atts[1].URL != "https://ex/2.png" {
		t.Errorf("expected capped to first 2, got %+v", atts)
	}

	if got := forwardRecordImageAtts(platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "text"), 0); got != nil {
		t.Errorf("expected nil without forward record, got %+v", got)
	}
}

// TestBuildUserMessageInjectsForwardRecordImages 直发合并转发（纯图片记录）
// 场景：记录内图片作为视觉输入注入 ContentParts。
//
// 通过预置 session.contentCache 命中缓存分支，绕过真实网络下载。
func TestBuildUserMessageInjectsForwardRecordImages(t *testing.T) {
	const img1 = "https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=1"
	const img2 = "https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=2"
	rec := &platform.ForwardRecord{Title: "T", Nodes: []platform.ForwardNode{
		{Sender: platform.UserInfo{DisplayName: "A"},
			Segments: []platform.Segment{{Type: platform.SegmentImage,
				Attachment: platform.Attachment{URL: img1, Kind: platform.AttachmentKindImage}}}},
		{Sender: platform.UserInfo{DisplayName: "B"},
			Segments: []platform.Segment{{Type: platform.SegmentImage,
				Attachment: platform.Attachment{URL: img2, Kind: platform.AttachmentKindImage}}}},
	}}
	evt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindPrivateMessage, ""),
		segs: []platform.Segment{{
			Type:  platform.SegmentForward,
			Extra: map[string]any{platform.SegmentExtraForwardNodes: rec},
		}},
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{}
	session.setCachedContent(img1, []byte("fake-png-1"), "image/png", "")
	session.setCachedContent(img2, []byte("fake-png-2"), "image/png", "")

	p := &Plugin{cfg: &Config{VisionEnabled: true, MaxImagesPerMessage: 4}}
	msg := p.buildUserMessage(ctx, "【合并转发聊天记录】T\n1. A: [图片]\n2. B: [图片]", session)

	if len(msg.ContentParts) != 3 {
		t.Fatalf("expected 3 content parts (text+2 images), got %d: %+v", len(msg.ContentParts), msg.ContentParts)
	}
	if msg.ContentParts[0].Type != ContentPartText {
		t.Errorf("expected text part first, got %+v", msg.ContentParts[0])
	}
	if msg.ContentParts[1].Type != ContentPartImage || msg.ContentParts[1].MimeType != "image/png" || len(msg.ContentParts[1].Data) == 0 {
		t.Errorf("expected first record image part, got %+v", msg.ContentParts[1])
	}
	if msg.ContentParts[2].Type != ContentPartImage {
		t.Errorf("expected second record image part, got %+v", msg.ContentParts[2])
	}
}

// TestBuildUserMessageForwardRecordImageCap 记录图片超出 max_images_per_message
// 时截断注入（不触发整条拒绝），截断部分在渲染文本中以 [图片] 占位可感知。
func TestBuildUserMessageForwardRecordImageCap(t *testing.T) {
	rec := &platform.ForwardRecord{Nodes: []platform.ForwardNode{
		{Segments: []platform.Segment{{Type: platform.SegmentImage,
			Attachment: platform.Attachment{URL: "https://ex/1.png", Kind: platform.AttachmentKindImage}}}},
		{Segments: []platform.Segment{{Type: platform.SegmentImage,
			Attachment: platform.Attachment{URL: "https://ex/2.png", Kind: platform.AttachmentKindImage}}}},
	}}
	evt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindPrivateMessage, ""),
		segs: []platform.Segment{{
			Type:  platform.SegmentForward,
			Extra: map[string]any{platform.SegmentExtraForwardNodes: rec},
		}},
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{}
	session.setCachedContent("https://ex/1.png", []byte("fake-png-1"), "image/png", "")
	session.setCachedContent("https://ex/2.png", []byte("fake-png-2"), "image/png", "")

	p := &Plugin{cfg: &Config{VisionEnabled: true, MaxImagesPerMessage: 1}}
	msg := p.buildUserMessage(ctx, "【合并转发聊天记录】\n1. A: [图片]\n2. B: [图片]", session)

	if len(msg.ContentParts) != 2 {
		t.Fatalf("expected 2 content parts (text+1 capped image), got %d: %+v", len(msg.ContentParts), msg.ContentParts)
	}
	if msg.ContentParts[1].SourceURL != "https://ex/1.png" {
		t.Errorf("expected only first image injected, got %+v", msg.ContentParts[1])
	}
}

func TestReplyQuoteFromSegments(t *testing.T) {
	// 命中：parallel_message.msg_nodes[0].content
	segs := []platform.Segment{{
		Type:      platform.SegmentReply,
		ReplyToID: "REFIDX_x",
		Extra: map[string]any{
			"parallel_message": `{"msg_nodes":[{"message_type":0,"content":"@蕾米莉亚 123456"}]}`,
		},
	}}
	if got := replyQuoteFromSegments(segs); got != "@蕾米莉亚 123456" {
		t.Errorf("expected quoted content, got %q", got)
	}

	// 无 parallel_message（其他平台）→ 空
	if got := replyQuoteFromSegments([]platform.Segment{{Type: platform.SegmentReply, ReplyToID: "m1"}}); got != "" {
		t.Errorf("expected empty without parallel_message, got %q", got)
	}

	// 非 reply 段 → 空
	if got := replyQuoteFromSegments([]platform.Segment{{Type: platform.SegmentText, Text: "x"}}); got != "" {
		t.Errorf("expected empty without reply segment, got %q", got)
	}

	// 富媒体引用（content 为占位）→ 返回占位，由调用方净化后跳过
	media := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			"parallel_message": `{"msg_nodes":[{"message_type":7,"content":"[图片] "}]}`,
		},
	}}
	if got := replyQuoteFromSegments(media); got != "[图片] " {
		t.Errorf("expected placeholder content for media quote, got %q", got)
	}
}

func TestBuildGroupContext(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小明", Content: "在吗", EventID: "1", Timestamp: now})
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小红", Content: "@123 在的", EventID: "2", Timestamp: now.Add(time.Second)})
	// 合成事件（AI 工具调用）应被跳过
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserName: "AI", Content: "/ping", EventID: "3",
		Platform: "synthetic", Timestamp: now.Add(2 * time.Second),
	})
	// 出站消息不在 QueryGroup 中，天然排除
	l.RecordOutboundSent("g1", "out-1", "bot", now.Add(3*time.Second))

	p := &Plugin{cfg: &Config{ContextGroupMessages: 10}, history: l}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.buildGroupContext(ctx, nil)
	if !strings.Contains(got, "小明: 在吗") {
		t.Errorf("expected user message in group context, got %q", got)
	}
	if !strings.Contains(got, "小红: 在的") {
		t.Errorf("expected mention markup stripped in group context, got %q", got)
	}
	if strings.Contains(got, "@123") {
		t.Errorf("mention markup should be stripped, got %q", got)
	}
	if strings.Contains(got, "/ping") || strings.Contains(got, "AI:") {
		t.Errorf("synthetic events should be skipped, got %q", got)
	}
	if strings.Contains(got, "bot") {
		t.Errorf("outbound messages should not appear in group window, got %q", got)
	}
}

func TestBuildGroupContextIncludeBotAndDedup(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小明", Content: "你好", EventID: "1", Timestamp: now})
	l.RecordOutboundSent("g1", "out-1", "AI 的回复", now.Add(time.Second))
	// 其他插件（如 /pic）的回复也应进入窗口
	l.RecordOutboundSent("g1", "out-2", "图片结果", now.Add(2*time.Second))

	p := &Plugin{cfg: &Config{ContextGroupMessages: 10, ContextGroupIncludeBot: true}, history: l}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	// 注入机器人名称：出站消息应以机器人自身名称标注
	ctx.SetBotName("蕾米莉亚")

	got := p.buildGroupContext(ctx, nil)
	if !strings.Contains(got, "蕾米莉亚: AI 的回复") {
		t.Errorf("expected bot outbound labeled with bot name, got %q", got)
	}
	if !strings.Contains(got, "蕾米莉亚: 图片结果") {
		t.Errorf("expected other plugin reply in group window, got %q", got)
	}
	if !strings.Contains(got, "小明: 你好") {
		t.Errorf("expected user message in group window, got %q", got)
	}
	// 提示行：说明本账号消息的归属，避免 AI 误认为其他账号发言
	if !strings.Contains(got, "由本机器人账号发出") || !strings.Contains(got, "蕾米莉亚") {
		t.Errorf("expected attribution hint in group window, got %q", got)
	}

	// 会话历史已含 "AI 的回复"（assistant 轮次）：开启去重后该条目被跳过
	skip := map[string]bool{"AI 的回复": true}
	got = p.buildGroupContext(ctx, skip)
	if strings.Contains(got, "AI 的回复") {
		t.Errorf("expected dedup against session history, got %q", got)
	}
	if !strings.Contains(got, "图片结果") {
		t.Errorf("other plugin reply should not be deduped, got %q", got)
	}

	// 未注入机器人名称时兜底"机器人"
	ctx2 := eventctx.NewContextFromEvent(evt, nil)
	got2 := p.buildGroupContext(ctx2, nil)
	if !strings.Contains(got2, "机器人: AI 的回复") {
		t.Errorf("expected fallback label, got %q", got2)
	}
}

func TestBuildGroupContextSkipsUnsentOutbound(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小明", Content: "你好", EventID: "1", Timestamp: now})
	l.RecordOutboundSent("g1", "out-1", "成功回复", now.Add(time.Second))
	// 发送失败的回复：不应以机器人身份注入群窗口
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", IsOutbound: true, Content: "失败回复", EventID: "2",
		Timestamp: now.Add(2 * time.Second), SendStatus: messagelog.SendStatusFailed,
	})
	// unknown（发送结果不明）同样跳过
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", IsOutbound: true, Content: "未知回复", EventID: "3",
		Timestamp: now.Add(3 * time.Second), SendStatus: messagelog.SendStatusUnknown,
	})
	// pending（未确认发送）由查询层 ExcludePending 排除
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", IsOutbound: true, Content: "发送中回复", EventID: "4",
		Timestamp: now.Add(4 * time.Second), SendStatus: messagelog.SendStatusPending,
	})

	p := &Plugin{cfg: &Config{ContextGroupMessages: 10, ContextGroupIncludeBot: true}, history: l}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	ctx.SetBotName("蕾米莉亚")

	got := p.buildGroupContext(ctx, nil)
	if !strings.Contains(got, "成功回复") {
		t.Errorf("expected confirmed reply in window, got %q", got)
	}
	if !strings.Contains(got, "小明: 你好") {
		t.Errorf("expected user message in window, got %q", got)
	}
	for _, bad := range []string{"失败回复", "未知回复", "发送中回复"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsent outbound %q must not appear as bot speech, got %q", bad, got)
		}
	}
}

// TestBuildGroupContextReplyInline 窗口条目是回复时内联被回复内容。
func TestBuildGroupContextReplyInline(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserName: "小红", Content: "我觉得不行",
		EventID: "e2", Timestamp: now.Add(-time.Second),
	})
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserName: "小明", Content: "你说得对",
		EventID: "e1", Timestamp: now, ReplyToMessageID: "e2",
	})

	p := &Plugin{cfg: &Config{ContextGroupMessages: 10}, history: l}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.buildGroupContext(ctx, nil)
	if !strings.Contains(got, "小明: 你说得对（回复 小红: 我觉得不行）") {
		t.Errorf("expected reply inline suffix, got %q", got)
	}
}

// TestBuildGroupContextMentions 窗口条目标注 @ 提及对象（跳过机器人自身）。
func TestBuildGroupContextMentions(t *testing.T) {
	l := messagelog.New(10)
	now := time.Now()
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserName: "小明", Content: "在吗",
		EventID: "1", Timestamp: now,
		Mentions: []platform.UserInfo{
			{ID: "u2", DisplayName: "小红"},
			{ID: "bot", DisplayName: "蕾米莉亚", IsSelf: true},
			{ID: "u4", DisplayName: "小刚"},
		},
	})
	// 无 Mentions 的普通条目不受影响
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserName: "小红", Content: "好的", EventID: "2",
		Timestamp: now.Add(time.Second),
	})

	p := &Plugin{cfg: &Config{ContextGroupMessages: 10}, history: l}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.buildGroupContext(ctx, nil)
	if !strings.Contains(got, "小明: 在吗（@小红、@小刚）") {
		t.Errorf("expected mention annotation, got %q", got)
	}
	if strings.Contains(got, "蕾米莉亚") {
		t.Errorf("bot self-mention should be skipped, got %q", got)
	}
	if !strings.Contains(got, "小红: 好的") {
		t.Errorf("entry without mentions should render unchanged, got %q", got)
	}
}

func TestBuildGroupContextDisabled(t *testing.T) {
	p := &Plugin{cfg: &Config{ContextGroupMessages: 0}, history: messagelog.New(10)}
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	if got := p.buildGroupContext(ctx, nil); got != "" {
		t.Errorf("expected empty when context_group_messages=0, got %q", got)
	}

	// 非群聊
	evt2 := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	p2 := &Plugin{cfg: &Config{ContextGroupMessages: 10}, history: messagelog.New(10)}
	if got := p2.buildGroupContext(eventctx.NewContextFromEvent(evt2, nil), nil); got != "" {
		t.Errorf("expected empty for private chat, got %q", got)
	}

	// history 为 nil
	p3 := &Plugin{cfg: &Config{ContextGroupMessages: 10}, history: nil}
	if got := p3.buildGroupContext(ctx, nil); got != "" {
		t.Errorf("expected empty when history unavailable, got %q", got)
	}
}

func TestBuildRuntimeContextUserIsBot(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "/test",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", IsBot: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p := &Plugin{cfg: &Config{ContextFields: []string{"user_is_bot"}}}
	runtime := p.buildRuntimeContext(ctx)
	if !strings.Contains(runtime, "发送者是否为机器人: 是") {
		t.Errorf("expected bot flag in runtime context, got %q", runtime)
	}

	evt2 := platform.NewSyntheticEvent("c2c", "/test",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u2", IsBot: false}))
	p2 := &Plugin{cfg: &Config{ContextFields: []string{"user_is_bot"}}}
	if runtime := p2.buildRuntimeContext(eventctx.NewContextFromEvent(evt2, nil)); !strings.Contains(runtime, "发送者是否为机器人: 否") {
		t.Errorf("expected non-bot flag in runtime context, got %q", runtime)
	}
}

// ── 引用消息图片提取（quotedImageFromSegments）──────────────────────────

// TestQuotedImageFromRawQuote QQ 引用富媒体消息：raw_quote 的
// msg_elements[].attachments[] 携带被引用图片（2026-08 实测报文结构）。
func TestQuotedImageFromRawQuote(t *testing.T) {
	segs := []platform.Segment{{
		Type:      platform.SegmentReply,
		ReplyToID: "REFIDX_x",
		Extra: map[string]any{
			"raw_quote": `[{"message_type":7,"content":"","attachments":[` +
				`{"url":"https://multimedia.nt.qq.com.cn/download?appid=1407&spec=0","content_type":"image/jpeg","width":1070,"height":473,"size":49276}]}]`,
		},
	}}
	url, mime := quotedImageFromSegments(segs)
	if url != "https://multimedia.nt.qq.com.cn/download?appid=1407&spec=0" || mime != "image/jpeg" {
		t.Errorf("expected quoted image url+mime, got (%q, %q)", url, mime)
	}
}

// TestQuotedImageParallelFallback raw_quote 缺失/无附件时回退 parallel_message。
func TestQuotedImageParallelFallback(t *testing.T) {
	segs := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			"raw_quote":        `[{"message_type":7,"content":"[图片] "}]`,
			"parallel_message": `{"msg_nodes":[{"message_type":7,"content":"[图片] ","attachments":[{"url":"https://ex.com/b.png","content_type":"image/png"}]}]}`,
		},
	}}
	url, mime := quotedImageFromSegments(segs)
	if url != "https://ex.com/b.png" || mime != "image/png" {
		t.Errorf("expected parallel fallback image, got (%q, %q)", url, mime)
	}

	// 仅 parallel_message（无 raw_quote）
	segs2 := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			"parallel_message": `{"msg_nodes":[{"attachments":[{"url":"https://ex.com/c.gif","content_type":"image/gif"}]}]}`,
		},
	}}
	if url, _ := quotedImageFromSegments(segs2); url != "https://ex.com/c.gif" {
		t.Errorf("expected image from parallel only, got %q", url)
	}
}

// TestQuotedImageSkipsNonImage 非 image/* 附件跳过，取后续图片项。
func TestQuotedImageSkipsNonImage(t *testing.T) {
	segs := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			"raw_quote": `[{"attachments":[{"url":"https://ex.com/v.mp4","content_type":"video/mp4"},{"url":"https://ex.com/a.jpg","content_type":"image/jpeg"}]}]`,
		},
	}}
	url, mime := quotedImageFromSegments(segs)
	if url != "https://ex.com/a.jpg" || mime != "image/jpeg" {
		t.Errorf("expected image attachment picked over video, got (%q, %q)", url, mime)
	}
}

// TestQuotedImageUntypedFallback 所有附件均未标注 content_type 时回退首个带 url 项。
func TestQuotedImageUntypedFallback(t *testing.T) {
	segs := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			"raw_quote": `[{"attachments":[{"url":"https://ex.com/no-type"}]}]`,
		},
	}}
	url, mime := quotedImageFromSegments(segs)
	if url != "https://ex.com/no-type" || mime != "" {
		t.Errorf("expected fallback to untyped attachment, got (%q, %q)", url, mime)
	}
}

// TestQuotedImageNone 无引用段 / 非法 JSON / 无 Extra 时返回空。
func TestQuotedImageNone(t *testing.T) {
	cases := [][]platform.Segment{
		nil,
		{{Type: platform.SegmentText, Text: "hello"}},
		{{Type: platform.SegmentReply, ReplyToID: "m1"}},
		{{Type: platform.SegmentReply, Extra: map[string]any{"raw_quote": "{invalid json"}}},
		{{Type: platform.SegmentReply, Extra: map[string]any{
			"parallel_message": `{"msg_nodes":[{"message_type":0,"content":"纯文本引用"}]}`,
		}}},
	}
	for i, segs := range cases {
		if url, _ := quotedImageFromSegments(segs); url != "" {
			t.Errorf("case %d: expected empty, got %q", i, url)
		}
	}
}

// ── buildUserMessage 引用图片注入 ────────────────────────────────────────

// TestQuotedImagePrefersTypedExtra 平台归一化引用附件（SegmentExtraQuoteAtts）
// 优先于 QQ 原始 JSON 兜底路径。
func TestQuotedImagePrefersTypedExtra(t *testing.T) {
	segs := []platform.Segment{{
		Type: platform.SegmentReply,
		Extra: map[string]any{
			platform.SegmentExtraQuoteAtts: []platform.Attachment{
				{Kind: platform.AttachmentKindImage, URL: "https://typed.example/a.png", MimeType: "image/png"},
			},
			"raw_quote": `[{"attachments":[{"url":"https://raw.example/b.jpg","content_type":"image/jpeg"}]}]`,
		},
	}}
	url, mime := quotedImageFromSegments(segs)
	if url != "https://typed.example/a.png" || mime != "image/png" {
		t.Errorf("expected typed Extra to win, got (%q, %q)", url, mime)
	}
}

// TestPickQuotedImage 归一化附件选择：Kind/MimeType 显式图片优先；
// 显式非图片类型跳过；全部未标注类型时回退首个带 URL 的项。
func TestPickQuotedImage(t *testing.T) {
	// Kind 标注优先
	url, _ := platform.PickQuotedImage([]platform.Attachment{
		{Kind: platform.AttachmentKindFile, URL: "https://x/doc.pdf", Name: "doc.pdf"},
		{Kind: platform.AttachmentKindImage, URL: "https://x/pic.jpg"},
	})
	if url != "https://x/pic.jpg" {
		t.Errorf("expected image-kind attachment, got %q", url)
	}

	// MimeType 标注优先于未标注项
	url, mime := platform.PickQuotedImage([]platform.Attachment{
		{URL: "https://x/unknown"},
		{URL: "https://x/a.png", MimeType: "image/png"},
	})
	if url != "https://x/a.png" || mime != "image/png" {
		t.Errorf("expected mime-typed image, got (%q, %q)", url, mime)
	}

	// 显式标注的非图片类型不作为兜底
	if url, _ := platform.PickQuotedImage([]platform.Attachment{
		{Kind: platform.AttachmentKindVideo, URL: "https://x/v.mp4"},
	}); url != "" {
		t.Errorf("expected typed non-image to be skipped, got %q", url)
	}

	// 无类型标注回退首个
	url, mime = platform.PickQuotedImage([]platform.Attachment{{URL: "https://x/unknown"}})
	if url != "https://x/unknown" || mime != "" {
		t.Errorf("expected untyped fallback, got (%q, %q)", url, mime)
	}

	// 无可用项
	if url, _ := platform.PickQuotedImage([]platform.Attachment{{Kind: platform.AttachmentKindVideo}}); url != "" {
		t.Errorf("expected empty without URL, got %q", url)
	}
}

// TestBuildUserMessageInjectsQuotedImage 引用图片场景：本条消息无图片附件，
// 从 reply 段提取被引用图片作为视觉输入注入。
//
// 通过预置 session.contentCache 命中缓存分支，绕过真实网络下载
// （SSRF 防护会拦截 httptest 的 loopback 地址），覆盖注入链路本身。
func TestBuildUserMessageInjectsQuotedImage(t *testing.T) {
	const imgURL = "https://multimedia.nt.qq.com.cn/download?appid=1407&spec=0"
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, ""),
		segs: []platform.Segment{{
			Type:      platform.SegmentReply,
			ReplyToID: "REFIDX_x",
			Extra: map[string]any{
				"raw_quote": fmt.Sprintf(`[{"attachments":[{"url":%q,"content_type":"image/jpeg"}]}]`, imgURL),
			},
		}},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	session := &Session{}
	session.setCachedContent(imgURL, []byte("fake-jpeg-bytes"), "image/jpeg", "")

	p := &Plugin{cfg: &Config{VisionEnabled: true}}
	msg := p.buildUserMessage(ctx, "这是什么", session)

	if len(msg.ContentParts) != 3 {
		t.Fatalf("expected 3 content parts (text+note+image), got %d: %+v", len(msg.ContentParts), msg.ContentParts)
	}
	if msg.ContentParts[0].Type != ContentPartText || msg.ContentParts[0].Text != "这是什么" {
		t.Errorf("expected user text part first, got %+v", msg.ContentParts[0])
	}
	if msg.ContentParts[1].Type != ContentPartText || !strings.Contains(msg.ContentParts[1].Text, "引用") {
		t.Errorf("expected quote note text part, got %+v", msg.ContentParts[1])
	}
	img := msg.ContentParts[2]
	if img.Type != ContentPartImage || img.MimeType != "image/jpeg" || len(img.Data) == 0 {
		t.Errorf("expected downloaded image part, got %+v", img)
	}
	if msg.Content != "" {
		t.Errorf("expected empty Content in parts mode, got %q", msg.Content)
	}
}

// TestBuildUserMessageQuotedImageDisabled vision_enabled=false 时忽略引用图片，
// 不触发任何下载尝试，消息保持纯文本。
func TestBuildUserMessageQuotedImageDisabled(t *testing.T) {
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, ""),
		segs: []platform.Segment{{
			Type: platform.SegmentReply,
			Extra: map[string]any{
				"raw_quote": `[{"attachments":[{"url":"https://ex.com/img.jpg","content_type":"image/jpeg"}]}]`,
			},
		}},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	p := &Plugin{cfg: &Config{VisionEnabled: false}}
	msg := p.buildUserMessage(ctx, "这是什么", &Session{})

	if msg.Content != "这是什么" || msg.ContentParts != nil {
		t.Errorf("expected plain text message unchanged, got content=%q parts=%+v", msg.Content, msg.ContentParts)
	}
}

// TestBuildUserMessageQuotedImageDownloadFail 下载失败时优雅降级为纯文本。
func TestBuildUserMessageQuotedImageDownloadFail(t *testing.T) {
	// 内网地址被 SSRF 防护拦截 → 下载必然失败
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, ""),
		segs: []platform.Segment{{
			Type: platform.SegmentReply,
			Extra: map[string]any{
				"raw_quote": `[{"attachments":[{"url":"http://127.0.0.1:1/x.jpg","content_type":"image/jpeg"}]}]`,
			},
		}},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	p := &Plugin{cfg: &Config{VisionEnabled: true}}
	msg := p.buildUserMessage(ctx, "这是什么", &Session{})

	if msg.Content != "这是什么" || msg.ContentParts != nil {
		t.Errorf("expected graceful fallback to plain text, got content=%q parts=%+v", msg.Content, msg.ContentParts)
	}
}

// TestHasImageAttachment Kind 与 MimeType 双通道识别图片附件
// （OneBot 只填 Kind、QQ 只填 MimeType、Telegram 两者皆空）。
func TestHasImageAttachment(t *testing.T) {
	cases := []struct {
		name string
		atts []platform.Attachment
		want bool
	}{
		{"Kind-only (OneBot)", []platform.Attachment{{Kind: platform.AttachmentKindImage, URL: "https://x/a.jpg"}}, true},
		{"MimeType-only (QQ)", []platform.Attachment{{MimeType: "image/jpeg", URL: "https://x/a.jpg"}}, true},
		{"both", []platform.Attachment{{Kind: platform.AttachmentKindImage, MimeType: "image/png"}}, true},
		{"audio not image", []platform.Attachment{{Kind: platform.AttachmentKindAudio, MimeType: "audio/ogg"}}, false},
		{"untyped (Telegram photo)", []platform.Attachment{{URL: "https://x/a.jpg"}}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := hasImageAttachment(tc.atts); got != tc.want {
			t.Errorf("%s: hasImageAttachment=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestBuildUserMessageOwnImageSkipsQuotedImage 自身消息携带图片（Kind 标注、
// OneBot 风格）时不再注入引用图片，避免重复/语义混淆。
func TestBuildUserMessageOwnImageSkipsQuotedImage(t *testing.T) {
	const quotedURL = "https://quoted.example/q.jpg"
	segsEvt := &segmentsReplyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, ""),
		segs: []platform.Segment{
			{
				Type: platform.SegmentImage,
				Attachment: platform.Attachment{
					Kind: platform.AttachmentKindImage,
					URL:  "https://own.example/own.jpg",
				},
			},
			{
				Type:      platform.SegmentReply,
				ReplyToID: "m1",
				Extra: map[string]any{
					platform.SegmentExtraQuoteAtts: []platform.Attachment{
						{Kind: platform.AttachmentKindImage, URL: quotedURL, MimeType: "image/jpeg"},
					},
				},
			},
		},
	}
	ctx := eventctx.NewContextFromEvent(segsEvt, nil)

	session := &Session{}
	session.setCachedContent(quotedURL, []byte("fake-jpeg"), "image/jpeg", "")

	p := &Plugin{cfg: &Config{VisionEnabled: true}}
	msg := p.buildUserMessage(ctx, "看图", session)

	for _, part := range msg.ContentParts {
		if part.Type == ContentPartText && strings.Contains(part.Text, "引用") {
			t.Fatalf("quoted image should not be injected when own image present: %+v", msg.ContentParts)
		}
	}
}
