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
