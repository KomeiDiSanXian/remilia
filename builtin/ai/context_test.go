package ai

import (
	stdctx "context"
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
	l.RecordOutbound("g1", "out-1", "这是机器人的回复", time.Now())
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
	l.RecordOutbound("g1", "out-1", "bot", now.Add(3*time.Second))

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
	l.RecordOutbound("g1", "out-1", "AI 的回复", now.Add(time.Second))
	// 其他插件（如 /pic）的回复也应进入窗口
	l.RecordOutbound("g1", "out-2", "图片结果", now.Add(2*time.Second))

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
