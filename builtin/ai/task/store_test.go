package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
)

// openTaskTestDB 为每个用例打开独立的内存 SQLite。
//
// infrastorage.NewMemory 使用 file::memory:?cache=shared，同一进程内的多个
// 连接会看到彼此的表；任务表名固定，直接复用会让用例相互污染，因此按用例名
// 构造独立 DSN。
func openTaskTestDB(t *testing.T) *infrastorage.Plugin {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	client, err := infrastorage.Open(infrastorage.WithDSN("file:" + name + "?mode=memory&cache=shared"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if sqlDB, err := client.DB().DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return client
}

func TestNewGormStoreNilClient(t *testing.T) {
	store, err := NewGormStore(nil)
	if !errors.Is(err, ErrNilStorageClient) {
		t.Fatalf("NewGormStore(nil) error = %v, want ErrNilStorageClient", err)
	}
	if store != nil {
		t.Fatal("store should be nil when client is nil")
	}
}

func TestGormStoreTableName(t *testing.T) {
	if name := (Record{}).TableName(); name != "ai_tasks" {
		t.Fatalf("TableName() = %q, want ai_tasks", name)
	}
}

func TestGormStoreRoundTrip(t *testing.T) {
	client := openTaskTestDB(t)
	store, err := NewGormStore(client)
	if err != nil {
		t.Fatalf("NewGormStore: %v", err)
	}

	tk := New("修复构建", "s1", "u1", "c1", "qq")
	if err := tk.SyncPlan(planOf("修复构建",
		step("step_1", "分析", session.PlanDone),
		step("step_2", "修改", session.PlanInProgress),
	)); err != nil {
		t.Fatalf("SyncPlan: %v", err)
	}
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.RecordArtifact(Artifact{Kind: ArtifactFileUpdated, Name: "sub/a.go", Size: 12}); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if err := store.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Goal != "修复构建" || got.Status != StatusRunning || got.PlanVersion != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Steps) != 2 || got.Steps[0].Status != StepSucceeded || got.Steps[1].Status != StepRunning {
		t.Fatalf("steps not preserved: %+v", got.Steps)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Name != "sub/a.go" || got.Artifacts[0].Size != 12 {
		t.Fatalf("artifacts not preserved: %+v", got.Artifacts)
	}

	active, err := store.ActiveBySession("s1")
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.ID != tk.ID {
		t.Fatalf("active ID = %q, want %q", active.ID, tk.ID)
	}

	// 取消并保存后，活动查询不再返回该任务，但历史列表保留。
	if err := got.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := store.Save(got); err != nil {
		t.Fatalf("Save(cancelled): %v", err)
	}
	if _, err := store.ActiveBySession("s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ActiveBySession after cancel = %v, want ErrNotFound", err)
	}
	list, err := store.ListBySession("s1")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 1 || list[0].Status != StatusCancelled {
		t.Fatalf("ListBySession = %+v", list)
	}
}

func TestGormStoreActivePicksNewestAndListsDescending(t *testing.T) {
	client := openTaskTestDB(t)
	store, err := NewGormStore(client)
	if err != nil {
		t.Fatalf("NewGormStore: %v", err)
	}

	older := New("旧任务", "s2", "u1", "c1", "qq")
	older.CreatedAt = time.Now().Add(-time.Hour)
	older.UpdatedAt = older.CreatedAt
	newer := New("新任务", "s2", "u1", "c1", "qq")
	for _, tk := range []*Task{older, newer} {
		if err := store.Save(tk); err != nil {
			t.Fatalf("Save(%s): %v", tk.ID, err)
		}
	}

	active, err := store.ActiveBySession("s2")
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.ID != newer.ID {
		t.Fatalf("active ID = %q, want newest %q", active.ID, newer.ID)
	}

	list, err := store.ListBySession("s2")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 2 || list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Fatalf("list order = %+v", list)
	}
}

func TestGormStoreSkipsCorruptedRecords(t *testing.T) {
	client := openTaskTestDB(t)
	store, err := NewGormStore(client)
	if err != nil {
		t.Fatalf("NewGormStore: %v", err)
	}

	valid := New("有效任务", "s3", "u1", "c1", "qq")
	if err := store.Save(valid); err != nil {
		t.Fatalf("Save(valid): %v", err)
	}
	corrupt := &Record{
		ID: "bad", SessionID: "s3", Status: "bogus",
		CreatedAt: time.Now().Add(time.Minute), UpdatedAt: time.Now().Add(time.Minute),
	}
	if err := client.Create(corrupt); err != nil {
		t.Fatalf("Create(corrupt): %v", err)
	}

	if _, err := store.Get("bad"); err == nil {
		t.Fatal("Get on corrupted record should fail")
	}
	active, err := store.ActiveBySession("s3")
	if err != nil {
		t.Fatalf("ActiveBySession should fall back to the valid record: %v", err)
	}
	if active.ID != valid.ID {
		t.Fatalf("active ID = %q, want %q", active.ID, valid.ID)
	}
	list, err := store.ListBySession("s3")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 1 || list[0].ID != valid.ID {
		t.Fatalf("corrupted record should be skipped, got %+v", list)
	}
}

func TestGormStoreNotFound(t *testing.T) {
	client := openTaskTestDB(t)
	store, err := NewGormStore(client)
	if err != nil {
		t.Fatalf("NewGormStore: %v", err)
	}
	if _, err := store.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
	if _, err := store.ActiveBySession("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ActiveBySession(missing) = %v, want ErrNotFound", err)
	}
	list, err := store.ListBySession("missing")
	if err != nil || len(list) != 0 {
		t.Fatalf("ListBySession(missing) = (%v, %v)", list, err)
	}
}

func TestGormStoreListActive(t *testing.T) {
	client := openTaskTestDB(t)
	store, err := NewGormStore(client)
	if err != nil {
		t.Fatalf("NewGormStore: %v", err)
	}

	base := time.Now()
	older := New("活动一", "s1", "u1", "c1", "qq")
	older.CreatedAt = base.Add(-time.Minute)
	newer := New("活动二", "s2", "u1", "c1", "qq")
	newer.CreatedAt = base
	done := New("已完成", "s3", "u1", "c1", "qq")
	done.Status = StatusSucceeded
	for _, tk := range []*Task{older, newer, done} {
		if err := store.Save(tk); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 || active[0].ID != older.ID || active[1].ID != newer.ID {
		t.Fatalf("ListActive = %+v, want oldest first and terminal excluded", active)
	}
}

func TestMemStoreRoundTripAndIsolation(t *testing.T) {
	store := NewMemStore()
	tk := New("目标", "s1", "u1", "c1", "qq")
	tk.Steps = []Step{{ID: "step_1", Description: "一步", Status: StepPending}}
	if err := store.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Steps[0].Description = "changed"
	got.Goal = "changed"
	again, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get(again): %v", err)
	}
	if again.Steps[0].Description != "一步" || again.Goal != "目标" {
		t.Fatal("store must not share mutable state with callers")
	}

	if err := store.Save(nil); err == nil {
		t.Fatal("Save(nil) should fail")
	}
	if _, err := store.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestMemStoreActiveAndList(t *testing.T) {
	store := NewMemStore()
	base := time.Now()
	older := New("旧", "s1", "u1", "c1", "qq")
	older.CreatedAt = base.Add(-time.Hour)
	newer := New("新", "s1", "u1", "c1", "qq")
	newestTerminal := New("已取消", "s1", "u1", "c1", "qq")
	newestTerminal.CreatedAt = base.Add(time.Hour)
	if err := newestTerminal.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	other := New("别的会话", "s2", "u1", "c1", "qq")

	for _, tk := range []*Task{older, newer, newestTerminal, other} {
		if err := store.Save(tk); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	active, err := store.ActiveBySession("s1")
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if active.ID != newer.ID {
		t.Fatalf("active ID = %q, want %q", active.ID, newer.ID)
	}

	list, err := store.ListBySession("s1")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list length = %d, want 3", len(list))
	}
	if list[0].ID != newestTerminal.ID {
		t.Fatalf("list should be newest first, got %q", list[0].ID)
	}
	if list[2].ID != older.ID {
		t.Fatalf("list should end with the oldest task, got %q", list[2].ID)
	}
}

func TestMemStoreListActive(t *testing.T) {
	store := NewMemStore()
	base := time.Now()
	older := New("活动一", "s1", "u1", "c1", "qq")
	older.CreatedAt = base.Add(-time.Minute)
	newer := New("活动二", "s2", "u1", "c1", "qq")
	newer.CreatedAt = base
	done := New("已完成", "s1", "u1", "c1", "qq")
	if err := done.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	for _, tk := range []*Task{older, newer, done} {
		if err := store.Save(tk); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 || active[0].ID != older.ID || active[1].ID != newer.ID {
		t.Fatalf("ListActive = %+v, want oldest first and terminal excluded", active)
	}
}

func TestNoopStore(t *testing.T) {
	var store NoopStore
	if err := store.Save(New("目标", "s", "u", "c", "p")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	if _, err := store.ActiveBySession("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ActiveBySession = %v, want ErrNotFound", err)
	}
	list, err := store.ListBySession("x")
	if err != nil || len(list) != 0 {
		t.Fatalf("ListBySession = (%v, %v)", list, err)
	}
	active, err := store.ListActive()
	if err != nil || len(active) != 0 {
		t.Fatalf("ListActive = (%v, %v)", active, err)
	}
}

func TestRecordToTaskCorruptedSteps(t *testing.T) {
	rec := &Record{ID: "x", Status: string(StatusRunning), Steps: "{not json"}
	if _, err := rec.ToTask(); err == nil || !strings.Contains(err.Error(), "steps") {
		t.Fatalf("corrupted steps error = %v", err)
	}
}
