// Package storage 提供统一的持久化存储抽象层。
//
// 基于 GORM，默认使用 SQLite（需要 CGO 或替换为 glebarez/sqlite 纯 Go 驱动）。
// 也支持 PostgreSQL、MySQL 等任意 GORM 兼容数据库。
//
// # 直接使用（不依赖插件系统）
//
//	p, err := storage.Open(storage.WithDSN("bot.db"))
//	if err != nil { log.Fatal(err) }
//	p.AutoMigrate(&MyModel{})
//	p.Create(&MyModel{Name: "test"})
//
// # 插件系统集成
//
// 如需将存储纳入插件生命周期管理，请使用上层适配器包 builtin/storage：
//
//	import builtin_storage "github.com/KomeiDiSanXian/remilia/builtin/storage"
//	pm.Register(builtin_storage.New(storage.WithDSN("data/bot.db")))
//
//	// 在其他插件中获取存储客户端（面向接口）
//	client := plugin.Service[storage.Client](ctx, "storage")
//	client.Must().AutoMigrate(&MyModel{})
//	client.Must().Create(&MyModel{Name: "test"})
//
//	// 若需要 GORM 高级特性（关联/事务等）
//	db := client.DB()
//	db.Transaction(func(tx *gorm.DB) error { ... })
package storage

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound 记录不存在时返回此错误。
// 对 gorm.ErrRecordNotFound 的统一封装，消费者无需直接依赖 gorm。
var ErrNotFound = errors.New("record not found")

// Client 最小化持久化接口。
//
// *Plugin 实现此接口。消费者通过插件系统或直接传入 *Plugin 使用。
// 链式查询（Where/Order/Limit/Offset）和底层 DB 访问仅由 *Plugin 提供，不属于此接口。
type Client interface {
	// AutoMigrate 自动创建/更新传入模型的表结构（幂等）
	AutoMigrate(dst ...any) error
	// Create 插入一条新记录
	Create(value any) error
	// Save 保存记录（主键存在则 UPDATE，否则 INSERT）
	Save(value any) error
	// First 查询第一条匹配记录并写入 dest（不存在时返回 ErrNotFound）
	First(dest any, conds ...any) error
	// Find 查询所有匹配记录并写入 dest（无记录时返回空切片而非错误）
	Find(dest any, conds ...any) error
	// Delete 删除匹配记录
	Delete(value any, conds ...any) error
	// Updates 更新非零字段（传 struct）或所有字段（传 map）
	Updates(values any) error
}

// Plugin 存储插件 API 对象，同时实现 Client 接口。
//
// 通常通过 plugin.Service[storage.Client](ctx, "storage") 获取为接口类型代理，
// 需要使用 GORM 高级特性时可用 plugin.Service[*storage.Plugin](ctx, "storage") 获取具体指针代理。
type Plugin struct {
	db *gorm.DB
}

// newPlugin 从 *gorm.DB 创建 Plugin 实例（包内使用）
func newPlugin(db *gorm.DB) *Plugin {
	return &Plugin{db: db}
}

// AutoMigrate 自动创建/更新表结构（幂等）
func (p *Plugin) AutoMigrate(dst ...any) error {
	return p.db.AutoMigrate(dst...)
}

// Create 插入一条新记录
func (p *Plugin) Create(value any) error {
	return p.db.Create(value).Error
}

// Save 保存记录（主键存在则 UPDATE，否则 INSERT）
func (p *Plugin) Save(value any) error {
	return p.db.Save(value).Error
}

// First 查询第一条匹配记录并写入 dest
//
// 记录不存在时返回 [ErrNotFound]（已从 gorm.ErrRecordNotFound 转换）。
func (p *Plugin) First(dest any, conds ...any) error {
	err := p.db.First(dest, conds...).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// Find 查询所有匹配记录并写入 dest（无记录时返回空切片，不返回错误）
func (p *Plugin) Find(dest any, conds ...any) error {
	return p.db.Find(dest, conds...).Error
}

// Delete 删除匹配记录
func (p *Plugin) Delete(value any, conds ...any) error {
	return p.db.Delete(value, conds...).Error
}

// Updates 更新非零字段（传 struct）或所有字段（传 map）
func (p *Plugin) Updates(values any) error {
	return p.db.Updates(values).Error
}

// Where 追加 WHERE 条件，返回新的 *Plugin（不修改原 Plugin）
func (p *Plugin) Where(query any, args ...any) *Plugin {
	return &Plugin{db: p.db.Where(query, args...)}
}

// Order 追加 ORDER BY 子句，返回新的 *Plugin
func (p *Plugin) Order(value any) *Plugin {
	return &Plugin{db: p.db.Order(value)}
}

// Limit 限制返回记录数，返回新的 *Plugin
func (p *Plugin) Limit(limit int) *Plugin {
	return &Plugin{db: p.db.Limit(limit)}
}

// Offset 跳过指定数量的记录，返回新的 *Plugin
func (p *Plugin) Offset(offset int) *Plugin {
	return &Plugin{db: p.db.Offset(offset)}
}

// DB 返回底层 *gorm.DB，以便使用 GORM 高级特性
func (p *Plugin) DB() *gorm.DB {
	return p.db
}
