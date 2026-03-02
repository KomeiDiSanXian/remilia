package engine

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

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
	InvalidateSortedCache(eventType dto.EventType)
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

// Adapter connects an event source to the Bot
type Adapter interface {
	Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
	Stop(ctx stdctx.Context) error
}
