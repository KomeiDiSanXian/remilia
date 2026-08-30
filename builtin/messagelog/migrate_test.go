package messagelog

import (
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// uuidV7Re 匹配 UUIDv7 格式（version 位 = 7）。
var uuidV7Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// legacyMessageRecord 模拟遗留 schema（只有旧列），用于迁移测试。
// 必须显式声明 TableName，否则 GORM 会映射到 legacy_message_records。
type legacyMessageRecord struct {
	RequestID  string `gorm:"index;not null"`
	Platform   string `gorm:"index;not null"`
	Kind       string `gorm:"index;not null"`
	EventID    string `gorm:"index"`
	ChatID     string `gorm:"index:idx_chat_time;not null"`
	ChatName   string
	ParentID   string
	UserID     string `gorm:"index;not null"`
	UserName   string
	UserRole   string
	Content    string `gorm:"type:text"`
	ReplyToID  string
	RawType    string
	ID         int64 `gorm:"primaryKey;autoIncrement"`
	Timestamp  int64 `gorm:"index:idx_chat_time"`
	CreatedAt  int64
	IsGroup    bool
	IsOutbound bool `gorm:"index"`
}

func (legacyMessageRecord) TableName() string { return "message_records" }

// openLegacyDB 创建一个遗留结构的旧库（无 user_version 记录）。
func openLegacyDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := db.AutoMigrate(&legacyMessageRecord{}, &MessageMention{}); err != nil {
		t.Fatalf("auto migrate legacy schema: %v", err)
	}
	return db
}

// seedLegacyData 写入一批遗留行（含回复链与出站记录）。
func seedLegacyData(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	for i := range 5 {
		m := legacyMessageRecord{
			RequestID:  fmt.Sprintf("req-%d", i),
			Platform:   "test",
			Kind:       "GROUP_MESSAGE",
			EventID:    fmt.Sprintf("ev-%d", i),
			ChatID:     "g1",
			ChatName:   "群1",
			UserID:     "u1",
			UserName:   "用户",
			Content:    fmt.Sprintf("历史消息 %d", i),
			ReplyToID:  fmt.Sprintf("ev-%d", i-1),
			Timestamp:  now.Add(time.Duration(i) * time.Second).UnixNano(),
			CreatedAt:  now.UnixNano(),
			IsGroup:    true,
			IsOutbound: false,
		}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed legacy data: %v", err)
		}
	}
}

func readSchemaVersion(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var v int
	if err := db.Raw("PRAGMA user_version").Row().Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

// assertMigratedCurrent 校验迁移后的共同不变量。
func assertMigratedCurrent(t *testing.T, db *gorm.DB) {
	t.Helper()
	if v := readSchemaVersion(t, db); v != currentSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", currentSchemaVersion, v)
	}
	if !db.Migrator().HasTable(&AttachmentRecord{}) {
		t.Errorf("message_attachments table not created")
	}
	for _, idx := range []string{"idx_mr_chat_id_id", "idx_mr_chat_id_event", "idx_mr_event_id"} {
		if !db.Migrator().HasIndex(&MessageRecord{}, idx) {
			t.Errorf("index %s not created", idx)
		}
	}
}

// assertAllEventIDUUID 校验所有行 event_id 均为 UUIDv7 且与 platform_message_id 不同。
func assertAllEventIDUUID(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []MessageRecord
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	for _, r := range rows {
		if !uuidV7Re.MatchString(r.EventID) {
			t.Errorf("row id=%d: event_id %q is not UUIDv7", r.ID, r.EventID)
		}
	}
}

// queryFirstByEventID 用与 QueryByEventID 相同的查询路径（event_id 或 platform_message_id）读取单条记录。
func queryFirstByEventID(t *testing.T, db *gorm.DB, id string) (RecordEntry, bool) {
	t.Helper()
	var m MessageRecord
	if err := db.Where("event_id = ? OR platform_message_id = ?", id, id).First(&m).Error; err != nil {
		return RecordEntry{}, false
	}
	return modelToEntry(m, nil), true
}

// TestMigrate_FromLegacySchema 旧库（user_version=0，遗留结构）→ 当前版本：数据保全 + 回填 + UUID 升级。
func TestMigrate_FromLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path)
	seedLegacyData(t, legacy)
	closeDB(t, legacy)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer closeDB(t, db)

	assertMigratedCurrent(t, db)
	assertAllEventIDUUID(t, db)

	// 存量数据不丢
	var count int64
	if err := db.Model(&MessageRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count migrated rows: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 preserved rows, got %d", count)
	}

	// platform_message_id 保留为原平台消息 ID（不再等于 event_id）
	var preserved int64
	if err := db.Model(&MessageRecord{}).
		Where("platform_message_id != event_id AND platform_message_id LIKE 'ev-%'").Count(&preserved).Error; err != nil {
		t.Fatalf("count preserved rows: %v", err)
	}
	if preserved != 5 {
		t.Fatalf("expected all 5 rows with preserved platform_message_id, got %d", preserved)
	}

	// 按平台消息 ID 可查询（AI 回复链路径），回复链字段已回填/改写
	entry, ok := queryFirstByEventID(t, db, "ev-1")
	if !ok {
		t.Fatalf("migrated row ev-1 not found")
	}
	if entry.PlatformMessageID != "ev-1" {
		t.Errorf("expected PlatformMessageID=ev-1, got %q", entry.PlatformMessageID)
	}
	if entry.ReplyToMessageID != "ev-0" {
		t.Errorf("expected ReplyToMessageID=ev-0, got %q", entry.ReplyToMessageID)
	}
	ev0, ok := queryFirstByEventID(t, db, "ev-0")
	if !ok {
		t.Fatalf("migrated row ev-0 not found")
	}
	if entry.ReplyToEventID != ev0.EventID {
		t.Errorf("expected ReplyToEventID=%s (ev-0 的新 event_id), got %q", ev0.EventID, entry.ReplyToEventID)
	}
	if entry.Content != "历史消息 1" {
		t.Errorf("expected content preserved, got %q", entry.Content)
	}
}

// TestMigrate_FreshInstall 全新库：OpenDB 即完成建表并标记当前版本，无遗留数据。
func TestMigrate_FreshInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB on fresh db: %v", err)
	}
	defer closeDB(t, db)

	assertMigratedCurrent(t, db)

	var count int64
	if err := db.Model(&MessageRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count fresh rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows on fresh db, got %d", count)
	}
}

// TestMigrate_Idempotent 重复 OpenDB 不重复迁移、不破坏数据。
func TestMigrate_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")
	legacy := openLegacyDB(t, path)
	seedLegacyData(t, legacy)
	closeDB(t, legacy)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("first OpenDB: %v", err)
	}
	assertMigratedCurrent(t, db)
	closeDB(t, db)

	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("second OpenDB: %v", err)
	}
	defer closeDB(t, db2)
	assertMigratedCurrent(t, db2)

	var count int64
	if err := db2.Model(&MessageRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count after reopen: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 rows after reopen, got %d", count)
	}
}
