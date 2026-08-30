package attachments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestGCRefCountReconcile：引用数归零且宽限期过后才回收；存在引用不误删。
func TestGCRefCountReconcile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "att")
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := fs.Put(newBytesReader([]byte("shared-binary")), 0)
	if err != nil {
		t.Fatal(err)
	}

	db := newTestDB(t, filepath.Join(dir, "test.db"))
	// 两条消息引用同一 sha256（内容去重）
	id1 := insertRow(t, db, Row{Type: "image", URL: "http://x/1", Name: "1", Status: StatusReady, StorageKey: key, SHA256: key})
	id2 := insertRow(t, db, Row{Type: "image", URL: "http://x/2", Name: "2", Status: StatusReady, StorageKey: key, SHA256: key})

	cfg := Config{Dir: dir, GCEnabled: true, GracePeriod: time.Hour}
	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.gcOnce(context.Background())
	// 仍有引用：不删
	rc, err := fs.Open(key)
	if err != nil {
		t.Fatalf("binary should be retained while referenced: %v", err)
	}
	rc.Close()

	// 删除全部引用
	db.Delete(&Row{}, id1)
	db.Delete(&Row{}, id2)

	// 宽限期未过：不删
	m.gcOnce(context.Background())
	rc, err = fs.Open(key)
	if err != nil {
		t.Fatalf("binary should be retained within grace period: %v", err)
	}
	rc.Close()

	// 让 mtime 过期（模拟宽限期已过）
	path := filepath.Join(dir, key)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	m.cfg.GracePeriod = time.Hour
	m.gcOnce(context.Background())

	rc, err = fs.Open(key)
	if err == nil {
		rc.Close()
		t.Fatal("binary should be deleted after grace period")
	}
	var cnt int64
	db.Model(&Row{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("expected no rows after delete+GC, got %d", cnt)
	}
}

// TestGCNewReferencePreventsDelete：引用存在（含删除瞬间前插入的新引用）→ 不误删。
func TestGCNewReferencePreventsDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "att")
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := fs.Put(newBytesReader([]byte("race-binary")), 0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, key)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)

	db := newTestDB(t, filepath.Join(dir, "test.db"))
	// 旧引用已被删除
	cfg := Config{Dir: dir, GracePeriod: time.Hour}
	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 新增引用（模拟并发：引用在计数之前插入，reconcile 以 COUNT(*) 为准）
	insertRow(t, db, Row{Type: "image", URL: "http://x/new", Name: "new", Status: StatusReady, StorageKey: key, SHA256: key})
	m.gcOnce(context.Background())
	rc, err := fs.Open(key)
	if err != nil {
		t.Fatalf("binary should survive new reference: %v", err)
	}
	rc.Close()
}

// TestGC_ConcurrentInsertRace 并发 GC + 真实下载流竞争：引用行只经 downloadOne
// 的两阶段 finalize（写锁事务内 rename + 标记 ready）产生，与 GC 删除串行化，
// 任意 ready 行的二进制必须存在（共享内容去重 + 引用删除造成的孤儿窗口内
// 重新下载必须恢复二进制）。
func TestGC_ConcurrentInsertRace(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t, filepath.Join(dir, "test.db"))
	cfg := Config{
		Dir:         filepath.Join(dir, "att"),
		GracePeriod: 0, // 引用归零立即回收，放大竞争窗口
		Concurrency: 2,
		MaxPending:  100,
	}
	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.budget = newDiskBudget(0, m.store, nil) // 不限磁盘

	body := []byte("race-binary-2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	u := srv.URL + "/r.png"

	var wg sync.WaitGroup
	stop := make(chan struct{})
	// GC 循环
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				m.gcOnce(context.Background())
			}
		}
	})
	// 真实下载流：插入 pending → downloadOne（两阶段 finalize）→ 半程删除引用
	wg.Go(func() {
		for i := range 40 {
			id := insertRow(t, db, Row{Type: "image", URL: u, Name: "r", Status: StatusPending})
			if _, _, err := m.dl.downloadOne(context.Background(), id, false); err != nil {
				t.Errorf("download failed: %v", err)
				continue
			}
			if i%2 == 0 {
				db.Delete(&Row{}, id)
			}
		}
	})
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// 收敛：再走一轮真实下载 + GC，全部 ready 行的二进制必须存在
	id := insertRow(t, db, Row{Type: "image", URL: u, Name: "final", Status: StatusPending})
	if _, _, err := m.dl.downloadOne(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	m.gcOnce(context.Background())
	var ready []Row
	db.Where("status = ?", StatusReady).Find(&ready)
	if len(ready) == 0 {
		t.Fatal("expected at least one ready row after convergence")
	}
	for _, r := range ready {
		if r.StorageKey == "" {
			t.Fatalf("ready row %d has empty storage key", r.ID)
		}
		rc, err := m.store.Open(r.StorageKey)
		if err != nil {
			t.Fatalf("binary deleted while %d references remain: %v", len(ready), err)
		}
		rc.Close()
	}
}
