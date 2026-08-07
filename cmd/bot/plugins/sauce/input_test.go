package sauce

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

func TestQuotedImageFromRawQuote(t *testing.T) {
	// QQ 引用消息：msg_elements[0].attachments[].url 为被引用图片
	raw := `[
		{"msg_idx":"REFIDX_1","message_type":103,"content":" ",
		 "attachments":[
			{"url":"https://img.example.com/photo1.png","content_type":"image/png"}
		 ]}
	]`
	assert.Equal(t, "https://img.example.com/photo1.png", quotedImageFromRawQuote(raw))
}

func TestQuotedImageFromRawQuoteNoImageType(t *testing.T) {
	// 附件非图片类型时跳过，取 image/* 附件
	raw := `[
		{"msg_idx":"REFIDX_1",
		 "attachments":[
			{"url":"https://img.example.com/a.mp3","content_type":"audio/mp3"},
			{"url":"https://img.example.com/b.png","content_type":"image/png"}
		 ]}
	]`
	assert.Equal(t, "https://img.example.com/b.png", quotedImageFromRawQuote(raw))
}

func TestQuotedImageFromRawQuoteNoContentType(t *testing.T) {
	// 无 content_type 时取第一个带 url 的附件
	raw := `[
		{"msg_idx":"REFIDX_1",
		 "attachments":[
			{"url":"https://img.example.com/only.png"}
		 ]}
	]`
	assert.Equal(t, "https://img.example.com/only.png", quotedImageFromRawQuote(raw))
}

func TestQuotedImageFromRawQuoteEmpty(t *testing.T) {
	assert.Equal(t, "", quotedImageFromRawQuote(""))
	assert.Equal(t, "", quotedImageFromRawQuote("not-json"))
	assert.Equal(t, "", quotedImageFromRawQuote(`[]`))
	assert.Equal(t, "", quotedImageFromRawQuote(`[{"attachments":[]}]`))
	assert.Equal(t, "", quotedImageFromRawQuote(`[{"attachments":[{"url":""}]}]`))
}

func TestQuotedImageFromParallel(t *testing.T) {
	// QQ parallel_message：msg_nodes 内的富媒体节点携带 attachments
	raw := `{"msg_nodes":[
		{"message_type":7,"content":"[图片] ",
		 "attachments":[{"url":"https://img.example.com/n1.png","content_type":"image/png"}]}
	]}`
	assert.Equal(t, "https://img.example.com/n1.png", quotedImageFromParallel(raw))
}

func TestQuotedImageFromParallelEmpty(t *testing.T) {
	assert.Equal(t, "", quotedImageFromParallel(""))
	assert.Equal(t, "", quotedImageFromParallel(`{"msg_nodes":[]}`))
	assert.Equal(t, "", quotedImageFromParallel(`{"msg_nodes":[{"attachments":[]}]}`))
}

func TestFindQuotedImageURL(t *testing.T) {
	evt := &replyQuoteEvent{
		segments: []platform.Segment{
			{
				Type: platform.SegmentReply,
				Extra: map[string]any{
					"raw_quote": `[{"msg_idx":"R1","attachments":[{"url":"https://img.example.com/q.png","content_type":"image/png"}]}]`,
				},
			},
			{Type: platform.SegmentText, Text: "hello"},
		},
	}
	assert.Equal(t, "https://img.example.com/q.png", findQuotedImageURL(evt))

	// 无 reply 段 → 空
	assert.Equal(t, "", findQuotedImageURL(&replyQuoteEvent{
		segments: []platform.Segment{{Type: platform.SegmentText, Text: "x"}},
	}))
}

// replyQuoteEvent 实现 platform.Event 的最小测试事件。
type replyQuoteEvent struct {
	segments []platform.Segment
}

func (e *replyQuoteEvent) Platform() string             { return "test" }
func (e *replyQuoteEvent) Kind() platform.EventKind     { return platform.EventKindPrivateMessage }
func (e *replyQuoteEvent) ID() string                   { return "id-1" }
func (e *replyQuoteEvent) Segments() []platform.Segment { return e.segments }
func (e *replyQuoteEvent) Sender() platform.UserInfo    { return platform.UserInfo{ID: "u1"} }
func (e *replyQuoteEvent) Chat() platform.ChatInfo      { return platform.ChatInfo{ID: "c1"} }
func (e *replyQuoteEvent) Timestamp() time.Time         { return time.Time{} }
