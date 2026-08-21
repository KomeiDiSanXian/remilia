package qq

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// TestQuoteAttachmentsFromElementsFirstElementOnly 103 引用消息只收集
// msg_elements[0]（被引用消息本体）的附件，后续元素的附件不混入。
func TestQuoteAttachmentsFromElementsFirstElementOnly(t *testing.T) {
	raw := `[
		{"message_type":7,"content":"[图片]",
		 "attachments":[{"url":"https://ex.com/a.png","content_type":"image/png"}]},
		{"message_type":7,"content":"[图片]",
		 "attachments":[{"url":"https://ex.com/b.png","content_type":"image/png"}]}
	]`
	atts := quoteAttachmentsFromElements(gjson.Parse(raw))
	if len(atts) != 1 {
		t.Fatalf("expected only first element attachments, got %d: %+v", len(atts), atts)
	}
	if atts[0].URL != "https://ex.com/a.png" {
		t.Errorf("unexpected attachment: %+v", atts[0])
	}
	if atts[0].Kind != platform.AttachmentKindImage {
		t.Errorf("expected Kind=image after parseAttachments, got %q", atts[0].Kind)
	}
}

// TestQuoteAttachmentsFromElementsEmpty 空数组/非数组返回 nil。
func TestQuoteAttachmentsFromElementsEmpty(t *testing.T) {
	if atts := quoteAttachmentsFromElements(gjson.Parse(`[]`)); atts != nil {
		t.Errorf("expected nil for empty array, got %+v", atts)
	}
	if atts := quoteAttachmentsFromElements(gjson.Parse(`{}`)); atts != nil {
		t.Errorf("expected nil for non-array, got %+v", atts)
	}
}
