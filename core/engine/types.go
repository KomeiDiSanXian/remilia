package engine

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
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

// Middleware 中间件函数类型
type Middleware = context.Middleware

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

// PluginCoordinator 是插件系统对 Engine 的完整依赖接口。
//
// 包含插件生命周期管理所需的全部 Engine 操作：
//   - Matcher 注册（On / OnCommand）
//   - 分组管理（RemoveGroup / DisableGroup / EnableGroup）
//   - 只读查询（嵌入 EngineReader，提供命令查询、Matcher 统计等）
//
// 使用此接口而非 *Engine 具体类型，可以：
//  1. 在不引入完整 engine 包的情况下使用插件系统（轻量嵌入）
//  2. 在单元测试中 mock Engine（避免集成测试的依赖）
//  3. 遵循依赖倒置原则（plugin 依赖抽象，不依赖具体实现）
//
// *Engine 已实现该接口的全部方法，调用方无需任何修改。
type PluginCoordinator interface {
	EngineReader
	// On 注册一个新的事件匹配器
	On(eventType EventType, rules ...context.Rule) *Matcher
	// OnCommand 注册一个命令匹配器（自动开启 O(1) 分发优化）
	OnCommand(eventType EventType, cmdPattern string, extraRules ...context.Rule) *Matcher
	// RemoveGroup 删除指定分组的所有 Matcher
	RemoveGroup(groupName string)
	// DisableGroup 禁用指定分组（暂停事件分发）
	DisableGroup(groupName string)
	// EnableGroup 启用指定分组（恢复事件分发）
	EnableGroup(groupName string)
}

// PlatformAdapter 是平台无关的适配器接口，取代 Adapter。
//
// StartPlatform 的 handler 接受 platform.Event，不依赖任何特定平台的数据结构，
// 使同一个 Bot 可以同时处理 QQ、Discord、Telegram 等多个平台的事件。
//
// 实现示例参见 platform/qq.Adapter 和根包的 WebhookServerAdapter。
type PlatformAdapter interface {
	// Platform 返回平台标识符（如 "qq"、"discord"）
	Platform() string
	// StartPlatform 启动事件接收循环，将事件以 platform.Event 形式回调给 handler
	StartPlatform(ctx stdctx.Context, handler func(platform.Event)) error
	// Stop 优雅停止
	Stop(ctx stdctx.Context) error
	// Sender 返回该平台的消息发送器
	Sender() platform.Sender
}
