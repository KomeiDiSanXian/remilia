package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQuotedImageFromForwardRecord 被引用消息是合并转发时（103 引用 102，
// reply 段 Extra[SegmentExtraQuotedForward]），从记录节点提取首个图片：
// 按节点顺序、跳过纯文本节点、递归嵌套子条目。
func TestQuotedImageFromForwardRecord(t *testing.T) {
	rec := &ForwardRecord{Nodes: []ForwardNode{
		{Segments: []Segment{{Type: SegmentText, Text: "纯文本"}}},
		{Segments: []Segment{
			{Type: SegmentImage, Attachment: Attachment{Kind: AttachmentKindImage, URL: "https://ex/first.png"}},
		}},
		{
			Kind: ForwardKindRecord,
			Related: []ForwardNode{
				{Segments: []Segment{{Type: SegmentImage, Attachment: Attachment{Kind: AttachmentKindImage, URL: "https://ex/nested.png"}}}},
			},
		},
	}}
	segs := []Segment{{Type: SegmentReply, Extra: map[string]any{SegmentExtraQuotedForward: rec}}}

	url, mime := QuotedImage(segs)
	assert.Equal(t, "https://ex/first.png", url, "应取记录中首个图片（嵌套的不抢占）")
	assert.Equal(t, "", mime)

	// 仅嵌套图片：递归提取
	recNestedOnly := &ForwardRecord{Nodes: []ForwardNode{
		{
			Kind: ForwardKindRecord,
			Related: []ForwardNode{
				{Segments: []Segment{{Type: SegmentImage, Attachment: Attachment{Kind: AttachmentKindImage, URL: "https://ex/nested.png"}}}},
			},
		},
	}}
	url, _ = QuotedImage([]Segment{{Type: SegmentReply, Extra: map[string]any{SegmentExtraQuotedForward: recNestedOnly}}})
	assert.Equal(t, "https://ex/nested.png", url)

	// 无图片记录 → 空（继续走后续兜底路径）
	recNoImage := &ForwardRecord{Nodes: []ForwardNode{
		{Segments: []Segment{{Type: SegmentText, Text: "无图片"}}},
	}}
	url, _ = QuotedImage([]Segment{{Type: SegmentReply, Extra: map[string]any{
		SegmentExtraQuotedForward: recNoImage,
		"raw_quote":               `[{"attachments":[{"url":"https://ex/raw.png","content_type":"image/png"}]}]`,
	}}})
	assert.Equal(t, "https://ex/raw.png", url, "记录无图片时回退 raw_quote 兜底")
}

// TestQuotedImageTypedAttsWinOverForwardRecord 归一化引用附件
// （SegmentExtraQuoteAtts）优先于转发记录载荷。
func TestQuotedImageTypedAttsWinOverForwardRecord(t *testing.T) {
	rec := &ForwardRecord{Nodes: []ForwardNode{
		{Segments: []Segment{{Type: SegmentImage, Attachment: Attachment{Kind: AttachmentKindImage, URL: "https://ex/record.png"}}}},
	}}
	segs := []Segment{{Type: SegmentReply, Extra: map[string]any{
		SegmentExtraQuoteAtts:     []Attachment{{Kind: AttachmentKindImage, URL: "https://ex/typed.png"}},
		SegmentExtraQuotedForward: rec,
	}}}

	url, _ := QuotedImage(segs)
	assert.Equal(t, "https://ex/typed.png", url)
}
