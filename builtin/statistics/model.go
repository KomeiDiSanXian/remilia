package statistics

import (
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"
)

// WordStat 词频统计（派生数据：可从 messagelog 全量重建）。
type WordStat struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Word      string `gorm:"not null;uniqueIndex:idx_word_chat"`
	ChatID    string `gorm:"not null;uniqueIndex:idx_word_chat"`
	Count     int
	UpdatedAt int64
}

// TableName 固定表名，避免 GORM 复数化。
func (WordStat) TableName() string { return "word_stats" }

// DailyMessageStat 每日消息量（按会话/日期/方向聚合）。
type DailyMessageStat struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	ChatID     string `gorm:"not null;uniqueIndex:idx_daily_chat_day_dir"`
	Day        string `gorm:"not null;uniqueIndex:idx_daily_chat_day_dir"` // YYYY-MM-DD（UTC）
	IsOutbound bool   `gorm:"uniqueIndex:idx_daily_chat_day_dir"`
	Count      int
}

// TableName 固定表名。
func (DailyMessageStat) TableName() string { return "daily_message_stats" }

// UserMessageStat 用户消息量（按会话/用户/日期聚合）。
type UserMessageStat struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	ChatID     string `gorm:"not null;uniqueIndex:idx_user_chat_user_day"`
	UserID     string `gorm:"not null;uniqueIndex:idx_user_chat_user_day"`
	Day        string `gorm:"not null;uniqueIndex:idx_user_chat_user_day"`
	IsOutbound bool
	Count      int
}

// TableName 固定表名。
func (UserMessageStat) TableName() string { return "user_message_stats" }

// ChatMessageStat 会话消息量（按会话/日期聚合，入站+出站）。
type ChatMessageStat struct {
	ID     int64  `gorm:"primaryKey;autoIncrement"`
	ChatID string `gorm:"not null;uniqueIndex:idx_chat_chat_day"`
	Day    string `gorm:"not null;uniqueIndex:idx_chat_chat_day"`
	Count  int
}

// TableName 固定表名。
func (ChatMessageStat) TableName() string { return "chat_message_stats" }

// OpenDB 打开或创建派生数据 SQLite 数据库（与 messagelog 事实库完全隔离）。
func OpenDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA synchronous = NORMAL")
	db.Exec("PRAGMA busy_timeout = 5000")

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	if err := db.AutoMigrate(&WordStat{}, &DailyMessageStat{}, &UserMessageStat{}, &ChatMessageStat{}); err != nil {
		return nil, err
	}
	return db, nil
}
