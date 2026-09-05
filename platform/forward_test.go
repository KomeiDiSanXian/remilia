package platform

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForwardRecordText 覆盖统一渲染：标题头、编号、嵌套缩进、
// 媒体占位与类型标记。
func TestForwardRecordText(t *testing.T) {
	rec := &ForwardRecord{
		Title: "月莫法师和蕾米莉亚的聊天记录",
		Nodes: []ForwardNode{
			{Sender: UserInfo{DisplayName: "月莫法师"}, Segments: []Segment{{Type: SegmentText, Text: "/update now"}}},
			{Sender: UserInfo{DisplayName: "蕾米莉亚"}, Segments: []Segment{
				{Type: SegmentImage, Attachment: Attachment{URL: "https://ex/a.png"}},
				{Type: SegmentImage, Attachment: Attachment{URL: "https://ex/b.png"}},
			}},
			{
				Sender: UserInfo{DisplayName: "月莫法师"},
				Segments: []Segment{
					{Type: SegmentText, Text: "[内层记录]"},
					{Type: SegmentFile, Attachment: Attachment{Name: "doc.pdf"}},
				},
				Kind: ForwardKindRecord,
				Related: []ForwardNode{
					{Sender: UserInfo{DisplayName: "月莫法师"}, Segments: []Segment{{Type: SegmentText, Text: "/update now"}}},
					{Sender: UserInfo{DisplayName: "蕾米莉亚"}, Segments: []Segment{{Type: SegmentText, Text: "🔍 正在检查更新..."}}},
				},
			},
			{
				Sender:   UserInfo{DisplayName: "月莫法师"},
				Segments: []Segment{{Type: SegmentText, Text: "你能看到这是什么吗"}},
				Kind:     ForwardKindQuote,
				Related: []ForwardNode{
					{Sender: UserInfo{DisplayName: "蕾米莉亚"}, Segments: []Segment{{Type: SegmentText, Text: "[聊天记录]"}}},
				},
			},
		},
	}

	out := ForwardRecordText(rec)

	assert.Contains(t, out, "【合并转发聊天记录】月莫法师和蕾米莉亚的聊天记录（共 4 条消息）")
	assert.Contains(t, out, "1. 月莫法师: /update now")
	// 多图合并计数；URL 不进入渲染文本
	assert.Contains(t, out, "2. 蕾米莉亚: [图片x2]")
	assert.NotContains(t, out, "https://ex/a.png")
	// 嵌套记录：类型标记 + 缩进子列表 + 文件占位
	assert.Contains(t, out, "3. 月莫法师: [内层记录] [文件] [嵌套聊天记录]")
	assert.Contains(t, out, "\n  1. 月莫法师: /update now")
	assert.Contains(t, out, "\n  2. 蕾米莉亚: 🔍 正在检查更新...")
	// 嵌套引用
	assert.Contains(t, out, "4. 月莫法师: 你能看到这是什么吗 [引用消息]")
	assert.Contains(t, out, "\n  1. 蕾米莉亚: [聊天记录]")

	// 空记录
	assert.Equal(t, "", ForwardRecordText(nil))
	assert.Equal(t, "", ForwardRecordText(&ForwardRecord{}))
}

// TestForwardRecordTextTruncation 覆盖防膨胀上限：单条文本截断、
// 每层条数上限、嵌套层级上限。
func TestForwardRecordTextTruncation(t *testing.T) {
	// 单条文本 250 rune → 截断至 200 + 省略号
	long := strings.Repeat("字", forwardNodeTextMaxRunes+50)
	rec := &ForwardRecord{Nodes: []ForwardNode{
		{Segments: []Segment{{Type: SegmentText, Text: long}}},
	}}
	out := ForwardRecordText(rec)
	assert.Contains(t, out, strings.Repeat("字", forwardNodeTextMaxRunes)+"…")
	assert.NotContains(t, out, strings.Repeat("字", forwardNodeTextMaxRunes+10))

	// 60 条 → 渲染 50 条 + 省略行
	many := &ForwardRecord{Nodes: make([]ForwardNode, 60)}
	for i := range many.Nodes {
		many.Nodes[i].Segments = []Segment{{Type: SegmentText, Text: "msg"}}
	}
	out = ForwardRecordText(many)
	assert.Contains(t, out, "50. msg")
	assert.NotContains(t, out, "51. msg")
	assert.Contains(t, out, "…（其余 10 条省略）")

	// 嵌套 5 层 → 第 3 层后省略（自叶子向上构造，避免值拷贝丢失子树）
	inner := []ForwardNode{{Segments: []Segment{{Type: SegmentText, Text: "leaf"}}}}
	for range 4 {
		inner = []ForwardNode{{
			Kind:     ForwardKindRecord,
			Segments: []Segment{{Type: SegmentText, Text: "L"}},
			Related:  inner,
		}}
	}
	deep := &ForwardRecord{Nodes: []ForwardNode{{
		Kind:     ForwardKindRecord,
		Segments: []Segment{{Type: SegmentText, Text: "L1"}},
		Related:  inner,
	}}}
	out = ForwardRecordText(deep)
	assert.Contains(t, out, "…（嵌套层级过深省略）")
	require.Equal(t, 1, strings.Count(out, "L1"))
}
