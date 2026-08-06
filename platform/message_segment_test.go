package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// SegmentsContent
// ────────────────────────────────────────────────────────────────────────────

func TestSegmentsContent(t *testing.T) {
	segs := []Segment{
		{Type: SegmentAt, UserID: "A", Text: "A用户"},
		{Type: SegmentText, Text: "一段文本 "},
		{Type: SegmentAt, UserID: "B", Text: "B用户"},
		{Type: SegmentText, Text: "又是一段文本 "},
		{Type: SegmentAt, UserID: "C"},
		{Type: SegmentAt, UserID: "D"},
		{Type: SegmentText, Text: "文本..."},
		{Type: SegmentAt, UserID: "E"},
	}
	// at 剥离：仅 text 段拼接
	assert.Equal(t, "一段文本 又是一段文本 文本...", SegmentsContent(segs))
}

func TestSegmentsContent_SpecialTypesSkipped(t *testing.T) {
	segs := []Segment{
		{Type: SegmentMentionAll},
		{Type: SegmentFace, FaceID: "21"},
		{Type: SegmentReply, ReplyToID: "r1"},
		{Type: SegmentForward, Extra: map[string]any{SegmentExtraSummary: "s"}},
		{Type: SegmentButton},
		{Type: SegmentUnknown},
		{Type: SegmentImage, Attachment: Attachment{URL: "u"}},
	}
	assert.Equal(t, "", SegmentsContent(segs))
}

// ────────────────────────────────────────────────────────────────────────────
// SegmentsAttachments
// ────────────────────────────────────────────────────────────────────────────

func TestSegmentsAttachments(t *testing.T) {
	segs := []Segment{
		{Type: SegmentText, Text: "x"},
		{Type: SegmentImage, Attachment: Attachment{URL: "img1"}},
		{Type: SegmentFace, FaceID: "21"},
		{Type: SegmentAudio, Attachment: Attachment{URL: "a1"}},
		{Type: SegmentVideo, Attachment: Attachment{URL: "v1"}},
		{Type: SegmentFile, Attachment: Attachment{URL: "f1"}},
	}
	atts := SegmentsAttachments(segs)
	require.Len(t, atts, 4)
	assert.Equal(t, "img1", atts[0].URL)
	assert.Equal(t, "a1", atts[1].URL)
	assert.Equal(t, "v1", atts[2].URL)
	assert.Equal(t, "f1", atts[3].URL)
}

// ────────────────────────────────────────────────────────────────────────────
// SegmentsMentions（聚合视图：保序去重、含自身 IsSelf=true、排除 mention_all）
// ────────────────────────────────────────────────────────────────────────────

func TestSegmentsMentions(t *testing.T) {
	segs := []Segment{
		{Type: SegmentMentionAll},
		{Type: SegmentAt, UserID: "A", Text: "A用户"},
		{Type: SegmentText, Text: "文本"},
		{Type: SegmentAt, UserID: "B"},
		{Type: SegmentAt, UserID: "C"},
		{Type: SegmentAt, UserID: "C"}, // 去重
		{Type: SegmentAt, UserID: "A"}, // 去重（保持首次出现顺序）
	}
	ms := SegmentsMentions(segs, "")
	require.Len(t, ms, 3)
	assert.Equal(t, "A", ms[0].ID)
	assert.Equal(t, "A用户", ms[0].DisplayName)
	assert.Equal(t, "B", ms[1].ID)
	assert.Equal(t, "C", ms[2].ID)
}

func TestSegmentsMentions_IsSelfByBotID(t *testing.T) {
	segs := []Segment{
		{Type: SegmentAt, UserID: "bot1", Text: "bot"},
		{Type: SegmentAt, UserID: "u1"},
	}
	ms := SegmentsMentions(segs, "bot1")
	require.Len(t, ms, 2)
	assert.True(t, ms[0].IsSelf, "UserID==botID 应标记 IsSelf")
	assert.True(t, ms[0].IsBot)
	assert.False(t, ms[1].IsSelf)
}

func TestSegmentsMentions_IsSelfOverride(t *testing.T) {
	// 平台 payload 自带自我标记（qq is_you / satori is_self）→ Extra 覆盖优先
	segs := []Segment{
		{Type: SegmentAt, UserID: "u1", Extra: map[string]any{SegmentExtraIsSelf: true}},
	}
	ms := SegmentsMentions(segs, "bot1") // botID 不匹配，但 Extra 覆盖
	require.Len(t, ms, 1)
	assert.True(t, ms[0].IsSelf)
}

func TestSegmentsMentions_SkipNoUserID(t *testing.T) {
	segs := []Segment{
		{Type: SegmentAt, Text: "@username"}, // telegram 纯 @username 无 UserID
		{Type: SegmentAt, UserID: "u1"},
	}
	ms := SegmentsMentions(segs, "")
	require.Len(t, ms, 1)
	assert.Equal(t, "u1", ms[0].ID)
}

// ────────────────────────────────────────────────────────────────────────────
// SegmentsReplyToID
// ────────────────────────────────────────────────────────────────────────────

func TestSegmentsReplyToID(t *testing.T) {
	assert.Equal(t, "r1", SegmentsReplyToID([]Segment{
		{Type: SegmentText, Text: "x"},
		{Type: SegmentReply, ReplyToID: "r1"},
	}))
	assert.Equal(t, "", SegmentsReplyToID([]Segment{{Type: SegmentText, Text: "x"}}))
}

// ────────────────────────────────────────────────────────────────────────────
// 分散 at 基准用例：段 → 出站段 往返保真
// ────────────────────────────────────────────────────────────────────────────

func TestMessageToOutbound_InterleavedAtRoundTrip(t *testing.T) {
	segs := []Segment{
		{Type: SegmentAt, UserID: "A", Text: "A用户"},
		{Type: SegmentText, Text: "一段文本 "},
		{Type: SegmentAt, UserID: "B", Text: "B用户"},
		{Type: SegmentText, Text: "又是一段文本 "},
		{Type: SegmentAt, UserID: "C"},
		{Type: SegmentAt, UserID: "D"},
		{Type: SegmentText, Text: "文本..."},
		{Type: SegmentAt, UserID: "E"},
	}
	m := Message{Platform: "onebot", Segments: segs}
	out := MessageToOutbound(m, WithTargetPlatform("onebot"))
	require.Len(t, out.Segments, len(segs))
	for i := range segs {
		assert.Equal(t, segs[i], out.Segments[i], "第 %d 段应保真", i)
	}
	// 便捷字段派生
	assert.Equal(t, "一段文本 又是一段文本 文本...", out.Text)
	assert.Equal(t, []string{"A", "B", "C", "D", "E"}, out.Mentions)
}

// ────────────────────────────────────────────────────────────────────────────
// 转发处置表（转发处置表）：同平台透传 vs 跨平台降级
// ────────────────────────────────────────────────────────────────────────────

func TestMessageToOutbound_SamePlatformPassthrough(t *testing.T) {
	segs := []Segment{
		{Type: SegmentReply, ReplyToID: "r1"},
		{Type: SegmentFace, FaceID: "21"},
		{Type: SegmentForward, Extra: map[string]any{"forward_id": "f1"}},
		{Type: SegmentButton, Text: "btn"},
		{Type: SegmentUnknown, Extra: map[string]any{"type": "xml"}},
		{Type: SegmentText, Text: "hi"},
	}
	m := Message{Platform: "onebot", Segments: segs}
	out := MessageToOutbound(m, WithTargetPlatform("onebot"))
	require.Len(t, out.Segments, len(segs))
	for i := range segs {
		assert.Equal(t, segs[i], out.Segments[i], "同平台应全量透传（第 %d 段）", i)
	}
}

func TestMessageToOutbound_CrossPlatformDegrade(t *testing.T) {
	segs := []Segment{
		{Type: SegmentReply, ReplyToID: "r1"},                                    // → 剥离
		{Type: SegmentFace, FaceID: "21"},                                        // → text("21")
		{Type: SegmentForward, Extra: map[string]any{SegmentExtraSummary: "摘要"}}, // → text("摘要")
		{Type: SegmentForward},                                                   // 无摘要 → 剥离
		{Type: SegmentButton, Text: "btn"},                                       // → 剥离
		{Type: SegmentUnknown, Extra: map[string]any{"type": "xml"}},             // → 剥离
		{Type: SegmentText, Text: "hi"},                                          // → 透传
		{Type: SegmentAt, UserID: "A"},                                           // → 透传
	}
	m := Message{Platform: "onebot", Segments: segs}
	out := MessageToOutbound(m) // 缺省 = 保守跨平台
	require.Len(t, out.Segments, 4)
	assert.Equal(t, SegmentText, out.Segments[0].Type)
	assert.Equal(t, "21", out.Segments[0].Text) // face 降级为 text
	assert.Equal(t, "摘要", out.Segments[1].Text)
	assert.Equal(t, "hi", out.Segments[2].Text)
	assert.Equal(t, SegmentAt, out.Segments[3].Type)
	assert.Equal(t, "A", out.Segments[3].UserID)
	assert.Empty(t, out.ReplyToID)
	assert.Equal(t, []string{"A"}, out.Mentions)
}

func TestMessageToOutbound_WithDegradeOverride(t *testing.T) {
	segs := []Segment{{Type: SegmentForward, Extra: map[string]any{"forward_id": "f1"}}}
	m := Message{Platform: "onebot", Segments: segs}
	out := MessageToOutbound(m, WithDegrade(func(s Segment, same bool) []Segment {
		if s.Type == SegmentForward {
			return []Segment{{Type: SegmentText, Text: "custom-forward"}}
		}
		return []Segment{s}
	}))
	require.Len(t, out.Segments, 1)
	assert.Equal(t, "custom-forward", out.Segments[0].Text)
}

// ────────────────────────────────────────────────────────────────────────────
// SegmentsToOutbound / OutboundSegments
// ────────────────────────────────────────────────────────────────────────────

func TestSegmentsToOutbound_DerivesConvenience(t *testing.T) {
	segs := []Segment{
		{Type: SegmentReply, ReplyToID: "r1"},
		{Type: SegmentAt, UserID: "A"},
		{Type: SegmentText, Text: "hi"},
		{Type: SegmentImage, Attachment: Attachment{URL: "u1"}},
	}
	out := SegmentsToOutbound(segs)
	assert.Equal(t, "hi", out.Text)
	assert.Equal(t, []string{"A"}, out.Mentions)
	assert.Equal(t, "r1", out.ReplyToID)
	require.Len(t, out.Attachments, 1)
	assert.Equal(t, "u1", out.Attachments[0].URL)
	assert.False(t, out.IsEmpty())
}

func TestOutboundSegments_FromFlatFields(t *testing.T) {
	m := OutboundMessage{
		ReplyToID:   "r1",
		Mentions:    []string{"A", "B"},
		Text:        "hi",
		Attachments: []Attachment{{Kind: AttachmentKindImage, URL: "u1"}},
	}
	segs := OutboundSegments(m)
	require.Len(t, segs, 5)
	assert.Equal(t, SegmentReply, segs[0].Type)
	assert.Equal(t, "r1", segs[0].ReplyToID)
	assert.Equal(t, SegmentAt, segs[1].Type)
	assert.Equal(t, "A", segs[1].UserID)
	assert.Equal(t, SegmentText, segs[3].Type)
	assert.Equal(t, "hi", segs[3].Text)
	assert.Equal(t, SegmentImage, segs[4].Type)
	// 已有 Segments 时原样返回
	m2 := OutboundMessage{Segments: segs, Text: "other"}
	assert.Equal(t, segs, OutboundSegments(m2))
}

func TestOutboundSegments_MarkdownMarked(t *testing.T) {
	m := OutboundMessage{Markdown: "**bold**"}
	segs := OutboundSegments(m)
	require.Len(t, segs, 1)
	assert.Equal(t, SegmentText, segs[0].Type)
	v, ok := segs[0].Extra["markdown"]
	require.True(t, ok)
	assert.Equal(t, true, v)
}

func TestOutboundMessage_IsEmptyWithSegments(t *testing.T) {
	assert.True(t, (OutboundMessage{}).IsEmpty())
	assert.False(t, (OutboundMessage{Segments: []Segment{{Type: SegmentText, Text: "x"}}}).IsEmpty())
}
