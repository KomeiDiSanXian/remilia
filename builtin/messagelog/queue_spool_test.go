package messagelog

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestLogger 构造一个启用 spool 的 Logger（测试用临时目录）。
func newTestLogger(t *testing.T, cap, queueSize int, opts ...Options) *Logger {
	t.Helper()
	l := New(cap)
	dir := filepath.Join(t.TempDir(), "spool")
	o := Options{
		SpoolEnabled: true,
		SpoolDir:     dir,
		SpoolMaxSize: 64 << 20,
		QueueSize:    queueSize,
		BatchSize:    100,
	}
	if len(opts) > 0 {
		o = opts[0]
		if o.SpoolDir == "" {
			o.SpoolDir = dir
		}
	}
	l.Start(o)
	t.Cleanup(l.Stop)
	return l
}

// TestDurableQueue_OverflowUnit 内存队列满 → spool 兜底 → drain + ack 语义。
func TestDurableQueue_OverflowUnit(t *testing.T) {
	sp, err := openSpool(filepath.Join(t.TempDir(), "spool"), 64<<20)
	if err != nil {
		t.Fatalf("openSpool: %v", err)
	}
	defer sp.close()

	q := newDurableQueue(4, sp)
	now := time.Now()
	for i := range 20 {
		q.enqueue(RecordEntry{EventID: string(rune('a' + i)), Content: "m", Timestamp: now})
	}
	if got := q.depth(); got != 4 {
		t.Fatalf("expected 4 in memory, got %d", got)
	}
	if got := sp.unacked(); got != 16 {
		t.Fatalf("expected 16 in spool, got %d", got)
	}

	all := q.drain(100)
	if len(all) != 20 {
		t.Fatalf("expected 20 drained, got %d", len(all))
	}
	// 只有来自 spool 的记录带偏移
	spoolSourced := 0
	for _, r := range all {
		if r.spoolNext > 0 {
			spoolSourced++
		}
	}
	if spoolSourced != 16 {
		t.Fatalf("expected 16 spool-sourced records, got %d", spoolSourced)
	}
	q.ack(all)
	if sp.unacked() != 0 {
		t.Fatalf("expected spool fully acked, got %d unacked", sp.unacked())
	}
}

// TestSpool_AppendReadAck 追加 → 读取 → ack；全部消费后文件截断。
func TestSpool_AppendReadAck(t *testing.T) {
	sp, err := openSpool(filepath.Join(t.TempDir(), "spool"), 64<<20)
	if err != nil {
		t.Fatalf("openSpool: %v", err)
	}
	defer sp.close()

	now := time.Now()
	for i := range 5 {
		if err := sp.append(RecordEntry{EventID: string(rune('a' + i)), Content: "msg", Timestamp: now}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if got := sp.unacked(); got != 5 {
		t.Fatalf("expected 5 unacked, got %d", got)
	}

	recs, err := sp.read(sp.cursor, 3)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	if err := sp.ack(recs[len(recs)-1].next, len(recs)); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := sp.unacked(); got != 2 {
		t.Fatalf("expected 2 unacked after partial ack, got %d", got)
	}

	// 全部消费后截断
	recs, err = sp.read(sp.cursor, 10)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if err := sp.ack(recs[len(recs)-1].next, len(recs)); err != nil {
		t.Fatalf("ack2: %v", err)
	}
	if sp.size != 0 || sp.cursor != 0 || sp.unacked() != 0 {
		t.Fatalf("expected truncated spool, got size=%d cursor=%d unacked=%d", sp.size, sp.cursor, sp.unacked())
	}
}

// TestSpool_ReplayAfterReopen crash 后重开：未 ack 记录可重放。
func TestSpool_ReplayAfterReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	sp, err := openSpool(dir, 64<<20)
	if err != nil {
		t.Fatalf("openSpool: %v", err)
	}
	now := time.Now()
	for i := range 3 {
		if err := sp.append(RecordEntry{EventID: string(rune('a' + i)), Content: "msg", Timestamp: now}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := sp.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// 模拟 crash：直接重开（cursor 未推进）
	sp2, err := openSpool(dir, 64<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sp2.close()
	recs, err := sp2.read(sp2.cursor, 10)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records after reopen, got %d", len(recs))
	}
}

// TestDurableQueue_OverflowToSpool 内存队列满 → spool 兜底 → 全部落库不丢。
func TestDurableQueue_OverflowToSpool(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "overflow.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	l := newTestLogger(t, 10, 8, Options{
		SpoolEnabled: true,
		SpoolDir:     filepath.Join(t.TempDir(), "spool"),
		SpoolMaxSize: 64 << 20,
		QueueSize:    8,
		BatchSize:    50,
	})
	l.UseDB(db)
	defer closeDB(t, db)

	now := time.Now()
	const n = 100
	for i := range n {
		l.Record(RecordEntry{
			EventID:   string(rune('a' + i)),
			ChatID:    "g1",
			UserID:    "u1",
			Content:   "消息",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	l.Stop() // 排空 + 落库

	var count int64
	if err := db.Model(&MessageRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Fatalf("expected %d rows, got %d", n, count)
	}
}

// TestFlush_SQLiteDown_RecordsKeptInSpool SQLite 不可用 → 记录留在 spool 不丢。
func TestFlush_SQLiteDown_RecordsKeptInSpool(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "down.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	spoolDir := filepath.Join(t.TempDir(), "spool")
	l := newTestLogger(t, 10, 100, Options{
		SpoolEnabled:  true,
		SpoolDir:      spoolDir,
		SpoolMaxSize:  64 << 20,
		QueueSize:     100,
		BatchSize:     10,
		FlushInterval: 20 * time.Millisecond,
	})
	l.UseDB(db)

	// 先正常写一批，确认落库
	now := time.Now()
	l.Record(RecordEntry{EventID: "ok-1", ChatID: "g1", UserID: "u1", Content: "ok", Timestamp: now})
	l.Stop()
	defer closeDB(t, db)

	var okCount int64
	if err := db.Model(&MessageRecord{}).Where("event_id = ?", "ok-1").Count(&okCount).Error; err != nil {
		t.Fatalf("count ok: %v", err)
	}
	if okCount != 1 {
		t.Fatalf("expected ok-1 persisted, got %d", okCount)
	}

	// 关闭底层连接，模拟 SQLite 不可用
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	l2 := newTestLogger(t, 10, 100, Options{
		SpoolEnabled:  true,
		SpoolDir:      spoolDir,
		SpoolMaxSize:  64 << 20,
		QueueSize:     100,
		BatchSize:     10,
		FlushInterval: 20 * time.Millisecond,
	})
	// 复用已关闭的 db：flush 必然失败
	l2.UseDB(db)
	l2.Record(RecordEntry{EventID: "kept-1", ChatID: "g1", UserID: "u1", Content: "kept", Timestamp: time.Now()})
	l2.Stop()

	if l2.queue.spool == nil || l2.queue.spool.unacked() == 0 {
		t.Fatal("expected records kept in spool when SQLite unavailable")
	}
}

// TestFlush_StopDrainsAll Stop 后排空全部待处理记录（含 spool 重放）。
func TestFlush_StopDrainsAll(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "drain.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	spoolDir := filepath.Join(t.TempDir(), "spool")
	// 预写 spool：直接 append（模拟上一轮 crash 遗留）
	sp, err := openSpool(spoolDir, 64<<20)
	if err != nil {
		t.Fatalf("openSpool: %v", err)
	}
	now := time.Now()
	for i := range 5 {
		if err := sp.append(RecordEntry{
			EventID:   string(rune('a' + i)),
			ChatID:    "g1",
			UserID:    "u1",
			Content:   "replay",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := sp.close(); err != nil {
		t.Fatalf("close spool: %v", err)
	}

	// 启动后正常排空：spool 遗留记录重放落库
	l := New(10)
	l.UseDB(db)
	l.Start(Options{SpoolEnabled: true, SpoolDir: spoolDir, SpoolMaxSize: 64 << 20, BatchSize: 50})
	l.Record(RecordEntry{EventID: "live-1", ChatID: "g1", UserID: "u1", Content: "live", Timestamp: time.Now()})
	l.Stop()

	var count int64
	if err := db.Model(&MessageRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 6 {
		t.Fatalf("expected 6 rows (5 replay + 1 live), got %d", count)
	}
}
