package storage

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// NewMemory 创建一个基于 SQLite 内存数据库的存储客户端。
//
// 适用于单元测试或不需要持久化的轻量场景。
// 每次调用返回独立的内存数据库实例，互不干扰。
//
// 注意：内存数据库在进程退出后数据丢失。
// 多个连接共享同一内存数据库时，请使用带命名的 DSN:
//
//	storage.NewMemory() // ":memory:" 每次新建独立实例
//
// 示例（在测试中）：
//
//	client := storage.NewMemory()
//	client.AutoMigrate(&MyModel{})
//	client.Create(&MyModel{...})
func NewMemory() (*Plugin, error) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: open in-memory db failed: %w", err)
	}
	return newPlugin(db), nil
}
