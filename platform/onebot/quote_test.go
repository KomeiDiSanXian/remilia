package onebot

import (
	stdctx "context"
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// fakeQuoteAPI 可编程的 APIClient 假实现，记录调用次数。
type fakeQuoteAPI struct {
	resp  *APIResponse
	err   error
	calls int
}

func (f *fakeQuoteAPI) Call(_ stdctx.Context, _ string, _ any) (*APIResponse, error) {
	f.calls++
	return f.resp, f.err
}

func quotedReplyEvent(t *testing.T) platform.Event {
	t.Helper()
	raw := `{"post_type":"message","message_type":"group","group_id":777,"user_id":111,` +
		`"message_id":200,"time":1,"self_id":9,"message":[` +
		`{"type":"reply","data":{"id":"42"}},{"type":"text","data":{"text":"这是什么"}}]}`
	ev, err := parseEvent([]byte(raw))
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	return ev
}

// TestEnrichQuotedAttachments 引用消息经 get_msg 回查后，被引用图片附件
// 归一化进 reply 段 Extra[SegmentExtraQuoteAtts]。
func TestEnrichQuotedAttachments(t *testing.T) {
	getMsg := GetMsgResult{
		MessageID: 42,
		Message: MessageChain{
			{Type: SegTypeImage, Data: map[string]string{"url": "https://multimedia.example/a.jpg", "file": "a.jpg"}},
			{Type: SegTypeText, Data: map[string]string{"text": "[图片]"}},
		},
	}
	data, err := json.Marshal(getMsg)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeQuoteAPI{resp: &APIResponse{Status: "ok", Retcode: 0, Data: data}}

	ev := quotedReplyEvent(t)
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)

	if api.calls != 1 {
		t.Fatalf("expected exactly one get_msg call, got %d", api.calls)
	}
	for _, s := range ev.Segments() {
		if s.Type != platform.SegmentReply {
			continue
		}
		atts, ok := s.Extra[platform.SegmentExtraQuoteAtts].([]platform.Attachment)
		if !ok || len(atts) != 1 {
			t.Fatalf("expected enriched quote attachment, got %+v", s.Extra)
		}
		if atts[0].URL != "https://multimedia.example/a.jpg" || atts[0].Kind != platform.AttachmentKindImage {
			t.Errorf("unexpected quote attachment: %+v", atts[0])
		}
		return
	}
	t.Fatal("no reply segment found")
}

// TestEnrichQuotedAttachmentsNoReply 无引用段的事件不应触发回查。
func TestEnrichQuotedAttachmentsNoReply(t *testing.T) {
	api := &fakeQuoteAPI{}
	raw := `{"post_type":"message","message_type":"private","user_id":1,"message_id":2,` +
		`"time":1,"self_id":9,"message":[{"type":"text","data":{"text":"hi"}}]}`
	ev, err := parseEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)
	if api.calls != 0 {
		t.Errorf("expected no API call without reply segment, got %d", api.calls)
	}
}

// TestEnrichQuotedAttachmentsAlreadyFilled 引用段已携带引用数据（如实现方内嵌）
// 时不应重复回查。
func TestEnrichQuotedAttachmentsAlreadyFilled(t *testing.T) {
	api := &fakeQuoteAPI{resp: &APIResponse{Status: "ok"}}
	ev := quotedReplyEvent(t)
	segs := ev.Segments()
	for i := range segs {
		if segs[i].Type == platform.SegmentReply {
			segs[i].Extra = map[string]any{platform.SegmentExtraQuoteAtts: []platform.Attachment{{URL: "https://x/a.png"}}}
		}
	}
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)
	if api.calls != 0 {
		t.Errorf("expected no API call when quote attachments present, got %d", api.calls)
	}
}

// TestEnrichQuotedAttachmentsAPIFailure 回查失败时静默跳过，不填充、不 panic。
func TestEnrichQuotedAttachmentsAPIFailure(t *testing.T) {
	api := &fakeQuoteAPI{err: stdctx.DeadlineExceeded}
	ev := quotedReplyEvent(t)
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)
	for _, s := range ev.Segments() {
		if s.Type == platform.SegmentReply {
			if _, ok := s.Extra[platform.SegmentExtraQuoteAtts]; ok {
				t.Error("expected no quote attachments on API failure")
			}
		}
	}
}

// TestEnrichQuotedAttachmentsFillsFirstReplyOnly 多 reply 段时只回查并填充首个
// reply 段（与 GetReplyToID 语义一致）。
func TestEnrichQuotedAttachmentsFillsFirstReplyOnly(t *testing.T) {
	getMsg := GetMsgResult{
		MessageID: 42,
		Message: MessageChain{
			{Type: SegTypeImage, Data: map[string]string{"url": "https://multimedia.example/a.jpg"}},
		},
	}
	data, err := json.Marshal(getMsg)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeQuoteAPI{resp: &APIResponse{Status: "ok", Retcode: 0, Data: data}}

	raw := `{"post_type":"message","message_type":"group","group_id":777,"user_id":111,` +
		`"message_id":200,"time":1,"self_id":9,"message":[` +
		`{"type":"reply","data":{"id":"42"}},{"type":"reply","data":{"id":"43"}},{"type":"text","data":{"text":"hi"}}]}`
	ev, err := parseEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)

	if api.calls != 1 {
		t.Fatalf("expected one get_msg call, got %d", api.calls)
	}
	var replyCount, filled int
	for _, s := range ev.Segments() {
		if s.Type != platform.SegmentReply {
			continue
		}
		replyCount++
		if _, ok := s.Extra[platform.SegmentExtraQuoteAtts]; ok {
			filled++
		}
	}
	if replyCount != 2 || filled != 1 {
		t.Errorf("expected exactly 1 of 2 reply segments filled, got reply=%d filled=%d", replyCount, filled)
	}
}

// TestEnrichQuotedAttachmentsSecondReplyUnfilledStillFetches 首个 reply 段未填充、
// 后续 reply 段已填充时仍应回查（锚定首个 reply 段）。
func TestEnrichQuotedAttachmentsSecondReplyUnfilledStillFetches(t *testing.T) {
	getMsg := GetMsgResult{
		MessageID: 42,
		Message: MessageChain{
			{Type: SegTypeImage, Data: map[string]string{"url": "https://multimedia.example/a.jpg"}},
		},
	}
	data, err := json.Marshal(getMsg)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeQuoteAPI{resp: &APIResponse{Status: "ok", Retcode: 0, Data: data}}

	raw := `{"post_type":"message","message_type":"group","group_id":777,"user_id":111,` +
		`"message_id":200,"time":1,"self_id":9,"message":[` +
		`{"type":"reply","data":{"id":"42"}},{"type":"reply","data":{"id":"43"}},{"type":"text","data":{"text":"hi"}}]}`
	ev, err := parseEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	segs := ev.Segments()
	segs[1].Extra = map[string]any{platform.SegmentExtraQuoteAtts: []platform.Attachment{{URL: "https://x/b.png"}}}

	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)

	if api.calls != 1 {
		t.Fatalf("expected one get_msg call, got %d", api.calls)
	}
	if _, ok := segs[0].Extra[platform.SegmentExtraQuoteAtts]; !ok {
		t.Error("expected first reply segment filled")
	}
}

// TestEnrichQuotedAttachmentsNoURLImage get_msg 返回的图片无 URL 时不填充。
func TestEnrichQuotedAttachmentsNoURLImage(t *testing.T) {
	getMsg := GetMsgResult{
		MessageID: 42,
		Message: MessageChain{
			{Type: SegTypeImage, Data: map[string]string{}},
			{Type: SegTypeText, Data: map[string]string{"text": "[图片]"}},
		},
	}
	data, err := json.Marshal(getMsg)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeQuoteAPI{resp: &APIResponse{Status: "ok", Retcode: 0, Data: data}}

	ev := quotedReplyEvent(t)
	enrichQuotedAttachments(stdctx.Background(), newSender(api), ev)

	for _, s := range ev.Segments() {
		if s.Type != platform.SegmentReply {
			continue
		}
		if _, ok := s.Extra[platform.SegmentExtraQuoteAtts]; ok {
			t.Error("expected no quote attachments when image has no URL")
		}
	}
}
