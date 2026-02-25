package middleware

import (
	"context"
	"time"

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
//	managed := middleware.NewManagedAdaptive()
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
//	managed := middleware.NewManagedAdaptiveWithContext(bot.Context())
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
//	engine.Use(middleware.SimpleAdaptive())
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
//	engine.Use(middleware.SimpleAdaptiveWithLimit(200))
func SimpleAdaptiveWithLimit(maxConcurrency int) eventctx.Middleware {
	return NewManagedAdaptiveWithLimit(maxConcurrency).Middleware()
}

// SimpleCircuitBreaker 创建带默认配置的熔断器中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleCircuitBreaker())
func SimpleCircuitBreaker() eventctx.Middleware {
	config := CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
		HalfOpenTimeout:     10 * time.Second,
	}
	cb := NewCircuitBreaker(config)
	return CircuitBreakerMiddleware(cb)
}

// SimpleRateLimit 创建简单固定速率限流中间件（全局共享，无 key 区分）。
//
// # 限流器选择指南
//
// 框架提供两种限流器，请根据场景选择：
//
//   - SimpleRateLimit / RateLimitTokenBucket（令牌桶）
//     适用：已知固定峰值场景（如 "每秒最多 10 条消息"）
//     特点：速率稳定、配置简单、无后台 goroutine
//     参数：perSecond = 每秒允许的最大请求数（burst 自动设为 2 倍）
//
//   - NewManagedAdaptive / NewManagedAdaptiveWithContext（自适应）
//     适用：峰值不确定、需根据 CPU/P99 延迟自动调整并发上限
//     特点：弹性伸缩、无需手动调参，但有后台 goroutine（需 Stop/WithContext）
//
// 经验法则：
//
//	固定场景（如限制某命令调用频率）→ SimpleRateLimit
//	高并发 Bot 保护整体系统负载   →  NewManagedAdaptiveWithContext(bot.Context())
//
// 使用示例:
//
//	// 全局限流：每秒最多处理 10 个事件
//	engine.Use(middleware.SimpleRateLimit(10))
//
//	// 按用户限流（每用户每秒 2 次）
//	engine.Use(middleware.RateLimitTokenBucket(2, 4, func(ctx *context.Context) string {
//	    author := ctx.GetAuthor()
//	    if author == nil { return "" }
//	    return author.UserOpenID
//	}))
func SimpleRateLimit(perSecond float64) eventctx.Middleware {
	if perSecond <= 0 {
		perSecond = 1
	}
	burst := int(perSecond * 2)
	if burst < 1 {
		burst = 1
	}
	return RateLimitTokenBucket(int(perSecond), burst, nil)
}

// SimpleDedup 创建带默认配置的去重中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleDedup())
func SimpleDedup() eventctx.Middleware {
	config := DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: time.Minute,
		StrictMode:      false,
	}
	filter := NewDedupFilter(config)
	return Dedup(filter)
}

// SimpleDedupWithTTL 创建指定TTL的去重中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleDedupWithTTL(5 * time.Minute))
func SimpleDedupWithTTL(ttl time.Duration) eventctx.Middleware {
	config := DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      ttl,
		CleanupInterval: time.Minute,
		StrictMode:      false,
	}
	filter := NewDedupFilter(config)
	return Dedup(filter)
}

// MiddlewareSet 中间件集合，提供常用中间件组合
type MiddlewareSet struct {
	middlewares []eventctx.Middleware
}

// NewMiddlewareSet 创建中间件集合
func NewMiddlewareSet() *MiddlewareSet {
	return &MiddlewareSet{
		middlewares: make([]eventctx.Middleware, 0),
	}
}

// WithLogging 添加日志中间件
func (s *MiddlewareSet) WithLogging() *MiddlewareSet {
	s.middlewares = append(s.middlewares, Logging())
	return s
}

// WithRecover 添加panic恢复中间件
func (s *MiddlewareSet) WithRecover() *MiddlewareSet {
	s.middlewares = append(s.middlewares, Recover())
	return s
}

// WithAdaptive 添加自适应限流中间件
func (s *MiddlewareSet) WithAdaptive() *MiddlewareSet {
	s.middlewares = append(s.middlewares, SimpleAdaptive())
	return s
}

// WithCircuitBreaker 添加熔断器中间件
func (s *MiddlewareSet) WithCircuitBreaker() *MiddlewareSet {
	s.middlewares = append(s.middlewares, SimpleCircuitBreaker())
	return s
}

// WithDedup 添加去重中间件
func (s *MiddlewareSet) WithDedup() *MiddlewareSet {
	s.middlewares = append(s.middlewares, SimpleDedup())
	return s
}

// Build 返回所有中间件
func (s *MiddlewareSet) Build() []eventctx.Middleware {
	return s.middlewares
}

// ProductionSet 返回生产环境推荐的中间件组合
//
// 中间件执行顺序（从外到内）：
//  1. Recover:        panic 恢复（最外层，保证任何 panic 都能被捕获）
//  2. Dedup:          去重过滤（在限流前过滤重复请求，避免浪费配额）
//  3. CircuitBreaker: 熔断器（在限流前熔断，快速失败）
//  4. Adaptive:       自适应限流
//  5. Logging:        日志记录（最内层，记录实际处理的请求）
//
// 使用示例:
//
//	engine.Use(middleware.ProductionSet()...)
func ProductionSet() []eventctx.Middleware {
	return NewMiddlewareSet().
		WithRecover().
		WithDedup().
		WithCircuitBreaker().
		WithAdaptive().
		WithLogging().
		Build()
}

// DevelopmentSet 返回开发环境推荐的中间件组合
//
// 包含：
//   - Recover: panic恢复
//   - Logging: 日志记录
//
// 使用示例:
//
//	engine.Use(middleware.DevelopmentSet()...)
func DevelopmentSet() []eventctx.Middleware {
	return NewMiddlewareSet().
		WithRecover().
		WithLogging().
		Build()
}

// BasicSet 返回基础中间件组合
//
// 仅包含 Recover，适合测试环境
//
// 使用示例:
//
//	engine.Use(middleware.BasicSet()...)
func BasicSet() []eventctx.Middleware {
	return NewMiddlewareSet().
		WithRecover().
		Build()
}
