package messagelog

import (
	"path/filepath"
	"testing"
	"time"
)

// TestQueryChat_DirectionFilter 方向过滤：inbound / outbound / both。
func TestQueryChat_DirectionFilter(t *testing.T) {
	l := New(20)
	now := time.Now()
	l.Record(RecordEntry{EventID: "in-1", ChatID: "g1", Content: "user", Timestamp: now})
	l.Record(RecordEntry{
		EventID: "out-1", ChatID: "g1", Content: "bot", Timestamp: now.Add(time.Second),
		IsOutbound: true, SendStatus: SendStatusSent,
	})

	if got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionInbound}); len(got) != 1 || got[0].EventID != "in-1" {
		t.Errorf("inbound filter mismatch: %+v", got)
	}
	if got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionOutbound}); len(got) != 1 || got[0].EventID != "out-1" {
		t.Errorf("outbound filter mismatch: %+v", got)
	}
	if got := l.QueryChat("g1", 10, QueryOptions{}); len(got) != 2 {
		t.Errorf("both direction should return 2, got %d", len(got))
	}
}

// TestQueryChat_ExcludePending pending 出站默认包含，ExcludePending 可排除。
func TestQueryChat_ExcludePending(t *testing.T) {
	l := New(20)
	now := time.Now()
	l.Record(RecordEntry{
		EventID: "pending-1", ChatID: "g1", Content: "sending", Timestamp: now,
		IsOutbound: true, SendStatus: SendStatusPending,
	})
	l.Record(RecordEntry{
		EventID: "sent-1", ChatID: "g1", Content: "sent", Timestamp: now.Add(time.Second),
		IsOutbound: true, SendStatus: SendStatusSent,
	})

	if got := l.QueryChat("g1", 10, QueryOptions{}); len(got) != 2 {
		t.Errorf("pending should be included by default, got %d", len(got))
	}
	if got := l.QueryChat("g1", 10, QueryOptions{ExcludePending: true}); len(got) != 1 || got[0].EventID != "sent-1" {
		t.Errorf("ExcludePending mismatch: %+v", got)
	}
}

// TestQueryChat_ExcludesRecalled 撤回消息默认排除，IncludeRecalled 可包含。
func TestQueryChat_ExcludesRecalled(t *testing.T) {
	l := New(20)
	now := time.Now()
	l.Record(RecordEntry{EventID: "recalled-1", ChatID: "g1", Content: "gone", Timestamp: now, IsRecalled: true})
	l.Record(RecordEntry{EventID: "live-1", ChatID: "g1", Content: "here", Timestamp: now.Add(time.Second)})

	if got := l.QueryChat("g1", 10, QueryOptions{}); len(got) != 1 || got[0].EventID != "live-1" {
		t.Errorf("recalled should be excluded by default: %+v", got)
	}
	if got := l.QueryChat("g1", 10, QueryOptions{IncludeRecalled: true}); len(got) != 2 {
		t.Errorf("IncludeRecalled mismatch: got %d", len(got))
	}
}

// TestQueryChat_StableFullOrder 相同时间戳按 (timestamp, event_id) 稳定排序。
func TestQueryChat_StableFullOrder(t *testing.T) {
	l := New(20)
	now := time.Now()
	l.Record(RecordEntry{EventID: "b", ChatID: "g1", Content: "b", Timestamp: now})
	l.Record(RecordEntry{EventID: "a", ChatID: "g1", Content: "a", Timestamp: now})
	got := l.QueryChat("g1", 10, QueryOptions{})
	if len(got) != 2 || got[0].EventID != "a" || got[1].EventID != "b" {
		t.Errorf("expected stable (timestamp, event_id) order, got %+v", got)
	}
}

// TestQueryRange_MergesCacheAndDB 时间窗查询合并热缓存与 DB，按 EventID 去重。
func TestQueryRange_MergesCacheAndDB(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "query_range.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now()
	ts := now.UnixNano()
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "db-1", Content: "db msg", Timestamp: ts - 10, CreatedAt: ts,
	}).Error; err != nil {
		t.Fatalf("create db record: %v", err)
	}
	// 缓存中未落库的条目也应出现在时间窗结果中
	l.Record(RecordEntry{EventID: "mem-1", ChatID: "g1", Content: "mem msg", Timestamp: now})

	entries, err := l.QueryRange("g1", now.Add(-time.Hour), now.Add(time.Hour), 10, QueryOptions{Direction: DirectionInbound})
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 merged entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].EventID != "db-1" || entries[1].EventID != "mem-1" {
		t.Errorf("unexpected merge order: %+v", entries)
	}
}

// TestQueryChat_BackfillsFromDB 缓存不足时以 DB 补齐（重启场景）。
func TestQueryChat_BackfillsFromDB(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "query_backfill.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now().UnixNano()
	for i := range 5 {
		if err := l.db.Create(&MessageRecord{
			ChatID: "g1", EventID: "db-" + string(rune('1'+i)), Content: "msg", Timestamp: now + int64(i), CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	l.Record(RecordEntry{EventID: "mem-1", ChatID: "g1", Content: "mem", Timestamp: time.Unix(0, now+100)})

	got := l.QueryChat("g1", 3, QueryOptions{Direction: DirectionInbound})
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d: %+v", len(got), got)
	}
	if got[2].EventID != "mem-1" {
		t.Errorf("expected newest mem-1 last, got %+v", got)
	}
}

// TestQueryChat_LoadsMentions DB 兜底路径同样加载 @ 提及（与缓存条目一致，
// 群窗口/回复上下文等消费者依赖 Mentions 字段）。
func TestQueryChat_LoadsMentions(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "query_mentions.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	now := time.Now().UnixNano()
	if err := l.db.Create(&MessageRecord{
		ChatID: "g1", EventID: "db-1", Content: "hi", Timestamp: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := l.db.Create(&MessageMention{
		EventID: "db-1", MentionID: "u2", DisplayName: "小红",
	}).Error; err != nil {
		t.Fatalf("create mention: %v", err)
	}

	got := l.QueryChat("g1", 10, QueryOptions{Direction: DirectionInbound})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if len(got[0].Mentions) != 1 || got[0].Mentions[0].DisplayName != "小红" {
		t.Errorf("expected mention loaded from DB, got %+v", got[0].Mentions)
	}
}

// TestAttachmentsByEventID 按 event_id 返回附件行（含 ID/Status），
// deleted 行跳过、其他消息的附件不串。
func TestAttachmentsByEventID(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "att_by_event.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)
	l := New(10)
	l.UseDB(db)

	rows := []AttachmentRecord{
		{EventID: "evt-1", Type: "image", Name: "a.png", MimeType: "image/png", Size: 100, URL: "http://x/a.png", Status: "ready", StorageKey: "k1"},
		{EventID: "evt-1", Type: "file", Name: "b.bin", MimeType: "application/octet-stream", URL: "http://x/b.bin", Status: "pending_lazy"},
		{EventID: "evt-1", Type: "image", Name: "gone.png", URL: "http://x/gone.png", Status: "deleted"},
		{EventID: "evt-2", Type: "image", Name: "other.png", URL: "http://x/other.png", Status: "ready", StorageKey: "k2"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("create attachment row: %v", err)
		}
	}

	atts, err := l.AttachmentsByEventID("evt-1")
	if err != nil {
		t.Fatalf("AttachmentsByEventID: %v", err)
	}
	if len(atts) != 2 {
		t.Fatalf("expected 2 non-deleted rows, got %d: %+v", len(atts), atts)
	}
	if atts[0].ID == 0 || atts[0].Name != "a.png" || atts[0].Status != "ready" || atts[0].StorageKey != "k1" {
		t.Errorf("first row mismatch: %+v", atts[0])
	}
	if atts[1].Name != "b.bin" || atts[1].Status != "pending_lazy" {
		t.Errorf("second row mismatch: %+v", atts[1])
	}
	for _, a := range atts {
		if a.Name == "gone.png" || a.Name == "other.png" {
			t.Errorf("unexpected attachment %q returned", a.Name)
		}
	}

	if got, _ := l.AttachmentsByEventID("no-such-event"); len(got) != 0 {
		t.Errorf("expected empty for unknown event, got %+v", got)
	}
	if got, _ := l.AttachmentsByEventID(""); got != nil {
		t.Errorf("expected nil for empty event_id, got %+v", got)
	}
}
