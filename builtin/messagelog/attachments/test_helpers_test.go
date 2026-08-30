package attachments

import (
	"bytes"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newBytesReader 返回 bytes.Reader（测试辅助）。
func newBytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}

// newTestDB 创建内存 SQLite 测试库并迁移附件表。
func newTestDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Row{}); err != nil {
		t.Fatal(err)
	}
	// 与生产 OpenDB 一致的写锁等待策略：并发 GC 持写锁期间等待而非 SQLITE_BUSY
	db.Exec("PRAGMA busy_timeout = 5000")
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// insertRow 插入一行附件并返回自增 ID。
func insertRow(t *testing.T, db *gorm.DB, row Row) int64 {
	t.Helper()
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}
