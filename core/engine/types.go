package engine

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
)

// EventType 平台无关的事件类型标识。
//
// 使用 string 的类型别名，与 dto.EventType（同为 string 别名）完全兼容，
// 允许新平台无需导入 openapi/dto 即可注册事件匹配器。
//
// QQ 平台常量（dto.C2CMessageCreate 等）可直接传入，无需类型转换。
type EventType = string

// Option Engine 配置选项函数类型
type Option func(*Engine)

// MatcherOption Matcher 配置选项函数类型
type MatcherOption func(*Matcher)

// MatcherLifecycle 定义 Matcher 核心生命周期操作（高频调用路径）。
//
// 这 4 个方法是 Matcher 日常操作（删除、处理器重建、缓存失效、命令缓存更新）的最小集合。
// 为 Matcher 单元测试提供更小的 mock 接口。
type MatcherLifecycle interface {
	// DeleteMatcher 将 Matcher 加入批量删除队列
	DeleteMatcher(m *Matcher)
	// RebuildMatcherChain 重建 Matcher 的中间件链缓存（Handler 变更时调用）
	RebuildMatcherChain(m *Matcher)
	// InvalidateSortedCache 使指定事件类型的已排序 Matcher 缓存失效
	InvalidateSortedCache(eventType EventType)
	// UpdateCommandCache 更新命令索引缓存
	UpdateCommandCache(m *Matcher)
}

// MatcherMigration 定义临时 Matcher 迁移操作（低频，仅在 SetTemp/SetTempWithMaxUse 时调用）。
//
// 与 MatcherLifecycle 分离，允许调用方按需 mock 迁移操作，而不必实现全部 8 个方法。
type MatcherMigration interface {
	// UpdateTempMatcherPriority 更新 TempManager 中 Matcher 的优先级
	UpdateTempMatcherPriority(m *Matcher)
	// UpdateMatcherIndex 强制更新匹配器的索引（命令 matcher 优先级变更时调用）
	UpdateMatcherIndex(m *Matcher)
	// UpdateMatcherCommand 更新 Matcher 的命令绑定
	UpdateMatcherCommand(m *Matcher)
	// MigrateMatcherToTemp 将 Matcher 从永久状态迁移到 TempManager
	MigrateMatcherToTemp(m *Matcher)
	// MigrateMatcherFromTemp 将 Matcher 从 TempManager 迁回永久状态
	MigrateMatcherFromTemp(m *Matcher)
}

// MatcherCoordinator 是 Matcher 对 Engine 的完整依赖接口。
//
// 组合 MatcherLifecycle（核心，4方法）+ MatcherMigration（迁移，4方法）。
//
// 测试建议：
//   - 仅测试核心流程时，实现 MatcherLifecycle 即可（4个方法）。
//   - 测试临时 Matcher 迁移时，额外实现 MatcherMigration（4个方法）。
//   - 完整集成测试使用 *Engine（已实现全部 8 个方法）。
type MatcherCoordinator interface {
	MatcherLifecycle
	MatcherMigration
}

// MatcherWriter 是 Engine 的 Matcher 注册接口。
//
// 插件系统中 liveRegistryWriter 仅依赖此接口（3 个方法），
// 无需 mock 完整的 PluginCoordinator（10 个方法）。
type MatcherWriter interface {
	// On 注册一个新的事件匹配器
	On(eventType EventType, rules ...context.Rule) *Matcher
	// OnCommand 注册一个命令匹配器（自动开启 O(1) 分发优化）
	OnCommand(eventType EventType, cmdPattern string, extraRules ...context.Rule) *Matcher
	// SetMatcherGroup 将已注册的 Matcher 划入指定分组并更新来源标签，同时维护 Engine 内部的 groupIndex。
	// 这是注册 Matcher 后设置分组的首选方法；与 Matcher.SetGroup 的区别在于此方法会同步更新
	// groupIndex，确保 RemoveGroup / DisableGroup / EnableGroup 能正确找到该 Matcher。
	SetMatcherGroup(m *Matcher, group, source string)
}

// GroupWriter 是 Engine 的插件分组管理接口。
//
// Instance.unload 仅依赖此接口中的 RemoveGroup，Manager 依赖完整分组管理。
type GroupWriter interface {
	// RemoveGroup 删除指定分组的所有 Matcher
	RemoveGroup(groupName string)
	// DisableGroup 禁用指定分组（暂停事件分发）
	DisableGroup(groupName string)
	// EnableGroup 启用指定分组（恢复事件分发）
	EnableGroup(groupName string)
}

// PluginCoordinator 是插件系统对 Engine 的完整依赖接口。
//
// 组合 MatcherWriter（3 方法）+ GroupWriter（3 方法）+ Reader（8 方法），
// 共 10 个方法。按职责拆分后，调用方可按需依赖更小的子接口：
//   - liveRegistryWriter → MatcherWriter（3 方法）
//   - Instance.unload    → GroupWriter（仅使用 RemoveGroup, 1 方法）
//   - Manager            → GroupWriter（4 方法）或 PluginCoordinator（完整）
//   - plugin_info.go     → Reader（8 方法，已通过 NewEngineReader 包装）
//
// *Engine 已同时实现上述所有接口，调用方零修改。
type PluginCoordinator interface {
	MatcherWriter
	GroupWriter
	Reader
}
