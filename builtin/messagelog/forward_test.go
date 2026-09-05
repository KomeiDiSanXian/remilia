package messagelog

import (
	"strings"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forwardSegmentEvent 包装合成事件并注入 SegmentForward 段
// （SyntheticEvent 无段注入接口），模拟 QQ message_type=102 直发合并转发：
// 平台派生 Content 为空，正文在结构化记录载荷中。
type forwardSegmentEvent struct {
	platform.Event
	segs []platform.Segment
}

func (e *forwardSegmentEvent) Segments() []platform.Segment { return e.segs }

// TestEventToEntryForwardRecord 直发合并转发消息入记录时渲染结构化记录
// 文本为 Content（消息窗口 / 回复上下文 / 统计等文本型下游可感知）。
func TestEventToEntryForwardRecord(t *testing.T) {
	rec := &platform.ForwardRecord{
		Title: "月莫法师和蕾米莉亚的聊天记录",
		Nodes: []platform.ForwardNode{
			{Sender: platform.UserInfo{DisplayName: "月莫法师"},
				Segments: []platform.Segment{{Type: platform.SegmentText, Text: "/update now"}}},
		},
	}
	evt := &forwardSegmentEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindPrivateMessage, ""),
		segs: []platform.Segment{{
			Type:  platform.SegmentForward,
			Extra: map[string]any{platform.SegmentExtraForwardNodes: rec},
		}},
	}

	entry := eventToEntry(evt, eventctx.NewContextFromEvent(evt, nil))

	require.Contains(t, entry.Content, "【合并转发聊天记录】月莫法师和蕾米莉亚的聊天记录")
	assert.Contains(t, entry.Content, "1. 月莫法师: /update now")
}

// TestEventToEntryContentPriority 正文非空时不渲染记录（普通消息行为不变）。
func TestEventToEntryContentPriority(t *testing.T) {
	evt := &forwardSegmentEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "普通正文"),
		segs: []platform.Segment{
			{Type: platform.SegmentText, Text: "普通正文"},
			{Type: platform.SegmentForward, Extra: map[string]any{
				platform.SegmentExtraForwardNodes: &platform.ForwardRecord{Title: "T"},
			}},
		},
	}

	entry := eventToEntry(evt, eventctx.NewContextFromEvent(evt, nil))
	assert.Equal(t, "普通正文", entry.Content)
	assert.False(t, strings.Contains(entry.Content, "【合并转发聊天记录】"))
}
