package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware/ctxkeys"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"golang.org/x/time/rate"
)

// Logging 记录处理耗时、事件类型及上下文信息（平台、用户、会话、请求链路 ID）。
//
// 相比 Metrics，Logging 输出更丰富的结构化字段，方便排查问题。
// 错误时输出 Error 级别，成功时输出 Debug 级别。
//
// 使用示例:
//
//	engine.Use(middleware.Logging())
func Logging() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)

			fields := logger.Fields{
				"latency_ms": time.Since(start).Milliseconds(),
				"event_type": ctx.GetEventType(),
			}

			if p := ctx.GetEventPlatform(); p != "" {
				fields["platform"] = p
			}
			if s := ctx.GetSenderInfo(); s.ID != "" {
				fields["user_id"] = s.ID
				if s.DisplayName != "" && s.DisplayName != s.ID {
					fields["user_name"] = s.DisplayName
				}
			}
			if c := ctx.GetChatInfo(); c.ID != "" {
				fields["chat_id"] = c.ID
				if c.IsGroup {
					fields["is_group"] = true
				}
			}
			if ridRaw, ok := ctx.Get(ctxkeys.CtxKeyRequestID); ok {
				if rid, ok2 := ridRaw.(string); ok2 && rid != "" {
					fields["request_id"] = rid
				}
			}

			entry := logger.WithError(err).WithFields(fields)
			if err != nil {
				entry.Error("handler execution failed")
			} else {
				entry.Debug("handler execution success")
			}
			return err
		}
	}
}

// Recover panic 恢复中间件，捕获 Handler 中的 panic 并转换为错误
// 记录详细的堆栈信息，避免进程崩溃
//
// 使用示例:
//
//	engine.Use(middleware.Recover())
func Recover() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					stack := captureStack()

					logger.WithFields(logger.Fields{
						"panic":      r,
						"stack":      stack,
						"event_type": ctx.GetEventType(),
					}).Error("[Recover] Panic recovered")

					err = fmt.Errorf("panic recovered: %v", r)
				}
			}()
			return next(ctx)
		}
	}
}

// captureStack 获取当前 goroutine 的完整堆栈信息。
//
// 先通过 runtime.Stack(nil, false) 获取所需缓冲区大小，再一次性分配。
// 避免自适应翻倍循环中的多次分配。上限 64KB，超过时截断并标记。
func captureStack() string {
	size := min(runtime.Stack(nil, false), 64*1024)
	buf := make([]byte, size)
	n := runtime.Stack(buf, false)
	if n == size && size >= 64*1024 {
		return string(buf[:n]) + "\n[stack truncated: exceeded 64KB limit]"
	}
	return string(buf[:n])
}

// Auth 简单鉴权：阻止非白名单用户（示例）
func Auth(allow func(ctx *eventctx.Context) bool) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if !allow(ctx) {
				logger.WithField("user", ctx.GetSenderInfo().ID).Warn("unauthorized")
				return fmt.Errorf("unauthorized")
			}
			return next(ctx)
		}
	}
}

// Timeout 创建一个超时控制中间件。
//
// 通过向 ctx 注入带 deadline 的 stdCtx 来实现超时控制，handler 应监听
// ctx.Context().Done() 来感知超时并提前退出。这与 Go 标准库 context 的
// 取消语义完全一致，且不引入额外 goroutine（避免并发写同一 Context 的 race）。
//
// 注意：若 handler 完全不检查 ctx.Context().Done()（如纯 CPU 计算），
// 则超时信号不会强制中断它——此时请在 handler 内部主动检查。
//
//	engine.Use(middleware.Timeout(5 * time.Second))
func Timeout(timeout time.Duration) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			stdCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
			defer cancel()

			originalCtx := ctx.Context()
			ctx.SetStdContext(stdCtx)
			defer ctx.SetStdContext(originalCtx)

			err := next(ctx)
			if err != nil && stdCtx.Err() != nil {
				logger.WithFields(logger.Fields{
					"timeout":    timeout,
					"event_type": ctx.GetEventType(),
				}).Warn("[Timeout] Handler execution timeout")
				return fmt.Errorf("handler timeout after %v: %w", timeout, err)
			}
			return err
		}
	}
}

// Metrics 记录处理耗时到 Debug 日志。
//
// Metrics 是 Logging 的轻量版本，始终使用 Debug 级别输出，
// 适合高频调用的开发诊断场景。生产环境建议使用 Logging。
func Metrics() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			logger.WithError(err).
				WithField("latency_ms", time.Since(start).Milliseconds()).
				Debug("[Metrics] handler executed")
			return err
		}
	}
}

// BackpressurePolicy 反压策略
type BackpressurePolicy int

const (
	BackpressureDrop    BackpressurePolicy = iota // 超过限制直接丢弃
	BackpressureBlock                             // 超过限制阻塞等待
	BackpressureTryWait                           // 超过限制等待一段时间，超时则丢弃
)

// Backpressure 创建一个反压（并发限制）中间件
//
// 使用示例:
//
//	engine.Use(middleware.Backpressure(100, middleware.BackpressureDrop, 0))
//	engine.Use(middleware.Backpressure(100, middleware.BackpressureBlock, 0))
//	engine.Use(middleware.Backpressure(100, middleware.BackpressureTryWait, 200*time.Millisecond))
func Backpressure(maxInFlight int, policy BackpressurePolicy, waitTimeout time.Duration) eventctx.Middleware {
	if maxInFlight <= 0 {
		maxInFlight = 100
	}
	if waitTimeout <= 0 && policy == BackpressureTryWait {
		waitTimeout = 200 * time.Millisecond
	}

	sema := make(chan struct{}, maxInFlight)
	var dropped atomic.Uint64

	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			acquired := false
			switch policy {
			case BackpressureDrop:
				select {
				case sema <- struct{}{}:
					acquired = true
				default:
					dropped.Add(1)
					logger.WithField("dropped_total", dropped.Load()).
						Warn("[Backpressure] Dropped due to limit")
					return fmt.Errorf("backpressure limit exceeded (drop)")
				}
			case BackpressureBlock:
				// 必须同时监听 ctx：无条件阻塞在信号量上时，外层 Timeout 中间件
				// 与停机取消都唤不醒等待者。一旦有 maxInFlight 个 handler 卡在
				// 不响应 ctx 的 IO 上，后续每个事件的 goroutine 都会永久堆积，
				// 直到内存耗尽，且全程没有任何丢弃或日志信号。
				select {
				case sema <- struct{}{}:
					acquired = true
				case <-ctx.Context().Done():
					dropped.Add(1)
					logger.WithField("dropped_total", dropped.Load()).
						Warn("[Backpressure] Aborted while waiting (context done)")
					return ctx.Context().Err()
				}
			case BackpressureTryWait:
				timer := time.NewTimer(waitTimeout)
				defer timer.Stop()
				select {
				case sema <- struct{}{}:
					acquired = true
				case <-timer.C:
					dropped.Add(1)
					logger.WithField("dropped_total", dropped.Load()).
						WithField("timeout", waitTimeout).
						Warn("[Backpressure] Dropped due to wait timeout")
					return fmt.Errorf("backpressure limit exceeded (timeout)")
				}
			}

			if acquired {
				defer func() {
					<-sema
				}()
			}

			return next(ctx)
		}
	}
}

// RequestID 请求 ID 中间件
// 为每个请求生成唯一 ID，便于链路追踪和日志关联
//
// 使用示例:
//
//	engine.Use(middleware.RequestID())
//
//	// 在 Handler 中获取
//	requestID, _ := ctx.Get(middleware.CtxKeyRequestID)
func RequestID() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				logger.WithError(err).Warn("[RequestID] Failed to generate random ID, falling back to timestamp")
				requestID := fmt.Sprintf("%d", time.Now().UnixNano())
				ctx.Set(ctxkeys.CtxKeyRequestID, requestID)
				return next(ctx)
			}
			requestID := hex.EncodeToString(b[:])

			ctx.Set(ctxkeys.CtxKeyRequestID, requestID)

			logger.WithFields(logger.Fields{
				ctxkeys.CtxKeyRequestID: requestID,
				"event_type":            ctx.GetEventType(),
			}).Debug("[RequestID] Generated")

			return next(ctx)
		}
	}
}

// 默认桶上限，可在测试中临时调小验证淘汰行为
var rateLimitMaxBuckets = 10000

// 过期 TTL 与清理间隔，可在测试中调小
var (
	rateLimitBucketTTL       = 10 * time.Minute
	rateLimitCleanupInterval = 5 * time.Minute
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	// MaxBuckets 最大桶数量，超过后淘汰最久未访问的桶
	// 默认: 10000
	MaxBuckets int

	// BucketTTL 桶过期时间
	// 默认: 10 分钟
	BucketTTL time.Duration

	// CleanupInterval 清理间隔
	// 默认: 5 分钟
	CleanupInterval time.Duration
}

// DefaultRateLimitConfig 返回默认限流配置
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MaxBuckets:      rateLimitMaxBuckets,
		BucketTTL:       rateLimitBucketTTL,
		CleanupInterval: rateLimitCleanupInterval,
	}
}

// RateLimitTokenBucket 创建一个令牌桶限流中间件
//
// 使用示例:
//
//	// 全局限流
//	engine.Use(middleware.RateLimitTokenBucket(10, 20, func(ctx *core.Context) string {
//	    return "global" // 所有请求共享一个限流器
//	}))
//
//	// 按用户限流
//	engine.Use(middleware.RateLimitTokenBucket(5, 10, func(ctx *core.Context) string {
//	    return ctx.GetAuthor() // 返回用户 ID
//	}))
func RateLimitTokenBucket(ratePerSec int, burst int, keyFn func(*eventctx.Context) string) eventctx.Middleware {
	return RateLimitTokenBucketWithConfig(DefaultRateLimitConfig(), ratePerSec, burst, keyFn)
}

// RateLimitTokenBucketWithConfig 使用指定配置创建令牌桶限流中间件
func RateLimitTokenBucketWithConfig(config RateLimitConfig, ratePerSec int, burst int, keyFn func(*eventctx.Context) string) eventctx.Middleware {
	if ratePerSec <= 0 {
		ratePerSec = 1
	}
	if burst <= 0 {
		burst = 1
	}
	if config.MaxBuckets <= 0 {
		config.MaxBuckets = rateLimitMaxBuckets
	}
	if config.BucketTTL <= 0 {
		config.BucketTTL = rateLimitBucketTTL
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = rateLimitCleanupInterval
	}

	shared := rate.NewLimiter(rate.Limit(ratePerSec), burst)

	rl := newRateLimitShards(config)
	var lastCleanup atomic.Int64
	lastCleanup.Store(time.Now().UnixNano())

	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			key := ""
			if keyFn != nil {
				key = keyFn(ctx)
			}

			now := time.Now()
			rl.cleanupIfNeeded(now, config, &lastCleanup)

			lim := shared
			if key != "" {
				lim = rl.getOrCreateLimiter(key, now, ratePerSec, burst)
			}

			if !lim.Allow() {
				logger.WithField("key", key).Warn("[RateLimit] Rate limited")
				return fmt.Errorf("rate limited")
			}
			return next(ctx)
		}
	}
}

// rateLimitShard 是分片令牌桶存储的一个分片。
type rateLimitShard struct {
	mu      sync.RWMutex
	buckets map[string]*rateLimitBucketEntry
}

type rateLimitBucketEntry struct {
	lim       *rate.Limiter
	lastVisit time.Time
}

const rateLimitNumShards = 64

// rateLimitShards 管理分片的令牌桶集合。
type rateLimitShards struct {
	shards [rateLimitNumShards]*rateLimitShard
}

func newRateLimitShards(config RateLimitConfig) *rateLimitShards {
	rl := &rateLimitShards{}
	for i := range rateLimitNumShards {
		rl.shards[i] = &rateLimitShard{
			buckets: make(map[string]*rateLimitBucketEntry),
		}
	}
	return rl
}

// fnv1aHash 对 key 做 FNV-1a 哈希，映射到分片索引。
func fnv1aHash(key string) int {
	var h uint64 = 14695981039346656037
	for i := range len(key) {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	return int(h & (rateLimitNumShards - 1))
}

// cleanupIfNeeded 检查是否到期清理过期桶，并按分片逐出最久未访问的桶。
func (rl *rateLimitShards) cleanupIfNeeded(now time.Time, config RateLimitConfig, lastCleanup *atomic.Int64) {
	nowNano := now.UnixNano()
	if time.Duration(nowNano-lastCleanup.Load()) < config.CleanupInterval {
		return
	}
	lastCleanup.Store(nowNano)
	for i := range rateLimitNumShards {
		s := rl.shards[i]
		s.mu.Lock()
		for k, v := range s.buckets {
			if now.Sub(v.lastVisit) > config.BucketTTL {
				delete(s.buckets, k)
			}
		}
		maxPerShard := config.MaxBuckets/rateLimitNumShards + 1
		if overflow := len(s.buckets) - maxPerShard; overflow > 0 {
			for range overflow {
				eldestKey, eldestTime := "", time.Time{}
				first := true
				for k, v := range s.buckets {
					if first || v.lastVisit.Before(eldestTime) {
						eldestKey, eldestTime = k, v.lastVisit
					}
					first = false
				}
				delete(s.buckets, eldestKey)
			}
		}
		s.mu.Unlock()
	}
}

// getOrCreateLimiter 获取或创建 key 对应的令牌桶。
func (rl *rateLimitShards) getOrCreateLimiter(key string, now time.Time, ratePerSec, burst int) *rate.Limiter {
	s := rl.shards[fnv1aHash(key)]
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.buckets[key]
	if ok {
		entry.lastVisit = now
		return entry.lim
	}
	entry = &rateLimitBucketEntry{
		lim:       rate.NewLimiter(rate.Limit(ratePerSec), burst),
		lastVisit: now,
	}
	s.buckets[key] = entry
	return entry.lim
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
	burst := max(int(perSecond*2), 1)
	return RateLimitTokenBucket(int(perSecond), burst, nil)
}

// Set 中间件集合，提供常用中间件组合
type Set struct {
	middlewares []eventctx.Middleware
}

// NewMiddlewareSet 创建中间件集合
func NewMiddlewareSet() *Set {
	return &Set{
		middlewares: make([]eventctx.Middleware, 0),
	}
}

// WithLogging 添加日志中间件
func (s *Set) WithLogging() *Set {
	s.middlewares = append(s.middlewares, Logging())
	return s
}

// WithRecover 添加panic恢复中间件
func (s *Set) WithRecover() *Set {
	s.middlewares = append(s.middlewares, Recover())
	return s
}

// WithAdaptive 添加自适应限流中间件
func (s *Set) WithAdaptive() *Set {
	s.middlewares = append(s.middlewares, ratelimit.SimpleAdaptive())
	return s
}

// WithCircuitBreaker 添加熔断器中间件
func (s *Set) WithCircuitBreaker() *Set {
	s.middlewares = append(s.middlewares, resilience.SimpleCircuitBreaker())
	return s
}

// WithDedup 添加去重中间件
func (s *Set) WithDedup() *Set {
	s.middlewares = append(s.middlewares, dedup.SimpleDedup())
	return s
}

// WithTimeout 添加超时控制中间件
func (s *Set) WithTimeout(timeout time.Duration) *Set {
	s.middlewares = append(s.middlewares, Timeout(timeout))
	return s
}

// WithRequestID 添加请求 ID 中间件
func (s *Set) WithRequestID() *Set {
	s.middlewares = append(s.middlewares, RequestID())
	return s
}

// WithRetry 添加重试中间件
func (s *Set) WithRetry(cfg resilience.RetryConfig) *Set {
	s.middlewares = append(s.middlewares, resilience.Retry(cfg))
	return s
}

// Build 返回所有中间件
func (s *Set) Build() []eventctx.Middleware {
	return s.middlewares
}

// ProductionSet 返回生产环境推荐的中间件组合
//
// 中间件执行顺序（从外到内）：
//  1. Recover:        panic 恢复（最外层，保证任何 panic 都能被捕获）
//  2. RequestID:      请求链路追踪 ID
//  3. Timeout:        超时控制（避免 handler 无限等待）
//  4. Dedup:          去重过滤（在限流前过滤重复请求，避免浪费配额）
//  5. CircuitBreaker: 熔断器（在限流前熔断，快速失败）
//  6. Adaptive:       自适应限流
//  7. Logging:        日志记录（最内层，记录实际处理的请求）
//
// 使用示例:
//
//	engine.Use(middleware.ProductionSet()...)
func ProductionSet() []eventctx.Middleware {
	return NewMiddlewareSet().
		WithRecover().
		WithRequestID().
		WithTimeout(30 * time.Second).
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
