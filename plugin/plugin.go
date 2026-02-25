// Package plugin 提供插件系统的核心接口和实现。
//
// # 使用 v2 API
//
// 插件应该使用 v2 API (PluginDescriptor)，它提供：
//   - 函数式设计，无需继承
//   - 自动依赖注入
//   - 更简洁的代码（减少 60% 样板代码）
//   - 完整的生命周期管理
//
// v2 示例：
//
//	func New() *plugin.PluginDescriptor {
//	    p := &MyPlugin{}
//	    return &plugin.PluginDescriptor{
//	        Name:    "myplugin",
//	        Version: "1.0.0",
//	        Deps:    []string{"cache"},
//	        Setup: func(ctx *plugin.SetupContext) (any, error) {
//	            p.cache = plugin.Must[cache.Plugin](ctx, "cache")
//	            ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/hello").Handle(p.handleHello)
//	            return p, nil // 框架自动以插件名注册到容器
//	        },
//	        Teardown: func(ctx *plugin.TeardownContext) error {
//	            ctx.Log.Info("plugin stopped")
//	            return nil
//	        },
//	    }
//	}
//
// # 公开类型说明
//
//   - [PluginDescriptor] — 插件定义（开发者使用）
//   - [PluginInstance]   — 插件运行时实例（Manager 返回）
//   - [Manager]          — 插件生命周期管理器
//
// # v1 API 已移除
//
// BasePlugin 和相关的 v1 API 已在 v2.0.0 中移除。
// 请使用 v2 API (PluginDescriptor) 替代。
//
// 迁移指南: docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md
package plugin

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// Metadata 插件元数据（统一结构）
//
// PluginDescriptor.Meta 字段使用此类型，Manager.GetMetadata 也返回此类型。
// 两者共用同一结构，消除了之前 Metadata 与 PluginMeta 之间的字段拷贝开销。
type Metadata struct {
	// --- 注册标识（框架在注册时自动填充，开发者无需手动设置）---
	Name         string   // 插件名称
	Version      string   // 版本号
	Dependencies []string // 依赖的插件列表

	// --- 显示信息（开发者在 PluginDescriptor.Meta 中填写）---
	Author      string   // 作者
	Description string   // 描述
	HelpText    string   // 帮助文本
	Category    string   // 分类（如 "管理"、"娱乐"、"工具"）
	Tags        []string // 标签
	Hidden      bool     // 是否在帮助中隐藏

	// --- 扩展信息（可选）---
	Homepage   string // 主页
	Repository string // 仓库地址
}

// PluginMeta 是 Metadata 的类型别名，用于 PluginDescriptor.Meta 字段。
//
// 两者完全相同，PluginMeta 仅作为语义上更清晰的名称供插件开发者使用：
//
//	Meta: &plugin.PluginMeta{
//	    Author:      "Team",
//	    Description: "My plugin",
//	    Category:    "core",
//	}
type PluginMeta = Metadata

// pluginInternal 插件内部接口（包私有）
//
// 仅供 Manager 内部使用，统一驱动 PluginInstance 的生命周期。
// 外部代码应使用 [PluginDescriptor] 定义插件，通过 [PluginInstance] 操作运行时实例。
type pluginInternal interface {
	// name 返回插件名称
	name() string

	// load 加载插件
	load(coordinator *engine.Engine) error

	// unload 卸载插件
	unload(coordinator *engine.Engine) error

	// reload 重新加载插件
	reload(coordinator *engine.Engine) error

	// dependencies 返回插件的依赖列表
	dependencies() []string
}

// MetadataProvider 插件元数据提供者接口（可选实现）
// 插件可以实现此接口来提供详细的元数据信息
type MetadataProvider interface {
	// Metadata 返回插件的元数据
	Metadata() *Metadata
}

// ConfigurablePlugin 可配置插件接口（可选实现）
// 实现此接口的插件支持配置管理
type ConfigurablePlugin interface {
	// GetConfig 获取插件配置
	GetConfig() Config

	// SetConfig 设置插件配置（由 Manager 调用）
	SetConfig(config Config)
}

// StatefulPlugin 有状态插件只读接口（可选实现）
// 插件消费者通过此接口查询插件运行状态，无法通过此接口修改状态。
// Manager 内部通过 statefulPluginWriter 接口进行写操作。
type StatefulPlugin interface {
	// GetState 获取插件状态
	GetState() State

	// GetLoadTime 获取加载时间
	GetLoadTime() time.Time

	// GetLastError 获取最后的错误
	GetLastError() error

	// GetUptime 获取运行时长
	GetUptime() time.Duration
}

// statefulPluginWriter 有状态插件写接口（仅 Manager 包内使用）
// 通过包级私有接口限制写方法只能由 Manager 调用，防止外部代码误修改插件状态。
type statefulPluginWriter interface {
	StatefulPlugin
	// SetState 设置插件状态（由 Manager 调用）
	SetState(state State)
	// SetLoadTime 设置加载时间（由 Manager 调用）
	SetLoadTime(t time.Time)
	// SetLastError 设置最后的错误（由 Manager 调用）
	SetLastError(err error)
}

// MatcherProvider 提供 Matcher 的插件接口（可选实现）
// 实现此接口的插件可以查询其注册的 Matcher
type MatcherProvider interface {
	// GetMatchers 获取插件注册的所有 Matcher
	GetMatchers() []*engine.Matcher
}

// --- v1 API 已在 v2.0.0 中移除 ---
//
// 以下 v1 API 已被移除:
//   - type BasePlugin
//   - func NewBasePlugin(name string) *BasePlugin
//   - func NewBasePluginWithMetadata(metadata *Metadata) *BasePlugin
//   - type EventAwarePlugin (v1 事件总线功能)
//   - type Plugin (公开接口，已改为包内私有 pluginInternal)
//
// 请使用 v2 API (PluginDescriptor) 替代。
// 迁移指南: docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md
