package satori

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── EncodeOutboundMessage ────────────────────────────────────────────────────

func TestEncodeOutboundMessage_Text(t *testing.T) {
	msg := platform.TextMessage("hello world")
	got := EncodeOutboundMessage(msg)
	if got != "hello world" {
		t.Errorf("plain text: got %q, want %q", got, "hello world")
	}
}

func TestEncodeOutboundMessage_TextEscape(t *testing.T) {
	msg := platform.TextMessage("<script>alert('xss')</script>")
	got := EncodeOutboundMessage(msg)
	if strings.Contains(got, "<script>") {
		t.Errorf("text should be XML-escaped, got %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected &lt;script&gt; in output, got %q", got)
	}
}

func TestEncodeOutboundMessage_Markdown(t *testing.T) {
	msg := platform.MarkdownMessage("# hello")
	got := EncodeOutboundMessage(msg)
	// Markdown 作为纯文本传递，但 '#' 不需要转义
	if !strings.Contains(got, "# hello") {
		t.Errorf("markdown text: got %q", got)
	}
}

func TestEncodeOutboundMessage_WithReply(t *testing.T) {
	msg := platform.TextMessage("hi").WithReply("msg-001")
	got := EncodeOutboundMessage(msg)
	if !strings.HasPrefix(got, `<quote id="msg-001"/>`) {
		t.Errorf("reply should produce leading <quote> element, got %q", got)
	}
}

func TestEncodeOutboundMessage_WithMentions(t *testing.T) {
	msg := platform.TextMessage("hello").WithMentions("111", "222")
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `<at id="111"/>`) {
		t.Errorf("mention 111 not found in %q", got)
	}
	if !strings.Contains(got, `<at id="222"/>`) {
		t.Errorf("mention 222 not found in %q", got)
	}
}

func TestEncodeOutboundMessage_ImageAttachment(t *testing.T) {
	msg := platform.ImageMessage("https://example.com/img.png")
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `<img src="https://example.com/img.png"`) {
		t.Errorf("image attachment: got %q", got)
	}
}

func TestEncodeOutboundMessage_ImageWithTitle(t *testing.T) {
	msg := platform.OutboundMessage{
		Attachments: []platform.Attachment{
			{Kind: platform.AttachmentKindImage, URL: "https://example.com/img.png", Name: "cute cat"},
		},
	}
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `title="cute cat"`) {
		t.Errorf("image with title: got %q", got)
	}
}

func TestEncodeOutboundMessage_AudioAttachment(t *testing.T) {
	msg := platform.AudioMessage("https://example.com/audio.mp3")
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `<audio src="https://example.com/audio.mp3"/>`) {
		t.Errorf("audio attachment: got %q", got)
	}
}

func TestEncodeOutboundMessage_VideoAttachment(t *testing.T) {
	msg := platform.VideoMessage("https://example.com/video.mp4")
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `<video src="https://example.com/video.mp4"/>`) {
		t.Errorf("video attachment: got %q", got)
	}
}

func TestEncodeOutboundMessage_FileAttachment(t *testing.T) {
	msg := platform.FileMessage("https://example.com/doc.pdf", "doc.pdf")
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `<file src="https://example.com/doc.pdf"`) {
		t.Errorf("file attachment: got %q", got)
	}
	if !strings.Contains(got, `title="doc.pdf"`) {
		t.Errorf("file attachment title: got %q", got)
	}
}

func TestEncodeOutboundMessage_SkipsDataOnlyAttachment(t *testing.T) {
	msg := platform.OutboundMessage{
		Text: "look",
		Attachments: []platform.Attachment{
			{Kind: platform.AttachmentKindImage, Data: []byte{1, 2, 3}}, // no URL
		},
	}
	got := EncodeOutboundMessage(msg)
	if strings.Contains(got, "<img") {
		t.Errorf("data-only attachment should be skipped, got %q", got)
	}
}

func TestEncodeOutboundMessage_ButtonLink(t *testing.T) {
	msg := platform.OutboundMessage{
		Buttons: []platform.Button{
			{Label: "Docs", URL: "https://example.com", Style: platform.ButtonStyleLink},
		},
	}
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `type="link"`) {
		t.Errorf("link button type: got %q", got)
	}
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("link button href: got %q", got)
	}
}

func TestEncodeOutboundMessage_ButtonAction(t *testing.T) {
	msg := platform.OutboundMessage{
		Buttons: []platform.Button{
			{ID: "btn1", Label: "Click Me", Style: platform.ButtonStylePrimary},
		},
	}
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `type="action"`) {
		t.Errorf("action button type: got %q", got)
	}
	if !strings.Contains(got, `id="btn1"`) {
		t.Errorf("action button id: got %q", got)
	}
	if !strings.Contains(got, "Click Me") {
		t.Errorf("action button label: got %q", got)
	}
}

func TestEncodeOutboundMessage_ButtonActionNoID(t *testing.T) {
	msg := platform.OutboundMessage{
		Buttons: []platform.Button{
			{Label: "NoID", Style: platform.ButtonStyleSecondary},
		},
	}
	got := EncodeOutboundMessage(msg)
	if !strings.Contains(got, `type="action"`) {
		t.Errorf("no-ID action button: got %q", got)
	}
	if strings.Contains(got, `id=`) {
		t.Errorf("no-ID action button should not have id attr: got %q", got)
	}
}

func TestEncodeOutboundMessage_Empty(t *testing.T) {
	got := EncodeOutboundMessage(platform.OutboundMessage{})
	if got != "" {
		t.Errorf("empty message: got %q, want empty string", got)
	}
}

func TestEncodeOutboundMessage_ReplyAndMentionOrder(t *testing.T) {
	// quote must come first, then at, then text
	msg := platform.TextMessage("hi").WithReply("r1").WithMentions("u1")
	got := EncodeOutboundMessage(msg)
	quoteIdx := strings.Index(got, "<quote")
	atIdx := strings.Index(got, "<at")
	textIdx := strings.Index(got, "hi")
	if quoteIdx < 0 || atIdx < 0 || textIdx < 0 {
		t.Fatalf("missing elements in %q", got)
	}
	if quoteIdx >= atIdx || atIdx >= textIdx {
		t.Errorf("order wrong: quote=%d at=%d text=%d in %q", quoteIdx, atIdx, textIdx, got)
	}
}

// ─── ParseMessageContent ──────────────────────────────────────────────────────

func TestParseMessageContent_Empty(t *testing.T) {
	text, atts := ParseMessageContent("")
	if text != "" {
		t.Errorf("empty content: text=%q, want empty", text)
	}
	if len(atts) != 0 {
		t.Errorf("empty content: atts=%v, want none", atts)
	}
}

func TestParseMessageContent_PlainText(t *testing.T) {
	text, atts := ParseMessageContent("hello world")
	if text != "hello world" {
		t.Errorf("plain text: got %q", text)
	}
	if len(atts) != 0 {
		t.Errorf("plain text: unexpected attachments %v", atts)
	}
}

func TestParseMessageContent_HTMLEscape(t *testing.T) {
	text, _ := ParseMessageContent("&lt;script&gt;alert&amp;")
	if text != "<script>alert&" {
		t.Errorf("HTML unescape: got %q", text)
	}
}

func TestParseMessageContent_AtMention(t *testing.T) {
	text, _ := ParseMessageContent(`<at id="123456"/>`)
	if text != "@123456" {
		t.Errorf("at mention: got %q, want @123456", text)
	}
}

func TestParseMessageContent_AtMentionWithName(t *testing.T) {
	text, _ := ParseMessageContent(`<at id="123" name="Alice"/>`)
	if text != "@Alice" {
		t.Errorf("at mention with name: got %q, want @Alice", text)
	}
}

func TestParseMessageContent_AtAll(t *testing.T) {
	text, _ := ParseMessageContent(`<at type="all"/>`)
	if text != "@全体成员" {
		t.Errorf("at all: got %q", text)
	}
}

func TestParseMessageContent_AtHere(t *testing.T) {
	text, _ := ParseMessageContent(`<at type="here"/>`)
	if text != "@在线成员" {
		t.Errorf("at here: got %q", text)
	}
}

func TestParseMessageContent_Image(t *testing.T) {
	text, atts := ParseMessageContent(`<img src="https://example.com/img.png"/>`)
	if text != "[图片]" {
		t.Errorf("image text: got %q, want [图片]", text)
	}
	if len(atts) != 1 {
		t.Fatalf("image attachments: expected 1, got %d", len(atts))
	}
	if atts[0].URL != "https://example.com/img.png" {
		t.Errorf("image URL: got %q", atts[0].URL)
	}
	if atts[0].MimeType != "image/*" {
		t.Errorf("image mime: got %q", atts[0].MimeType)
	}
}

func TestParseMessageContent_ImageWithTitle(t *testing.T) {
	_, atts := ParseMessageContent(`<img src="https://example.com/img.png" title="cute cat"/>`)
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Name != "cute cat" {
		t.Errorf("image title: got %q", atts[0].Name)
	}
}

func TestParseMessageContent_Audio(t *testing.T) {
	text, atts := ParseMessageContent(`<audio src="https://example.com/a.mp3"/>`)
	if text != "[语音]" {
		t.Errorf("audio text: got %q", text)
	}
	if len(atts) != 1 || atts[0].URL != "https://example.com/a.mp3" {
		t.Errorf("audio attachment: got %+v", atts)
	}
}

func TestParseMessageContent_Video(t *testing.T) {
	text, atts := ParseMessageContent(`<video src="https://example.com/v.mp4"/>`)
	if text != "[视频]" {
		t.Errorf("video text: got %q", text)
	}
	if len(atts) != 1 || atts[0].URL != "https://example.com/v.mp4" {
		t.Errorf("video attachment: got %+v", atts)
	}
}

func TestParseMessageContent_File(t *testing.T) {
	text, atts := ParseMessageContent(`<file src="https://example.com/doc.pdf" title="doc.pdf"/>`)
	if text != "[文件]" {
		t.Errorf("file text: got %q", text)
	}
	if len(atts) != 1 || atts[0].URL != "https://example.com/doc.pdf" {
		t.Errorf("file attachment: got %+v", atts)
	}
	if atts[0].Name != "doc.pdf" {
		t.Errorf("file name: got %q", atts[0].Name)
	}
}

func TestParseMessageContent_Link(t *testing.T) {
	text, _ := ParseMessageContent(`<a href="https://example.com">Click here</a>`)
	if !strings.Contains(text, "Click here") {
		t.Errorf("link label: got %q", text)
	}
	if !strings.Contains(text, "https://example.com") {
		t.Errorf("link href: got %q", text)
	}
}

func TestParseMessageContent_LinkNoLabel(t *testing.T) {
	text, _ := ParseMessageContent(`<a href="https://example.com"/>`)
	if text != "https://example.com" {
		t.Errorf("link no label: got %q", text)
	}
}

func TestParseMessageContent_Br(t *testing.T) {
	text, _ := ParseMessageContent("line1<br/>line2")
	if !strings.Contains(text, "\n") {
		t.Errorf("br should produce newline: got %q", text)
	}
}

func TestParseMessageContent_Sharp(t *testing.T) {
	text, _ := ParseMessageContent(`<sharp id="channel-1" name="general"/>`)
	if text != "#general" {
		t.Errorf("sharp with name: got %q, want #general", text)
	}
}

func TestParseMessageContent_SharpNoName(t *testing.T) {
	text, _ := ParseMessageContent(`<sharp id="ch-99"/>`)
	if text != "#ch-99" {
		t.Errorf("sharp no name: got %q, want #ch-99", text)
	}
}

func TestParseMessageContent_Emoji(t *testing.T) {
	text, _ := ParseMessageContent(`<emoji name="smile"/>`)
	if text != ":smile:" {
		t.Errorf("emoji with name: got %q, want :smile:", text)
	}
}

func TestParseMessageContent_EmojiByID(t *testing.T) {
	text, _ := ParseMessageContent(`<emoji id="123"/>`)
	if text != "[emoji:123]" {
		t.Errorf("emoji by id: got %q, want [emoji:123]", text)
	}
}

func TestParseMessageContent_Button(t *testing.T) {
	text, _ := ParseMessageContent(`<button type="action" id="b1">OK</button>`)
	if text != "[OK]" {
		t.Errorf("button label: got %q, want [OK]", text)
	}
}

func TestParseMessageContent_QuoteSkipped(t *testing.T) {
	text, _ := ParseMessageContent(`<quote id="msg1">quoted text</quote>real text`)
	if strings.Contains(text, "quoted text") {
		t.Errorf("quote content should be skipped, got %q", text)
	}
	if !strings.Contains(text, "real text") {
		t.Errorf("real text after quote should be preserved: got %q", text)
	}
}

func TestParseMessageContent_DecorativeElements(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"<b>bold</b>", "bold"},
		{"<i>italic</i>", "italic"},
		{"<u>under</u>", "under"},
		{"<s>strike</s>", "strike"},
		{"<code>code</code>", "code"},
	}
	for _, tc := range cases {
		text, _ := ParseMessageContent(tc.tag)
		if text != tc.want {
			t.Errorf("decorative %s: got %q, want %q", tc.tag, text, tc.want)
		}
	}
}

func TestParseMessageContent_MixedContent(t *testing.T) {
	content := `hello <at id="user1"/> <img src="https://img.example.com/x.png"/> world`
	text, atts := ParseMessageContent(content)
	if !strings.Contains(text, "hello") {
		t.Errorf("mixed: missing 'hello' in %q", text)
	}
	if !strings.Contains(text, "@user1") {
		t.Errorf("mixed: missing '@user1' in %q", text)
	}
	if !strings.Contains(text, "[图片]") {
		t.Errorf("mixed: missing '[图片]' in %q", text)
	}
	if !strings.Contains(text, "world") {
		t.Errorf("mixed: missing 'world' in %q", text)
	}
	if len(atts) != 1 {
		t.Errorf("mixed: expected 1 attachment, got %d", len(atts))
	}
}

// ─── roundtrip: encode → parse ────────────────────────────────────────────────

func TestEncodeParseRoundtrip_TextAndMentions(t *testing.T) {
	original := platform.TextMessage("hello").WithMentions("userA")
	encoded := EncodeOutboundMessage(original)
	text, _ := ParseMessageContent(encoded)

	// Encoded form is "<at id="userA"/>hello" (no whitespace between mention and text).
	// The HTML parser will nest "hello" under <at>, so after the fix both are present.
	if !strings.Contains(text, "hello") {
		t.Errorf("roundtrip text: got %q, missing 'hello'", text)
	}
	if !strings.Contains(text, "@userA") {
		t.Errorf("roundtrip mention: got %q, missing '@userA'", text)
	}
}
