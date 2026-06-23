package storage

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Open 根据选项打开数据库，返回 Plugin 实例。
//
// 用于在插件系统之外直接使用存储，或由上层适配器调用（如 builtin/storage）。
// 调用方负责在不再使用时关闭连接：
//
//	p, err := storage.Open(storage.WithDSN("bot.db"))
//	if err != nil { ... }
//	sqlDB, _ := p.DB().DB()
//	defer sqlDB.Close()
func Open(opts ...Option) (*Plugin, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}
	return openDB(o)
}

// openDB 根据配置打开数据库连接并返回 Plugin（包内使用）。
func openDB(o *options) (*Plugin, error) {
	var dialector gorm.Dialector
	switch o.driver {
	case DriverSQLite:
		dialector = sqlite.Open(o.dsn)
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q (import the corresponding gorm driver and use WithDialector)", o.driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	return newPlugin(db), nil
}
