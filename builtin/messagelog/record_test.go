package messagelog

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRecord_AssignsUUIDv7 未指定 EventID 时自动分配 UUIDv7，热缓存条目一致。
func TestRecord_AssignsUUIDv7(t *testing.T) {
	l := New(10)
	eid := l.Record(RecordEntry{ChatID: "g1", UserID: "u1", Content: "hi", Timestamp: time.Now()})
	if !uuidV7Re.MatchString(eid) {
		t.Fatalf("expected UUIDv7 event_id, got %q", eid)
	}
	msgs := l.QueryGroup("g1", 10)
	if len(msgs) != 1 || msgs[0].EventID != eid {
		t.Fatalf("ring entry event_id mismatch: %+v", msgs)
	}
}

// TestRecord_KeepsProvidedEventID 显式传入 EventID 时保留（测试/重放场景）。
func TestRecord_KeepsProvidedEventID(t *testing.T) {
	l := New(10)
	eid := l.Record(RecordEntry{EventID: "given-id", ChatID: "g1", Content: "hi", Timestamp: time.Now()})
	if eid != "given-id" {
		t.Fatalf("expected event_id preserved, got %q", eid)
	}
}

// TestFlush_IdempotentOnDuplicateEventID 相同 event_id 重复记录只落一条
// （INSERT OR IGNORE + 唯一索引，幂等写入）。
func TestFlush_IdempotentOnDuplicateEventID(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "idem_flush.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	l := New(10)
	l.UseDB(db)
	l.Start()
	defer l.Stop()
	defer closeDB(t, db)

	now := time.Now()
	for range 3 {
		l.Record(RecordEntry{EventID: "dup-1", ChatID: "g1", UserID: "u1", Content: "same", Timestamp: now})
	}
	l.Record(RecordEntry{EventID: "uniq-2", ChatID: "g1", UserID: "u1", Content: "other", Timestamp: now})

	l.Stop() // 触发 flush，确保全部落盘

	var count int64
	if err := db.Model(&MessageRecord{}).Where("event_id = ?", "dup-1").Count(&count).Error; err != nil {
		t.Fatalf("count dup-1: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row for dup-1, got %d", count)
	}
	var total int64
	if err := db.Model(&MessageRecord{}).Count(&total).Error; err != nil {
		t.Fatalf("count total: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 rows total, got %d", total)
	}
}

// TestFlush_RoundTripNewFields 新字段（platform_message_id / send_status /
// 回复链 / 编辑撤回）经 flush 落库后可完整读回。
func TestFlush_RoundTripNewFields(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "roundtrip.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	l := New(10)
	l.UseDB(db)
	l.Start()
	defer l.Stop()
	defer closeDB(t, db)

	now := time.Now()
	ts := now
	eid := l.Record(RecordEntry{
		EventID:           "rt-1",
		PlatformMessageID: "pm-1",
		RequestID:         "req-1",
		Platform:          "test",
		Kind:              "OUTBOUND",
		ChatID:            "g1",
		UserID:            "bot",
		Content:           "出站消息",
		ReplyToMessageID:  "pm-0",
		Timestamp:         ts,
		CreatedAt:         now,
		IsOutbound:        true,
		SendStatus:        SendStatusSent,
		TriggeredBy:       "u1",
	})
	if eid != "rt-1" {
		t.Fatalf("unexpected event_id: %q", eid)
	}
	l.Stop()

	entry, ok := queryFirstByEventID(t, db, "pm-1")
	if !ok {
		t.Fatalf("record not found by platform_message_id")
	}
	if entry.EventID != "rt-1" || entry.PlatformMessageID != "pm-1" {
		t.Errorf("identity mismatch: event=%q platform=%q", entry.EventID, entry.PlatformMessageID)
	}
	if entry.ReplyToMessageID != "pm-0" {
		t.Errorf("expected ReplyToMessageID=pm-0, got %q", entry.ReplyToMessageID)
	}
	if entry.SendStatus != SendStatusSent {
		t.Errorf("expected SendStatus=sent, got %q", entry.SendStatus)
	}
	if entry.TriggeredBy != "u1" {
		t.Errorf("expected TriggeredBy=u1, got %q", entry.TriggeredBy)
	}
	if !entry.IsOutbound || entry.Content != "出站消息" {
		t.Errorf("outbound content mismatch: %+v", entry)
	}
}
