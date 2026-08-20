// Package storage 提供存储基础设施的插件系统集成适配器。
//
// 核心存储抽象（Client 接口、Plugin 结构体等）位于 infra/storage。
// 本包负责将 infra/storage 包装为 plugin.Descriptor，
// 使其能参与插件生命周期管理（Setup/Teardown）和依赖注入。
//
// # 使用方式
//
//	import (
//	    builtin_storage "github.com/KomeiDiSanXian/remilia/builtin/storage"
//	    "github.com/KomeiDiSanXian/remilia/infra/storage"
//	)
//
//	// 注册存储插件（main 或 bot setup 时）
//	pm.Register(builtin_storage.New(storage.WithDSN("data/bot.db")))
//
//	// 在其他插件中获取存储客户端（面向接口）
//	clientProxy := ctx.Service[storage.Client]("storage")
//	clientProxy.Must().AutoMigrate(&MyModel{})
//	clientProxy.Must().Create(&MyModel{Name: "test"})
package storage

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// New 创建存储插件描述符。
//
// 默认使用 SQLite，DSN 为 "bot.db"。
// 注册后，其他插件通过以下方式获取：
//
//	// 接口方式（基础 CRUD）
//	clientProxy := ctx.Service[storage.Client]("storage")
//
//	// 具体类型方式（需要链式查询或 GORM 高级特性时）
//	p := ctx.Service[*storage.Plugin]("storage")
//	p.Must().Where("...").First(...)
//	p.Must().DB().Transaction(...)
//
// 示例：
//
//	pm.Register(storage.New(
//	    infrastorage.WithDSN("data/bot.db"),
//	))
func New(opts ...infrastorage.Option) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "storage",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "持久化存储插件，基于 GORM，默认使用 SQLite",
			Category:    "基础设施",
			Tags:        []string{"存储", "数据库", "持久化", "gorm", "sqlite"},
			HelpText: `存储插件无需手动使用。其他插件通过依赖注入获取：
  clientProxy := ctx.Service[storage.Client]("storage")
  clientProxy.Must().AutoMigrate(&MyModel{})
  clientProxy.Must().Create(&MyModel{...})`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if ctx.DryRun {
				// DryRun 阶段不真实打开数据库，返回空壳避免副作用
				return &infrastorage.Plugin{}, nil
			}
			p, err := infrastorage.Open(opts...)
			if err != nil {
				return nil, fmt.Errorf("storage: open db failed: %w", err)
			}
			ctx.Log.Info("Database connected")
			// 额外以接口类型导出，供消费者通过 ctx.Service[storage.Client] 获取
			ctx.ExportIface[infrastorage.Client]("storage", p)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			if ctx.API == nil {
				return nil
			}
			p, ok := ctx.API.(*infrastorage.Plugin)
			if !ok || p.DB() == nil {
				return nil
			}
			sqlDB, err := p.DB().DB()
			if err != nil {
				logger.WithError(err).Warn("[storage] Failed to get underlying sql.DB for close")
				return nil
			}
			if err := sqlDB.Close(); err != nil {
				return fmt.Errorf("storage: close db failed: %w", err)
			}
			ctx.Log.Info("Database connection closed")
			return nil
		},
	}
}
