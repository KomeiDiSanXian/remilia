package messagelog

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog/attachments"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newAttachmentLogger 创建带附件管理器的 Logger（scope=none 避免启动扫描触发真实下载）。
func newAttachmentLogger(t *testing.T, scope string) (*Logger, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-image-bytes"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	l := New(20)
	l.UseDB(db)
	l.Start(Options{
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     10,
		Attachments: &AttachmentOptions{
			Dir:                 filepath.Join(dir, "att"),
			Scope:               scope,
			HotWindowAge:        24 * time.Hour,
			MaxPending:          100,
			DownloadConcurrency: 2,
			DownloadRetries:     1,
			DownloadBackoff:     []time.Duration{time.Millisecond},
			MaxSize:             1 << 20,
			GCEnabled:           true,
			GracePeriod:         time.Hour,
		},
	})
	t.Cleanup(l.Stop)
	return l, srv
}

// TestFlush_RecordsAttachmentRows URL 附件元数据始终落库，状态按下载范围。
func TestFlush_RecordsAttachmentRows(t *testing.T) {
	l, srv := newAttachmentLogger(t, "none") // scope=none → 全部 pending_lazy
	now := time.Now()
	eid := l.Record(RecordEntry{
		ChatID:    "g1",
		UserID:    "u1",
		Content:   "带图消息",
		Timestamp: now,
		Attachments: []AttachmentMeta{
			{Type: "image", Name: "img.png", URL: srv.URL + "/img.png"},
		},
	})
	l.Stop() // 排空落库

	var rows []AttachmentRecord
	if err := l.db.Where("event_id = ?", eid).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 attachment row, got %d", len(rows))
	}
	if rows[0].Status != string(attachments.StatusPendingLazy) {
		t.Fatalf("status = %q, want pending_lazy (scope=none)", rows[0].Status)
	}
	if rows[0].Type != "image" || rows[0].URL == "" || rows[0].EventID != eid {
		t.Fatalf("row mismatch: %+v", rows[0])
	}
}

// TestFetch_LazyDownload pending_lazy 附件经 Fetch 触发下载 → ready 且可读。
func TestFetch_LazyDownload(t *testing.T) {
	l, srv := newAttachmentLogger(t, "none")
	now := time.Now()
	eid := l.Record(RecordEntry{
		ChatID:    "g1",
		UserID:    "u1",
		Content:   "懒加载图",
		Timestamp: now,
		Attachments: []AttachmentMeta{
			{Type: "image", Name: "img.png", URL: srv.URL + "/img.png"},
		},
	})
	l.Stop()

	var rows []AttachmentRecord
	if err := l.db.Where("event_id = ?", eid).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Status != string(attachments.StatusPendingLazy) {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	rc, err := l.Fetch(rows[0].ID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "fake-image-bytes" {
		t.Fatalf("content = %q", data)
	}
	// 行已就绪
	row := AttachmentRecord{}
	l.db.First(&row, rows[0].ID)
	if row.Status != string(attachments.StatusReady) || row.StorageKey == "" {
		t.Fatalf("row after fetch: %+v", row)
	}
}

// TestQuery_RequireAttachments 仅返回带附件的消息。
func TestQuery_RequireAttachments(t *testing.T) {
	l, _ := newAttachmentLogger(t, "none")
	now := time.Now()
	l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "纯文本", Timestamp: now})
	l.Record(RecordEntry{
		ChatID: "g1", UserID: "u1", Content: "带图", Timestamp: now.Add(time.Second),
		Attachments: []AttachmentMeta{{Type: "image", Name: "a.png", URL: "http://x/a.png"}},
	})
	l.Stop()

	all := l.QueryChat("g1", 10, QueryOptions{})
	if len(all) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(all))
	}
	withAtt := l.QueryChat("g1", 10, QueryOptions{RequireAttachments: true})
	if len(withAtt) != 1 || withAtt[0].Content != "带图" {
		t.Fatalf("RequireAttachments = %+v", withAtt)
	}
	if len(withAtt[0].Attachments) != 1 {
		t.Fatalf("attachment meta missing: %+v", withAtt[0].Attachments)
	}
}

// TestOutboundDataAttachmentStored 出站 Data 附件：flush 时写入 Store → ready。
func TestOutboundDataAttachmentStored(t *testing.T) {
	l, _ := newAttachmentLogger(t, "none")
	l.RecordOutboundSent("g1", "pm-1", "带图出站", time.Now())
	// 模拟 OnOutbound：带 Data 附件
	req := platform.SendRequest{
		Message: platform.OutboundMessage{Text: "图"}.WithAttachments(platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Data:     []byte("bot-generated-image"),
			Name:     "bot.png",
			MimeType: "image/png",
		}),
	}
	e := outboundEntry("g1", req)
	e.EventID = "test-outbound-eid-1"
	l.setOutboundData(e.EventID, req.Message.Attachments)
	l.Record(e)
	l.Stop()

	var rows []AttachmentRecord
	if err := l.db.Where("event_id = ?", e.EventID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 attachment row, got %d", len(rows))
	}
	if rows[0].Status != string(attachments.StatusReady) || rows[0].StorageKey == "" {
		t.Fatalf("data attachment should be ready: %+v", rows[0])
	}
	rc, err := l.Fetch(rows[0].ID)
	if err != nil {
		t.Fatalf("Fetch data attachment: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "bot-generated-image" {
		t.Fatalf("content = %q", data)
	}
}

// TestRetention_DeletesAttachmentRefs retention 删除消息时同步删除附件引用行。
func TestRetention_DeletesAttachmentRefs(t *testing.T) {
	l, srv := newAttachmentLogger(t, "none")
	now := time.Now()
	// 记录 5 条，其中 3 条带附件
	for i := range 5 {
		e := RecordEntry{ChatID: "g1", UserID: "u1", Content: "m", Timestamp: now.Add(time.Duration(i) * time.Second)}
		if i < 3 {
			e.Attachments = []AttachmentMeta{{Type: "image", Name: "a.png", URL: srv.URL + "/img.png"}}
		}
		l.Record(e)
	}
	l.Stop()

	var total int64
	l.db.Model(&MessageRecord{}).Count(&total)
	if total != 5 {
		t.Fatalf("expected 5 messages, got %d", total)
	}
	// 保留 2 条（max_entries=2）→ 删除最旧 3 条及其附件引用
	l.retention = RetentionOptions{MaxEntries: 2}
	l.applyRetention()

	l.db.Model(&MessageRecord{}).Count(&total)
	if total != 2 {
		t.Fatalf("expected 2 messages after retention, got %d", total)
	}
	var attRows int64
	l.db.Model(&AttachmentRecord{}).Count(&attRows)
	if attRows != 0 {
		t.Fatalf("expected 0 attachment rows after retention, got %d", attRows)
	}
}
