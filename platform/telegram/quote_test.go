package telegram

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestBuildTelegramSegmentsQuoteAttachments 引用消息：update 内嵌的
// reply_to_message 媒体以 file_id 归一化进 reply 段
// Extra[SegmentExtraQuoteAtts]（URL 由适配器解析步骤统一换取）。
func TestBuildTelegramSegmentsQuoteAttachments(t *testing.T) {
	msg := &Message{
		MessageID: 10,
		Date:      1,
		From:      &User{ID: 1},
		Chat:      &Chat{ID: 1, Type: ChatTypePrivate},
		Text:      "这是什么",
		ReplyToMsg: &Message{
			MessageID: 9,
			Photo:     []PhotoSize{{FileID: "q1", Width: 100, Height: 100}},
		},
	}
	segs := buildTelegramSegments(msg)
	if len(segs) == 0 || segs[0].Type != platform.SegmentReply {
		t.Fatalf("expected reply segment first, got %+v", segs)
	}
	atts, ok := segs[0].Extra[platform.SegmentExtraQuoteAtts].([]platform.Attachment)
	if !ok || len(atts) != 1 {
		t.Fatalf("expected quote attachments in reply Extra, got %+v", segs[0].Extra)
	}
	meta, ok := atts[0].Extra[ExtraKeyFile].(*FileMeta)
	if !ok || meta.FileID != "q1" {
		t.Errorf("expected file_id q1 in quote attachment meta, got %+v", atts[0])
	}
	if atts[0].URL != "" {
		t.Errorf("quote attachment URL should be resolved by adapter, got %q", atts[0].URL)
	}
}

// TestBuildTelegramSegmentsNoQuotedMedia 无媒体的引用消息不应携带引用附件键。
func TestBuildTelegramSegmentsNoQuotedMedia(t *testing.T) {
	msg := &Message{
		MessageID: 11,
		Date:      1,
		From:      &User{ID: 1},
		Chat:      &Chat{ID: 1, Type: ChatTypePrivate},
		Text:      "hi",
		ReplyToMsg: &Message{
			MessageID: 9,
			Text:      "纯文本被引用消息",
		},
	}
	segs := buildTelegramSegments(msg)
	if len(segs) == 0 || segs[0].Type != platform.SegmentReply {
		t.Fatalf("expected reply segment first, got %+v", segs)
	}
	if _, ok := segs[0].Extra[platform.SegmentExtraQuoteAtts]; ok {
		t.Error("expected no quote attachments for text-only quoted message")
	}
}

// TestResolveAttachmentURLsPropagatesToEvent 回归测试：URL 解析必须写进事件
// 内部段（此前遍历 platform.Attachments 的值拷贝改写，分发前全部丢失——
// 插件层拿到的 URL 恒为空），且解析范围覆盖引用段附件。
func TestResolveAttachmentURLsPropagatesToEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"x","file_size":123,"file_path":"photos/x.jpg"}}`))
	}))
	defer srv.Close()

	cl := NewClient("TOK")
	cl.baseURL = srv.URL + "/botTOK"
	a := &PollingAdapter{client: cl}

	msg := &Message{
		MessageID: 10,
		Date:      1,
		From:      &User{ID: 1},
		Chat:      &Chat{ID: 1, Type: ChatTypePrivate},
		Photo:     []PhotoSize{{FileID: "own"}},
		ReplyToMsg: &Message{
			MessageID: 9,
			Photo:     []PhotoSize{{FileID: "quoted"}},
		},
	}
	e := newMessageEventWithBot(msg, false, "bot")

	a.resolveAttachmentURLs(stdctx.Background(), e, 5*time.Second)

	var ownURL, quoteURL string
	for _, s := range e.Segments() {
		switch s.Type {
		case platform.SegmentImage:
			if ownURL == "" {
				ownURL = s.Attachment.URL
			}
		case platform.SegmentReply:
			if atts, ok := s.Extra[platform.SegmentExtraQuoteAtts].([]platform.Attachment); ok && len(atts) > 0 {
				quoteURL = atts[0].URL
			}
		}
	}
	if ownURL == "" {
		t.Error("own attachment URL not propagated into event segments (mutation lost on copy)")
	}
	if quoteURL == "" {
		t.Error("quoted attachment URL not propagated into reply segment Extra")
	}
}

// TestResolveAttachmentURLsDedupesFileID 本条消息与引用段为同一文件时只
// getFile 一次，URL 复用到所有同 FileID 附件。
func TestResolveAttachmentURLsDedupesFileID(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"same","file_size":123,"file_path":"photos/x.jpg"}}`))
	}))
	defer srv.Close()

	cl := NewClient("TOK")
	cl.baseURL = srv.URL + "/botTOK"
	a := &PollingAdapter{client: cl}

	msg := &Message{
		MessageID: 10,
		Date:      1,
		From:      &User{ID: 1},
		Chat:      &Chat{ID: 1, Type: ChatTypePrivate},
		Photo:     []PhotoSize{{FileID: "same"}},
		ReplyToMsg: &Message{
			MessageID: 9,
			Photo:     []PhotoSize{{FileID: "same"}},
		},
	}
	e := newMessageEventWithBot(msg, false, "bot")

	a.resolveAttachmentURLs(stdctx.Background(), e, 5*time.Second)

	if calls != 1 {
		t.Errorf("expected exactly one getFile for duplicate FileID, got %d", calls)
	}
	segs := e.Segments()
	var ownURL, quoteURL string
	for _, s := range segs {
		switch s.Type {
		case platform.SegmentImage:
			if ownURL == "" {
				ownURL = s.Attachment.URL
			}
		case platform.SegmentReply:
			if atts, ok := s.Extra[platform.SegmentExtraQuoteAtts].([]platform.Attachment); ok && len(atts) > 0 {
				quoteURL = atts[0].URL
			}
		}
	}
	if ownURL == "" || quoteURL == "" || ownURL != quoteURL {
		t.Errorf("expected both attachments resolved to same URL, own=%q quote=%q", ownURL, quoteURL)
	}
}
