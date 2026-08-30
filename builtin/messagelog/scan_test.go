package messagelog

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// fakePublisher 记录发布的事件（同步写入，便于断言）。
type fakePublisher struct {
	mu      sync.Mutex
	events  []MessageRecorded
	failErr error
}

func (f *fakePublisher) PublishContext(_ context.Context, topic string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	if topic == TopicMessageRecorded {
		if ev, ok := data.(MessageRecorded); ok {
			f.events = append(f.events, ev)
		}
	}
	return nil
}

func (f *fakePublisher) snapshot() []MessageRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]MessageRecorded, len(f.events))
	copy(out, f.events)
	return out
}

// TestBroadcast_PublishesAfterFlush flush 提交成功后广播 MessageRecorded 事件。
func TestBroadcast_PublishesAfterFlush(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "broadcast.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	pub := &fakePublisher{}
	l := New(10)
	l.SetEventPublisher(pub)
	l.UseDB(db)
	l.Start(Options{FlushInterval: 10 * time.Millisecond, BatchSize: 10})

	now := time.Now()
	l.Record(RecordEntry{EventID: "ev-1", ChatID: "g1", UserID: "u1", Content: "hello", Timestamp: now})
	l.Record(RecordEntry{EventID: "ev-2", ChatID: "g1", UserID: "u2", Content: "world", Timestamp: now.Add(time.Second)})
	l.Stop()

	events := pub.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(events))
	}
	if events[0].Record.EventID != "ev-1" || events[1].Record.EventID != "ev-2" {
		t.Errorf("unexpected event order: %+v", events)
	}
	if events[0].Record.ChatID != "g1" || events[0].Record.Content != "hello" {
		t.Errorf("unexpected event payload: %+v", events[0])
	}
}

// TestBroadcast_NoPublisherNoEvents 未注入 publisher 时广播循环不启动、不阻塞。
func TestBroadcast_NoPublisherNoEvents(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "nopub.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	l := New(10)
	l.UseDB(db)
	l.Start(Options{FlushInterval: 10 * time.Millisecond})
	l.Record(RecordEntry{EventID: "ev-1", ChatID: "g1", Content: "hi", Timestamp: time.Now()})
	l.Stop() // 不 panic、不挂起
}

// TestBroadcast_StopDrainsPending 停止时 flushLoop 收尾批次同样被广播。
func TestBroadcast_StopDrainsPending(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "drain_broadcast.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	pub := &fakePublisher{}
	l := New(10)
	l.SetEventPublisher(pub)
	l.UseDB(db)
	l.Start(Options{FlushInterval: time.Hour, BatchSize: 100}) // 不触发定时 flush

	const n = 5
	now := time.Now()
	for i := range n {
		l.Record(RecordEntry{EventID: string(rune('a' + i)), ChatID: "g1", Content: "m", Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	l.Stop() // 触发收尾 flush

	if got := len(pub.snapshot()); got != n {
		t.Fatalf("expected %d published events after stop drain, got %d", n, got)
	}
}

// TestScanAfter_FullRebuild 全量重建：按 (timestamp, id) 升序返回全部记录。
func TestScanAfter_FullRebuild(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	l := New(10)
	l.UseDB(db)
	l.Start(Options{FlushInterval: time.Hour, BatchSize: 100})
	now := time.Now()
	for i := range 7 {
		l.Record(RecordEntry{
			EventID:   string(rune('a' + i)),
			ChatID:    "g1",
			UserID:    "u1",
			Content:   "msg",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	l.Stop()

	var got []string
	if err := l.ScanAfter(time.Time{}, 0, func(e RecordEntry) error {
		got = append(got, e.EventID)
		return nil
	}); err != nil {
		t.Fatalf("ScanAfter: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("expected 7 records, got %d", len(got))
	}
	for i, id := range got {
		if id != string(rune('a'+i)) {
			t.Fatalf("expected ordered scan, got %v", got)
		}
	}
}

// TestScanAfter_ResumeFromCursor 从 (timestamp, id) 游标续扫，不重复不遗漏。
func TestScanAfter_ResumeFromCursor(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "scan_cursor.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	l := New(10)
	l.UseDB(db)
	l.Start(Options{FlushInterval: time.Hour, BatchSize: 100})
	now := time.Now()
	for i := range 5 {
		l.Record(RecordEntry{
			EventID:   string(rune('a' + i)),
			ChatID:    "g1",
			Content:   "msg",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	l.Stop()

	// 先全量扫到第 3 条，记录游标
	var cursorTS time.Time
	var cursorID int64
	count := 0
	if err := l.ScanAfter(time.Time{}, 0, func(e RecordEntry) error {
		count++
		if count == 3 {
			cursorTS = e.Timestamp
			// 从 DB 回读该记录的 id（ScanAfter 回调不暴露 id）
			var m MessageRecord
			if err := db.Where("event_id = ?", e.EventID).First(&m).Error; err != nil {
				return err
			}
			cursorID = m.ID
		}
		return nil
	}); err != nil {
		t.Fatalf("first scan: %v", err)
	}

	var resumed []string
	if err := l.ScanAfter(cursorTS, cursorID, func(e RecordEntry) error {
		resumed = append(resumed, e.EventID)
		return nil
	}); err != nil {
		t.Fatalf("resume scan: %v", err)
	}
	if len(resumed) != 2 {
		t.Fatalf("expected 2 remaining records, got %d: %v", len(resumed), resumed)
	}
	if resumed[0] != "d" || resumed[1] != "e" {
		t.Fatalf("unexpected resume order: %v", resumed)
	}
}

// TestScanAfter_IncludesMentionsAndAttachments 重建补齐 mentions 与 attachments。
func TestScanAfter_IncludesMentionsAndAttachments(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "scan_join.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	l := New(10)
	l.UseDB(db)
	l.Start(Options{FlushInterval: time.Hour, BatchSize: 100})
	l.Record(RecordEntry{
		EventID:   "ev-with",
		ChatID:    "g1",
		Content:   "hi",
		Timestamp: time.Now(),
		Mentions:  []platform.UserInfo{{ID: "u9", DisplayName: "nine"}},
		Attachments: []AttachmentMeta{
			{Type: "image", Name: "a.png", URL: "http://x/a", Status: "ready"},
		},
	})
	l.Stop()

	var got RecordEntry
	if err := l.ScanAfter(time.Time{}, 0, func(e RecordEntry) error {
		got = e
		return nil
	}); err != nil {
		t.Fatalf("ScanAfter: %v", err)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].ID != "u9" {
		t.Errorf("expected mention restored, got %+v", got.Mentions)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "a.png" {
		t.Errorf("expected attachment restored, got %+v", got.Attachments)
	}
}
