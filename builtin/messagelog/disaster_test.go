package messagelog

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCrashRecovery_NoLossNoDup 模拟 kill -9：已落库记录不受影响，spool 遗留
// 未 ack 记录在重启后重放且恰好一次（event_id 幂等）。
func TestCrashRecovery_NoLossNoDup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crash.db")
	spoolDir := filepath.Join(t.TempDir(), "spool")

	// 阶段 1：正常记录 3 条并落库
	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	l1 := New(10)
	l1.UseDB(db1)
	l1.Start(Options{SpoolEnabled: true, SpoolDir: spoolDir, SpoolMaxSize: 64 << 20, BatchSize: 100})
	now := time.Now()
	for i := range 3 {
		l1.Record(RecordEntry{EventID: fmt.Sprintf("ok-%d", i), ChatID: "g1", Content: "done", Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	l1.Stop()
	closeDB(t, db1)

	// 阶段 2：模拟崩溃——向 spool 追加 2 条未 ack 记录（落库前进程被杀）
	sp, err := openSpool(spoolDir, 64<<20)
	if err != nil {
		t.Fatalf("openSpool: %v", err)
	}
	for i := range 2 {
		if err := sp.append(RecordEntry{EventID: fmt.Sprintf("crash-%d", i), ChatID: "g1", Content: "lost?", Timestamp: now.Add(time.Duration(10+i) * time.Second)}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := sp.close(); err != nil {
		t.Fatalf("close spool: %v", err)
	}

	// 阶段 3：重启 → 重放恰好一次，0 丢失 0 重复
	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("reopen DB: %v", err)
	}
	defer closeDB(t, db2)
	l2 := New(10)
	l2.UseDB(db2)
	l2.Start(Options{SpoolEnabled: true, SpoolDir: spoolDir, SpoolMaxSize: 64 << 20, BatchSize: 100})
	l2.Stop()

	var total int64
	if err := db2.Model(&MessageRecord{}).Count(&total).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected 5 rows (3 live + 2 replayed), got %d", total)
	}
	for i := range 2 {
		var m MessageRecord
		if err := db2.Where("event_id = ?", fmt.Sprintf("crash-%d", i)).First(&m).Error; err != nil {
			t.Fatalf("crash-%d not replayed: %v", i, err)
		}
	}
	var dup int64
	db2.Model(&MessageRecord{}).Distinct("event_id").Count(&dup)
	if dup != 5 {
		t.Fatalf("expected 5 distinct event_ids, got %d", dup)
	}
}

// TestFlush_ConcurrentCommitRace 多生产者并发记录：SQLite 提交恰好一次，0 丢失 0 重复。
func TestFlush_ConcurrentCommitRace(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "race.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	l := New(1000)
	l.UseDB(db)
	// race 模式下纯 Go SQLite 驱动显著变慢，放宽排空上限避免 Stop 超时误报丢失
	l.Start(Options{
		FlushInterval:    10 * time.Millisecond,
		BatchSize:        500,
		QueueSize:        200000,
		StopDrainTimeout: 2 * time.Minute,
	})
	defer l.Stop()

	const producers = 8
	const perProducer = 500
	now := time.Now()
	var wg sync.WaitGroup
	for g := range producers {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range perProducer {
				l.Record(RecordEntry{
					EventID:   fmt.Sprintf("ev-%d-%d", g, i),
					ChatID:    "g1",
					UserID:    "u1",
					Content:   "msg",
					Timestamp: now.Add(time.Duration(g*perProducer+i) * time.Second),
				})
			}
		}(g)
	}
	wg.Wait()
	l.Stop()

	var total int64
	if err := db.Model(&MessageRecord{}).Count(&total).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	const want = producers * perProducer
	if total != want {
		t.Fatalf("expected %d rows, got %d (loss detected)", want, total)
	}
	var distinct int64
	db.Model(&MessageRecord{}).Distinct("event_id").Count(&distinct)
	if distinct != want {
		t.Fatalf("expected %d distinct event_ids, got %d (duplicates detected)", want, distinct)
	}
}

// TestSendUnknown_TimeoutRecoveredOnRestart 发送超时后崩溃：重启把遗留 pending
// 标记为 unknown（不伪造 sent/failed），且恰好一行。
func TestSendUnknown_TimeoutRecoveredOnRestart(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "unknown.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(t, db)

	now := time.Now()
	// 阶段 1：pending 出站已落库（平台可能已送达，结果未知）
	l1 := New(10)
	l1.UseDB(db)
	l1.Start(Options{FlushInterval: 10 * time.Millisecond, BatchSize: 100})
	l1.Record(RecordEntry{
		EventID: "out-1", ChatID: "g1", Content: "hello", Timestamp: now,
		IsOutbound: true, SendStatus: SendStatusPending,
	})
	l1.Stop()

	// 阶段 2：重启（结果记录永远不来，类似超时后进程被杀）
	l2 := New(10)
	l2.UseDB(db)
	l2.Start(Options{FlushInterval: 10 * time.Millisecond, BatchSize: 100})
	l2.Stop()

	var m MessageRecord
	if err := db.Where("event_id = ?", "out-1").First(&m).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if m.SendStatus != string(SendStatusUnknown) {
		t.Fatalf("expected unknown after restart, got %q", m.SendStatus)
	}
	var cnt int64
	db.Model(&MessageRecord{}).Where("event_id = ?", "out-1").Count(&cnt)
	if cnt != 1 {
		t.Fatalf("expected exactly 1 row, got %d", cnt)
	}
}
