package main

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { closeDB(db) })
	return db
}

// TestAISessionRename 覆盖 records → ai_session_records 的改名、旧索引清理与幂等。
func TestAISessionRename(t *testing.T) {
	db := newTestDB(t)

	stmts := []string{
		`CREATE TABLE records (id text PRIMARY KEY, user_id text, chat_id text, session_json text)`,
		`CREATE INDEX idx_records_user_id ON records(user_id)`,
		`CREATE INDEX idx_records_chat_id ON records(chat_id)`,
		`INSERT INTO records (id, user_id, chat_id, session_json) VALUES ('s1', 'u1', 'c1', '{}')`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	if !hasWork(db, jobAISession) {
		t.Fatal("legacy records table should be detected as work")
	}
	if err := runAISession(db, true); err != nil {
		t.Fatalf("run: %v", err)
	}

	if tableExists(db, "records") {
		t.Fatal("records must be renamed away")
	}
	if !tableExists(db, "ai_session_records") {
		t.Fatal("ai_session_records must exist")
	}
	var count int
	if err := db.Raw("SELECT count(*) FROM ai_session_records").Row().Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows = %d, want 1 (rename must not copy or drop data)", count)
	}
	for _, idx := range []string{"idx_records_user_id", "idx_records_chat_id"} {
		var n int
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", idx).Row().Scan(&n); err != nil {
			t.Fatalf("index lookup: %v", err)
		}
		if n != 0 {
			t.Fatalf("legacy index %s still present", idx)
		}
	}
	if hasWork(db, jobAISession) {
		t.Fatal("migration must be idempotent")
	}
}

// TestAISessionSkipsExistingTarget 目标表已存在时不得覆盖。
func TestAISessionSkipsExistingTarget(t *testing.T) {
	db := newTestDB(t)
	for _, s := range []string{
		`CREATE TABLE records (id text PRIMARY KEY)`,
		`CREATE TABLE ai_session_records (id text PRIMARY KEY)`,
	} {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if hasWork(db, jobAISession) {
		t.Fatal("existing target table must suppress the rename")
	}
	if err := runAISession(db, true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !tableExists(db, "records") {
		t.Fatal("records must be left untouched when target exists")
	}
}

// TestMessagelogWorkDetection 只读判断需不需要 messagelog 迁移。
func TestMessagelogWorkDetection(t *testing.T) {
	db := newTestDB(t)

	if hasWork(db, jobMessagelog) {
		t.Fatal("empty db must not require messagelog migration")
	}

	if err := db.Exec(`CREATE TABLE message_records (id integer PRIMARY KEY AUTOINCREMENT, event_id text)`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if !hasWork(db, jobMessagelog) {
		t.Fatal("legacy messagelog schema (user_version=0) should require migration")
	}
	if err := db.Exec("PRAGMA user_version = 2").Error; err != nil {
		t.Fatalf("set version: %v", err)
	}
	if hasWork(db, jobMessagelog) {
		t.Fatal("user_version=2 must not require migration")
	}
}
