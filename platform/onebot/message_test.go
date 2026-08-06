package onebot

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCQString_PlainText(t *testing.T) {
	result := parseCQString("hello world")
	require.Len(t, result, 1)
	assert.Equal(t, "text", result[0].Type)
	assert.Equal(t, "hello world", result[0].TextData())
}

func TestParseCQString_WithCQCode(t *testing.T) {
	result := parseCQString("hello [CQ:face,id=21]")
	require.Len(t, result, 2)
	assert.Equal(t, "text", result[0].Type)
	assert.Equal(t, "face", result[1].Type)
	assert.Equal(t, "21", result[1].Data["id"])
}

func TestParseCQString_OnlyCQCode(t *testing.T) {
	result := parseCQString("[CQ:at,qq=12345]")
	require.Len(t, result, 1)
	assert.Equal(t, "at", result[0].Type)
	assert.Equal(t, "12345", result[0].AtQQ())
}

func TestParseCQString_WithSpecialChars(t *testing.T) {
	result := parseCQString("&amp;#91;test&amp;#93;")
	require.Len(t, result, 1)
	assert.Equal(t, "&#91;test&#93;", result[0].TextData())
}

func TestParseCQString_PreservesAmpersandEncoding(t *testing.T) {
	result := parseCQString("&amp;amp;")
	require.Len(t, result, 1)
	assert.Equal(t, "&amp;", result[0].TextData())
}

func TestParseCQString_Empty(t *testing.T) {
	result := parseCQString("")
	assert.Empty(t, result)
}

func TestParseCQString_Malformed(t *testing.T) {
	result := parseCQString("[CQ:unclosed")
	require.Len(t, result, 1)
	assert.Equal(t, "text", result[0].Type)
}

func TestParseCQContent(t *testing.T) {
	seg := parseCQContent("image,file=https://example.com/img.jpg,type=flash")
	assert.Equal(t, "image", seg.Type)
	assert.Equal(t, "https://example.com/img.jpg", seg.Data["file"])
	assert.Equal(t, "flash", seg.Data["type"])
}

func TestParseCQContent_NoParams(t *testing.T) {
	seg := parseCQContent("shake")
	assert.Equal(t, "shake", seg.Type)
	assert.Empty(t, seg.Data)
}

func TestSplitCQParams(t *testing.T) {
	t.Run("simple split", func(t *testing.T) {
		parts := splitCQParams("a=1,b=2,c=3")
		assert.Equal(t, []string{"a=1", "b=2", "c=3"}, parts)
	})

	t.Run("escaped comma", func(t *testing.T) {
		parts := splitCQParams("a=1&#44;2,b=3")
		assert.Equal(t, []string{"a=1,2", "b=3"}, parts)
	})

	t.Run("empty string", func(t *testing.T) {
		parts := splitCQParams("")
		assert.Empty(t, parts)
	})
}

func TestEscapeCQValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"a&b", "a&amp;b"},
		{"a[b", "a&#91;b"},
		{"a]b", "a&#93;b"},
		{"a,b", "a&#44;b"},
		{"hello", "hello"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, escapeCQValue(tt.in), "escapeCQValue(%q)", tt.in)
	}
}

func TestEscapeText(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"a&b", "a&amp;b"},
		{"a[b", "a&#91;b"},
		{"a]b", "a&#93;b"},
		{"a,b", "a,b"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, escapeText(tt.in), "escapeText(%q)", tt.in)
	}
}

func TestToCQString(t *testing.T) {
	mc := MessageChain{
		textSegment("hello "),
		{Type: SegTypeAt, Data: map[string]string{"qq": "12345"}},
		{Type: SegTypeFace, Data: map[string]string{"id": "21"}},
	}
	result := mc.ToCQString()
	assert.Equal(t, "hello [CQ:at,qq=12345][CQ:face,id=21]", result)
}

func TestToCQString_Empty(t *testing.T) {
	mc := MessageChain{}
	assert.Empty(t, mc.ToCQString())
}

func TestToCQString_WithSpecialChars(t *testing.T) {
	mc := MessageChain{
		textSegment("a&b"),
	}
	result := mc.ToCQString()
	assert.Equal(t, "a&amp;b", result)
}

func TestOutboundToChain_Text(t *testing.T) {
	msg := platform.TextMessage("hello")
	chain := OutboundToChain(msg)
	require.Len(t, chain, 1)
	assert.Equal(t, "text", chain[0].Type)
	assert.Equal(t, "hello", chain[0].TextData())
}

func TestOutboundToChain_Markdown(t *testing.T) {
	msg := platform.MarkdownMessage("**bold**")
	chain := OutboundToChain(msg)
	require.Len(t, chain, 1)
	assert.Equal(t, "markdown", chain[0].Type)
	assert.Equal(t, "**bold**", chain[0].Data["content"])
}

func TestOutboundToChain_Reply(t *testing.T) {
	msg := platform.TextMessage("hello").WithReply("5000")
	chain := OutboundToChain(msg)
	require.Len(t, chain, 2)
	assert.Equal(t, "reply", chain[0].Type)
	assert.Equal(t, "5000", chain[0].ReplyID())
}

func TestOutboundToChain_Mentions(t *testing.T) {
	msg := platform.TextMessage("hello").WithMentions("12345")
	chain := OutboundToChain(msg)
	require.Len(t, chain, 2)
	assert.Equal(t, "at", chain[0].Type)
	assert.Equal(t, "12345", chain[0].AtQQ())
}

func TestOutboundToChain_Attachment(t *testing.T) {
	msg := platform.TextMessage("pic").WithAttachments(
		platform.Attachment{URL: "https://example.com/img.png", MimeType: "image/png", Name: "img.png"},
	)
	chain := OutboundToChain(msg)
	require.Len(t, chain, 2)
	assert.Equal(t, "image", chain[1].Type)
}

func TestOutboundToChain_Empty(t *testing.T) {
	msg := platform.TextMessage("")
	chain := OutboundToChain(msg)
	assert.Empty(t, chain)
}

// ── 出站段路径：Segments 优先、保序、交错 at 保真 ─────────────────────

func TestOutboundToChain_SegmentsPriority(t *testing.T) {
	// Segments 非空时忽略便捷字段
	msg := platform.TextMessage("flat")
	msg.Segments = []platform.Segment{{Type: platform.SegmentText, Text: "segmented"}}
	chain := OutboundToChain(msg)
	require.Len(t, chain, 1)
	assert.Equal(t, "segmented", chain[0].TextData())
}

func TestOutboundToChain_SegmentsInterleavedAt(t *testing.T) {
	// 分散 at 基准用例：文本夹 at 保序
	segs := []platform.Segment{
		{Type: platform.SegmentAt, UserID: "A"},
		{Type: platform.SegmentText, Text: "一段文本 "},
		{Type: platform.SegmentAt, UserID: "B"},
		{Type: platform.SegmentText, Text: "又是一段文本 "},
		{Type: platform.SegmentAt, UserID: "C"},
		{Type: platform.SegmentAt, UserID: "D"},
		{Type: platform.SegmentText, Text: "文本..."},
		{Type: platform.SegmentAt, UserID: "E"},
	}
	chain := OutboundChainFromSegments(segs)
	require.Len(t, chain, 8)
	assert.Equal(t, "at", chain[0].Type)
	assert.Equal(t, "A", chain[0].AtQQ())
	assert.Equal(t, "text", chain[1].Type)
	assert.Equal(t, "一段文本 ", chain[1].TextData())
	assert.Equal(t, "at", chain[2].Type)
	assert.Equal(t, "B", chain[2].AtQQ())
	assert.Equal(t, "at", chain[5].Type)
	assert.Equal(t, "D", chain[5].AtQQ())
	assert.Equal(t, "at", chain[7].Type)
	assert.Equal(t, "E", chain[7].AtQQ())
}

func TestOutboundChainFromSegments_FullMapping(t *testing.T) {
	segs := []platform.Segment{
		{Type: platform.SegmentReply, ReplyToID: "r1"},
		{Type: platform.SegmentMentionAll},
		{Type: platform.SegmentFace, FaceID: "21"},
		{Type: platform.SegmentImage, Attachment: platform.Attachment{URL: "u1", MimeType: "image/png"}},
		{Type: platform.SegmentAudio, Attachment: platform.Attachment{URL: "u2"}},
		{Type: platform.SegmentVideo, Attachment: platform.Attachment{URL: "u3"}},
		{Type: platform.SegmentFile, Attachment: platform.Attachment{URL: "u4", Name: "f.pdf"}},
		{Type: platform.SegmentForward, Extra: map[string]any{"forward_id": "fwd1"}},
		{Type: platform.SegmentButton},                                        // 无发送能力 → 跳过
		{Type: platform.SegmentUnknown, Extra: map[string]any{"type": "xml"}}, // 跳过
	}
	chain := OutboundChainFromSegments(segs)
	require.Len(t, chain, 8)
	assert.Equal(t, "reply", chain[0].Type)
	assert.Equal(t, SegTypeAt, chain[1].Type)
	assert.Equal(t, "all", chain[1].AtQQ())
	assert.Equal(t, "face", chain[2].Type)
	assert.Equal(t, "21", chain[2].Data["id"])
	assert.Equal(t, "image", chain[3].Type)
	assert.Equal(t, "record", chain[4].Type)
	assert.Equal(t, "video", chain[5].Type)
	assert.Equal(t, "file", chain[6].Type)
	assert.Equal(t, "f.pdf", chain[6].Data["name"])
	assert.Equal(t, "forward", chain[7].Type)
	assert.Equal(t, "fwd1", chain[7].Data["id"])
}

func TestOutboundChainFromSegments_ForwardNoID(t *testing.T) {
	// 无 forward_id 的 forward 段（跨平台摘要已降级为 text，此路径为同平台异常）→ 跳过
	chain := OutboundChainFromSegments([]platform.Segment{{Type: platform.SegmentForward}})
	assert.Empty(t, chain)
}

// ── 跨平台转发往返（跨平台转发）：入站段 → MessageToOutbound → 出站段 ────────────────

func TestMessageToOutbound_RoundTripCrossPlatform(t *testing.T) {
	// 入站：onebot CQ 链（分散 at 基准用例）
	chain := MessageChain{
		{Type: SegTypeText, Data: map[string]string{"text": "@A用户 一段文本 "}},
		{Type: SegTypeAt, Data: map[string]string{"qq": "B"}},
		{Type: SegTypeText, Data: map[string]string{"text": " 又是一段文本 "}},
		{Type: SegTypeAt, Data: map[string]string{"qq": "C"}},
		{Type: SegTypeAt, Data: map[string]string{"qq": "D"}},
		{Type: SegTypeText, Data: map[string]string{"text": " 文本..."}},
		{Type: SegTypeAt, Data: map[string]string{"qq": "E"}},
		{Type: SegTypeFace, Data: map[string]string{"id": "21"}},
		{Type: SegTypeReply, Data: map[string]string{"id": "5000"}},
	}
	inSegs := chain.Segments()

	// 跨平台转发（缺省保守）：face → text，reply 剥离
	m := platform.Message{Platform: PlatformID, Segments: inSegs}
	out := platform.MessageToOutbound(m)
	require.Len(t, out.Segments, 8)

	// 出站：目标平台（discord 风格）内联渲染
	content := ""
	for _, s := range out.Segments {
		switch s.Type {
		case platform.SegmentText:
			content += s.Text
		case platform.SegmentAt:
			content += "<@" + s.UserID + ">"
		}
	}
	want := "@A用户 一段文本 <@B> 又是一段文本 <@C><@D> 文本...<@E>21"
	assert.Equal(t, want, content)
	assert.Empty(t, out.ReplyToID, "跨平台 reply 段应剥离")

	// 同平台转发（WithTargetPlatform）：全量透传
	same := platform.MessageToOutbound(m, platform.WithTargetPlatform(PlatformID))
	require.Len(t, same.Segments, len(inSegs))
	for i := range inSegs {
		assert.Equal(t, inSegs[i], same.Segments[i], "同平台第 %d 段应保真", i)
	}
	assert.Equal(t, "5000", same.ReplyToID)
}

func TestAttachmentToSegment_Image(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/img.jpg", MimeType: "image/jpeg"})
	assert.Equal(t, "image", seg.Type)
}

func TestAttachmentToSegment_Audio(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/audio.mp3", MimeType: "audio/mpeg"})
	assert.Equal(t, "record", seg.Type)
}

func TestAttachmentToSegment_Video(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/vid.mp4", MimeType: "video/mp4"})
	assert.Equal(t, "video", seg.Type)
}

func TestAttachmentToSegment_File(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/doc.pdf", MimeType: "application/pdf", Name: "doc.pdf"})
	assert.Equal(t, "file", seg.Type)
}

func TestAttachmentToSegment_UnknownMIME(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/file.xyz", MimeType: "application/octet-stream"})
	assert.Equal(t, "file", seg.Type)
}

func TestAttachmentToSegment_EmptyMIME(t *testing.T) {
	seg := attachmentToSegment(platform.Attachment{URL: "https://ex.com/img.jpg", MimeType: ""})
	assert.Equal(t, "image", seg.Type)
}

func TestFullText(t *testing.T) {
	mc := MessageChain{
		textSegment("hello "),
		{Type: SegTypeAt, Data: map[string]string{"qq": "123"}},
		textSegment(" world"),
	}
	assert.Equal(t, "hello @123 world", mc.FullText())
}

func TestText(t *testing.T) {
	mc := MessageChain{
		textSegment("hello"),
		{Type: SegTypeAt, Data: map[string]string{"qq": "123"}},
		textSegment(" world"),
	}
	assert.Equal(t, "hello world", mc.Text())
}

func TestGetAtList(t *testing.T) {
	mc := MessageChain{
		{Type: SegTypeAt, Data: map[string]string{"qq": "123"}},
		{Type: SegTypeAt, Data: map[string]string{"qq": "456"}},
		textSegment("text"),
	}
	assert.Equal(t, []string{"123", "456"}, mc.GetAtList())
}

func TestGetAtList_Empty(t *testing.T) {
	mc := MessageChain{textSegment("no at")}
	assert.Empty(t, mc.GetAtList())
}

func TestGetReplyToID(t *testing.T) {
	mc := MessageChain{
		{Type: SegTypeReply, Data: map[string]string{"id": "999"}},
		textSegment("reply"),
	}
	assert.Equal(t, "999", mc.GetReplyToID())
}

func TestGetReplyToID_NoReply(t *testing.T) {
	mc := MessageChain{textSegment("no reply")}
	assert.Empty(t, mc.GetReplyToID())
}

func TestMessageChain_UnmarshalJSON_Null(t *testing.T) {
	var mc MessageChain
	err := json.Unmarshal([]byte(`null`), &mc)
	require.NoError(t, err)
	assert.Empty(t, mc)
}

func TestMessageChain_MarshalJSON(t *testing.T) {
	mc := MessageChain{textSegment("hello"), {Type: SegTypeAt, Data: map[string]string{"qq": "123"}}}
	data, err := json.Marshal(mc)
	require.NoError(t, err)
	assert.Contains(t, string(data), "text")
	assert.Contains(t, string(data), "at")
}

func TestMessageChain_MarshalJSON_Empty(t *testing.T) {
	mc := MessageChain{}
	data, err := json.Marshal(mc)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))
}

func TestMessageSegment_MarshalJSON_RawData(t *testing.T) {
	node, err := NewNodeSegment("10001", "Alice", MessageChain{textSegment("hello")})
	require.NoError(t, err)
	data, err := json.Marshal(node)
	require.NoError(t, err)
	assert.Contains(t, string(data), "node")
	assert.Contains(t, string(data), "10001")
}

func TestMessageSegment_TextData(t *testing.T) {
	s := MessageSegment{Type: SegTypeText, Data: map[string]string{"text": "hello"}}
	assert.Equal(t, "hello", s.TextData())
	assert.Empty(t, MessageSegment{Type: SegTypeText}.TextData())
}

func TestMessageSegment_ImageURL(t *testing.T) {
	s := MessageSegment{Type: SegTypeImage, Data: map[string]string{"url": "https://example.com/img.jpg"}}
	assert.Equal(t, "https://example.com/img.jpg", s.ImageURL())

	s2 := MessageSegment{Type: SegTypeImage, Data: map[string]string{"file": "local.jpg"}}
	assert.Equal(t, "local.jpg", s2.ImageURL())
}

func TestMessageSegment_AtQQ(t *testing.T) {
	s := MessageSegment{Type: SegTypeAt, Data: map[string]string{"qq": "12345"}}
	assert.Equal(t, "12345", s.AtQQ())
	assert.Equal(t, "", MessageSegment{Type: SegTypeAt}.AtQQ())
}

func TestMessageSegment_ReplyID(t *testing.T) {
	s := MessageSegment{Type: SegTypeReply, Data: map[string]string{"id": "999"}}
	assert.Equal(t, "999", s.ReplyID())
}

func TestSegmentData_UnmarshalJSON(t *testing.T) {
	t.Run("null", func(t *testing.T) {
		var d SegmentData
		err := json.Unmarshal([]byte(`null`), &d)
		require.NoError(t, err)
		assert.Nil(t, d)
	})

	t.Run("empty", func(t *testing.T) {
		var d SegmentData
		err := json.Unmarshal([]byte(`{}`), &d)
		require.NoError(t, err)
		assert.Empty(t, d)
	})

	t.Run("non-object", func(t *testing.T) {
		var d SegmentData
		err := json.Unmarshal([]byte(`""`), &d)
		require.NoError(t, err)
		assert.Nil(t, d)
	})
}

func TestToAttachments(t *testing.T) {
	mc := MessageChain{
		{Type: SegTypeImage, Data: map[string]string{"url": "https://ex.com/img.jpg", "file": "img.jpg"}},
		{Type: SegTypeRecord, Data: map[string]string{"url": "https://ex.com/audio.mp3"}},
		{Type: SegTypeVideo, Data: map[string]string{"url": "https://ex.com/vid.mp4"}},
		{Type: SegTypeFile, Data: map[string]string{"url": "https://ex.com/doc.pdf", "name": "doc.pdf"}},
		{Type: SegTypeMface, Data: map[string]string{"url": "https://ex.com/face.png", "summary": "face"}},
	}
	atts := mc.ToAttachments()
	require.Len(t, atts, 5)
	assert.Equal(t, "https://ex.com/img.jpg", atts[0].URL)
	assert.Equal(t, "https://ex.com/audio.mp3", atts[1].URL)
	assert.Equal(t, "https://ex.com/vid.mp4", atts[2].URL)
	assert.Equal(t, "https://ex.com/doc.pdf", atts[3].URL)
	assert.Equal(t, "https://ex.com/face.png", atts[4].URL)
}

func TestToAttachments_Empty(t *testing.T) {
	mc := MessageChain{textSegment("text")}
	atts := mc.ToAttachments()
	assert.Empty(t, atts)
}

func TestRawJSONToString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"hello"`, "hello"},
		{`123`, "123"},
		{`true`, "true"},
		{`{"a":1}`, `{"a":1}`},
		{`[1,2,3]`, `[1,2,3]`},
		{`null`, ""},
		{`""`, ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, rawJSONToString(json.RawMessage(tt.input)))
	}
}
