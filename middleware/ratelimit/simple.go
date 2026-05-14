package ratelimit

import (
	"context"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// ManagedAdaptiveRateLimiter 封装了 AdaptiveRateLimiter，提供 Stop 机制。
// 通过此类型使用时，调用方可以在应用退出时调用 Stop() 释放后台 goroutine。
type ManagedAdaptiveRateLimiter struct {
	arl *AdaptiveRateLimiter
}

// Middleware 返回中间件函数
func (m *ManagedAdaptiveRateLimiter) Middleware() eventctx.Middleware {
	return m.arl.Middleware()
}

// Stop 停止后台 goroutine（adjustLoop 和 metricsLoop）
func (m *ManagedAdaptiveRateLimiter) Stop() {
	m.arl.Stop()
}

// NewManagedAdaptive 创建一个可管理的自适应限流器（推荐使用）
//
// 与 SimpleAdaptive 不同，此函数返回 *ManagedAdaptiveRateLimiter，
// 调用方应在应用退出时调用 Stop() 来释放后台 goroutine。
//
// 使用示例:
//
//	managed := ratelimit.NewManagedAdaptive()
//	engine.Use(managed.Middleware())
//	defer managed.Stop()
func NewManagedAdaptive() *ManagedAdaptiveRateLimiter {
	return NewManagedAdaptiveWithContext(context.Background())
}

// NewManagedAdaptiveWithContext 创建与外部 context 联动的可管理限流器。
//
// 当 parent ctx 被取消时（如 Bot 关闭），后台 goroutine 自动退出，无需手动调用 Stop()。
//
// 推荐与 Bot 根 context 配合使用：
//
//	managed := ratelimit.NewManagedAdaptiveWithContext(bot.Context())
//	engine.Use(managed.Middleware())
func NewManagedAdaptiveWithContext(parent context.Context) *ManagedAdaptiveRateLimiter {
	arl := NewAdaptiveRateLimiterWithContext(parent, DefaultAdaptiveConfig())
	arl.Start()
	return &ManagedAdaptiveRateLimiter{arl: arl}
}

// NewManagedAdaptiveWithLimit 创建带自定义并发限制的可管理限流器
func NewManagedAdaptiveWithLimit(maxConcurrency int) *ManagedAdaptiveRateLimiter {
	return NewManagedAdaptiveWithLimitContext(context.Background(), maxConcurrency)
}

// NewManagedAdaptiveWithLimitContext 创建带自定义并发限制且与外部 context 联动的限流器
func NewManagedAdaptiveWithLimitContext(parent context.Context, maxConcurrency int) *ManagedAdaptiveRateLimiter {
	config := DefaultAdaptiveConfig()
	config.MaxConcurrency = maxConcurrency
	config.InitialLimit = maxConcurrency / 2
	config.MinConcurrency = maxConcurrency / 10
	arl := NewAdaptiveRateLimiterWithContext(parent, config)
	arl.Start()
	return &ManagedAdaptiveRateLimiter{arl: arl}
}

// SimpleAdaptive 创建带默认配置的自适应限流器中间件
//
// 注意：此函数仅返回中间件函数，后台 goroutine 无法被停止。
// 在需要优雅关闭的场景，请改用 NewManagedAdaptive()。
//
// 使用示例:
//
//	engine.Use(ratelimit.SimpleAdaptive())
func SimpleAdaptive() eventctx.Middleware {
	return NewManagedAdaptive().Middleware()
}

// SimpleAdaptiveWithLimit 创建指定并发限制的自适应限流器
//
// 注意：此函数仅返回中间件函数，后台 goroutine 无法被停止。
// 在需要优雅关闭的场景，请改用 NewManagedAdaptiveWithLimit()。
//
// 使用示例:
//
//	engine.Use(ratelimit.SimpleAdaptiveWithLimit(200))
func SimpleAdaptiveWithLimit(maxConcurrency int) eventctx.Middleware {
	return NewManagedAdaptiveWithLimit(maxConcurrency).Middleware()
}
