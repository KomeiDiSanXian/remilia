package middleware

import (
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// SimpleAdaptive 创建带默认配置的自适应限流器中间件
//
// 这是最简单的使用方式，适合大多数场景
//
// 使用示例:
//
//	engine.Use(middleware.SimpleAdaptive())
func SimpleAdaptive() eventctx.Middleware {
	arl := NewAdaptiveRateLimiter(DefaultAdaptiveConfig())
	arl.Start()
	return arl.Middleware()
}

// SimpleAdaptiveWithLimit 创建指定并发限制的自适应限流器
//
// 快速设置最大并发数，其他参数使用默认值
//
// 使用示例:
//
//	engine.Use(middleware.SimpleAdaptiveWithLimit(200))
func SimpleAdaptiveWithLimit(maxConcurrency int) eventctx.Middleware {
	config := DefaultAdaptiveConfig()
	config.MaxConcurrency = maxConcurrency
	config.InitialLimit = maxConcurrency / 2
	config.MinConcurrency = maxConcurrency / 10

	arl := NewAdaptiveRateLimiter(config)
	arl.Start()
	return arl.Middleware()
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
// 包含：
//   - Recover: panic恢复
//   - Logging: 日志记录
//   - Adaptive: 自适应限流
//   - CircuitBreaker: 熔断器
//   - Dedup: 去重
//
// 使用示例:
//
//	engine.Use(middleware.ProductionSet()...)
func ProductionSet() []eventctx.Middleware {
	return NewMiddlewareSet().
		WithRecover().
		WithLogging().
		WithAdaptive().
		WithCircuitBreaker().
		WithDedup().
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
