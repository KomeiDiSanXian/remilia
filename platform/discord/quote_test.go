package discord

import (
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// TestBuildDiscordSegmentsQuoteAttachments reply payload 内嵌
// referenced_message 时，其附件归一化进 reply 段 Extra[SegmentExtraQuoteAtts]。
func TestBuildDiscordSegmentsQuoteAttachments(t *testing.T) {
	m := &discordgo.Message{
		ID:        "m2",
		ChannelID: "c1",
		Content:   "这是什么",
		MessageReference: &discordgo.MessageReference{
			MessageID: "m1",
			ChannelID: "c1",
		},
		ReferencedMessage: &discordgo.Message{
			ID: "m1",
			Attachments: []*discordgo.MessageAttachment{{
				ID:          "att1",
				URL:         "https://cdn.discordapp.com/attachments/c1/m1/a.png",
				ContentType: "image/png",
				Filename:    "a.png",
				Size:        1024,
				Width:       100,
				Height:      80,
			}},
		},
	}
	segs := buildDiscordSegments(m)
	if len(segs) == 0 || segs[0].Type != platform.SegmentReply {
		t.Fatalf("expected reply segment first, got %+v", segs)
	}
	if segs[0].ReplyToID != "m1" {
		t.Errorf("expected ReplyToID m1, got %q", segs[0].ReplyToID)
	}
	atts, ok := segs[0].Extra[platform.SegmentExtraQuoteAtts].([]platform.Attachment)
	if !ok || len(atts) != 1 {
		t.Fatalf("expected normalized quote attachment in reply Extra, got %+v", segs[0].Extra)
	}
	if atts[0].URL != "https://cdn.discordapp.com/attachments/c1/m1/a.png" || atts[0].MimeType != "image/png" {
		t.Errorf("unexpected quote attachment: %+v", atts[0])
	}
}

// TestBuildDiscordSegmentsNoReferencedMessage 无 referenced_message（已删除 /
// 未内嵌）时引用段不携带引用附件键，由适配器回查路径补齐。
func TestBuildDiscordSegmentsNoReferencedMessage(t *testing.T) {
	m := &discordgo.Message{
		ID:               "m2",
		ChannelID:        "c1",
		MessageReference: &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	segs := buildDiscordSegments(m)
	if len(segs) == 0 || segs[0].Type != platform.SegmentReply {
		t.Fatalf("expected reply segment first, got %+v", segs)
	}
	if _, ok := segs[0].Extra[platform.SegmentExtraQuoteAtts]; ok {
		t.Error("expected no quote attachments without referenced_message")
	}
}

// ── fetchReferencedMessage 回查 ─────────────────────────────────────────

// TestFetchReferencedMessageSuccess 回查成功时补全 ReferencedMessage。
func TestFetchReferencedMessageSuccess(t *testing.T) {
	orig := fetchChannelMessage
	fetchChannelMessage = func(s *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
		if channelID != "c1" || messageID != "m1" {
			t.Fatalf("unexpected args: %q %q", channelID, messageID)
		}
		return &discordgo.Message{ID: "m1", ChannelID: "c1"}, nil
	}
	t.Cleanup(func() { fetchChannelMessage = orig })

	m := &discordgo.Message{
		ID:               "m2",
		ChannelID:        "c1",
		MessageReference: &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	fetchReferencedMessage(&discordgo.Session{}, m)
	if m.ReferencedMessage == nil || m.ReferencedMessage.ID != "m1" {
		t.Fatalf("expected referenced message filled, got %+v", m.ReferencedMessage)
	}
}

// TestFetchReferencedMessageAlreadyPresent 已内嵌 referenced_message 时不再回查。
func TestFetchReferencedMessageAlreadyPresent(t *testing.T) {
	calls := 0
	orig := fetchChannelMessage
	fetchChannelMessage = func(*discordgo.Session, string, string) (*discordgo.Message, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { fetchChannelMessage = orig })

	m := &discordgo.Message{
		ID:                "m2",
		ChannelID:         "c1",
		ReferencedMessage: &discordgo.Message{ID: "m1"},
		MessageReference:  &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	fetchReferencedMessage(&discordgo.Session{}, m)
	if calls != 0 {
		t.Errorf("expected no fetch when referenced_message embedded, got %d calls", calls)
	}
}

// TestFetchReferencedMessageError 回查失败静默跳过，不 panic。
func TestFetchReferencedMessageError(t *testing.T) {
	orig := fetchChannelMessage
	fetchChannelMessage = func(*discordgo.Session, string, string) (*discordgo.Message, error) {
		return nil, errors.New("404")
	}
	t.Cleanup(func() { fetchChannelMessage = orig })

	m := &discordgo.Message{
		ID:               "m2",
		ChannelID:        "c1",
		MessageReference: &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	fetchReferencedMessage(&discordgo.Session{}, m)
	if m.ReferencedMessage != nil {
		t.Errorf("expected no referenced message on error, got %+v", m.ReferencedMessage)
	}
}

// TestFetchReferencedMessageStateCache 状态缓存命中时零网络调用。
func TestFetchReferencedMessageStateCache(t *testing.T) {
	calls := 0
	orig := fetchChannelMessage
	fetchChannelMessage = func(*discordgo.Session, string, string) (*discordgo.Message, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { fetchChannelMessage = orig })

	sess := &discordgo.Session{State: discordgo.NewState()}
	sess.State.MaxMessageCount = 100
	if err := sess.State.ChannelAdd(&discordgo.Channel{ID: "c1", Type: discordgo.ChannelTypeDM}); err != nil {
		t.Fatal(err)
	}
	if err := sess.State.MessageAdd(&discordgo.Message{ID: "m1", ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}

	m := &discordgo.Message{
		ID:               "m2",
		ChannelID:        "c1",
		MessageReference: &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	fetchReferencedMessage(sess, m)
	if calls != 0 {
		t.Errorf("expected state cache hit without REST call, got %d calls", calls)
	}
	if m.ReferencedMessage == nil || m.ReferencedMessage.ID != "m1" {
		t.Fatalf("expected referenced message from cache, got %+v", m.ReferencedMessage)
	}
}

// TestFetchReferencedMessageSemaphoreFull 并发信号量已满时直接跳过（不排队、不阻塞）。
func TestFetchReferencedMessageSemaphoreFull(t *testing.T) {
	calls := 0
	orig := fetchChannelMessage
	origTimeout := quotedFetchTimeout
	quotedFetchTimeout = 100 * time.Millisecond
	fetchChannelMessage = func(*discordgo.Session, string, string) (*discordgo.Message, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() {
		fetchChannelMessage = orig
		quotedFetchTimeout = origTimeout
	})

	// 占满信号量
	for i := 0; i < cap(quotedFetchSem); i++ {
		quotedFetchSem <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(quotedFetchSem); i++ {
			<-quotedFetchSem
		}
	}()

	m := &discordgo.Message{
		ID:               "m2",
		ChannelID:        "c1",
		MessageReference: &discordgo.MessageReference{MessageID: "m1", ChannelID: "c1"},
	}
	fetchReferencedMessage(&discordgo.Session{}, m)
	if calls != 0 {
		t.Errorf("expected no fetch when semaphore full, got %d calls", calls)
	}
	if m.ReferencedMessage != nil {
		t.Errorf("expected no fill when semaphore full, got %+v", m.ReferencedMessage)
	}
}
