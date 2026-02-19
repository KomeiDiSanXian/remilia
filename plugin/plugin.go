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
//	    return &plugin.PluginDescriptor{
//	        Name:    "myplugin",
//	        Version: "1.0.0",
//	        Deps:    []string{"cache"},
//	        Setup: func(ctx *plugin.SetupContext) error {
//	            cache := ctx.MustGet("cache")
//	            // 初始化逻辑...
//	            return nil
//	        },
//	    }
//	}
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

// Metadata 插件元数据
type Metadata struct {
	// 基本信息
	Name        string // 插件名称
	Version     string // 版本号
	Author      string // 作者
	Description string // 描述
	HelpText    string // 帮助文本

	// 分类和标签
	Category string   // 分类（如 "管理"、"娱乐"、"工具"）
	Tags     []string // 标签

	// 依赖信息
	Dependencies []string // 依赖的插件列表

	// 可见性
	Hidden bool // 是否在帮助中隐藏

	// 联系方式（保留用于兼容性）
	Homepage   string // 主页
	Repository string // 仓库地址
}

// Plugin 插件接口
// 所有插件必须实现此接口的基本方法
//
// 注意: 推荐使用 v2 API (PluginDescriptor) 而不是直接实现此接口
type Plugin interface {
	// Name 返回插件名称
	Name() string

	// Load 加载插件
	// 在此方法中应该注册事件处理器、初始化资源等
	Load(coordinator *engine.Engine) error

	// Unload 卸载插件
	// 在此方法中应该清理资源、移除事件处理器等
	Unload(coordinator *engine.Engine) error

	// Reload 重新加载插件（热重载）
	Reload(coordinator *engine.Engine) error

	// Dependencies 返回插件的依赖列表
	Dependencies() []string
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

// StatefulPlugin 有状态插件接口（可选实现）
// 实现此接口的插件支持状态查询
type StatefulPlugin interface {
	// GetState 获取插件状态
	GetState() State

	// SetState 设置插件状态（由 Manager 调用）
	SetState(state State)

	// GetLoadTime 获取加载时间
	GetLoadTime() time.Time

	// SetLoadTime 设置加载时间（由 Manager 调用）
	SetLoadTime(t time.Time)

	// GetLastError 获取最后的错误
	GetLastError() error

	// SetLastError 设置最后的错误（由 Manager 调用）
	SetLastError(err error)

	// GetUptime 获取运行时长
	GetUptime() time.Duration
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
//
// 请使用 v2 API (PluginDescriptor) 替代。
// 迁移指南: docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md
