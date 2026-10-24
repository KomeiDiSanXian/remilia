package storage

import (
	"fmt"
	"strings"

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
		dialector = sqlite.Open(withSQLiteConnectionPragmas(o.dsn))
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q (import the corresponding gorm driver and use WithDialector)", o.driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if o.driver == DriverSQLite {
		applySQLitePragmas(db)
	}
	return newPlugin(db), nil
}

// withSQLiteConnectionPragmas 把"连接级" PRAGMA 交给驱动，在每条连接建立时执行。
//
// PRAGMA 分两类：journal_mode 是库级设置（设一次长期生效），synchronous 是
// 连接级设置——用 db.Exec 执行只会改到连接池里的某一条连接，之后新建的连接
// 仍按默认值（FULL），持久化/性能策略因此随机生效。走驱动的 _pragma 参数
// 才能保证每条连接一致。
//
// busy_timeout 不需要显式设置：glebarez/go-sqlite 为每条连接默认设为 5000ms
// （applyQueryParams），SQLite 的写锁竞争已有等待而非立即 SQLITE_BUSY。
// 调用方自己配置过 _pragma 时不介入，避免覆盖其对持久化的显式选择。
func withSQLiteConnectionPragmas(dsn string) string {
	if strings.Contains(dsn, "_pragma=") {
		return dsn
	}
	sep := "?"
	if strings.ContainsRune(dsn, '?') {
		sep = "&"
	}
	return dsn + sep + "_pragma=synchronous(NORMAL)"
}

// applySQLitePragmas 设置库级 PRAGMA 与连接数上限。
//
// 连接数上限 5：WAL 允许多读者 + 单写者，连接开太多只会增加写锁竞争与
// 文件句柄占用，与 messagelog 的取值保持一致。
func applySQLitePragmas(db *gorm.DB) {
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA auto_vacuum = INCREMENTAL")

	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
}
