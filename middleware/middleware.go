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
	"golang.org/x/time/rate"
)

// Logging 记录处理耗时与错误
func Logging() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			entry := logger.WithError(err).WithFields(logger.Fields{
				"latency": time.Since(start),
				"type":    ctx.GetEventType(),
			})
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
					// 获取堆栈信息（自适应缓冲区，避免深调用栈截断）
					stack := captureStack()

					// 记录详细日志
					logger.WithFields(logger.Fields{
						"panic":      r,
						"stack":      stack,
						"event_type": ctx.GetEventType(),
					}).Error("[Recover] Panic recovered")

					// 转换为错误
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

			// 保存原始 context，处理完后恢复，避免影响后续中间件
			originalCtx := ctx.Context()
			ctx.SetStdContext(stdCtx)
			defer ctx.SetStdContext(originalCtx)

			err := next(ctx) // 同步调用，无额外 goroutine
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

// Metrics 打点示例：这里只是打印，可对接 Prometheus
func Metrics() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			latency := time.Since(start)
			// 简单日志打点，生产环境可使用 PrometheusMetrics 中间件
			logger.WithError(err).WithField("latency_ms", latency.Milliseconds()).Debug("metrics")
			return err
		}
	}
}

// ConcurrencyLimit 创建一个并发限制中间件
//
// 使用示例:
//
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyDrop, 0))
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyBlock, 0))
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyTryWait, 200*time.Millisecond))
func ConcurrencyLimit(maxInFlight int, policy ConcurrencyPolicy, waitTimeout time.Duration) eventctx.Middleware {
	if maxInFlight <= 0 {
		maxInFlight = 100 // 默认值
	}
	if waitTimeout <= 0 && policy == ConcurrencyTryWait {
		waitTimeout = 200 * time.Millisecond // 默认超时
	}

	sema := make(chan struct{}, maxInFlight)
	var dropped uint64

	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			// 尝试获取令牌
			acquired := false
			switch policy {
			case ConcurrencyDrop:
				select {
				case sema <- struct{}{}:
					acquired = true
				default:
					atomic.AddUint64(&dropped, 1)
					logger.WithField("dropped_total", atomic.LoadUint64(&dropped)).
						Warn("[ConcurrencyLimit] Dropped due to concurrency limit")
					return fmt.Errorf("concurrency limit exceeded (drop)")
				}
			case ConcurrencyBlock:
				sema <- struct{}{}
				acquired = true
			case ConcurrencyTryWait:
				timer := time.NewTimer(waitTimeout)
				defer timer.Stop()
				select {
				case sema <- struct{}{}:
					acquired = true
				case <-timer.C:
					atomic.AddUint64(&dropped, 1)
					logger.WithField("dropped_total", atomic.LoadUint64(&dropped)).
						WithField("timeout", waitTimeout).
						Warn("[ConcurrencyLimit] Dropped due to wait timeout")
					return fmt.Errorf("concurrency limit exceeded (timeout)")
				}
			}

			if acquired {
				defer func() {
					<-sema // 释放信号量：从 channel 接收，释放一个令牌
				}()
			}

			return next(ctx)
		}
	}
}

// ConcurrencyPolicy 并发策略
type ConcurrencyPolicy int

const (
	ConcurrencyDrop    ConcurrencyPolicy = iota // 超过限制直接丢弃
	ConcurrencyBlock                            // 超过限制阻塞等待
	ConcurrencyTryWait                          // 超过限制等待一段时间，超时则丢弃
)

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
			// 生成唯一 ID（16 字节 crypto/rand + hex 编码，防碰撞 + 防时间回拨）
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				logger.WithError(err).Warn("[RequestID] Failed to generate random ID, falling back to timestamp")
				requestID := fmt.Sprintf("%d", time.Now().UnixNano())
				ctx.Set(CtxKeyRequestID, requestID)
				return next(ctx)
			}
			requestID := hex.EncodeToString(b[:])

			// 存储到 Context（V2 sugar）
			ctx.Set(CtxKeyRequestID, requestID)

			// 记录日志
			logger.WithFields(logger.Fields{
				CtxKeyRequestID: requestID,
				"event_type":    ctx.GetEventType(),
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

	// 轻量级防泄漏：跟踪访问时间，超过上限时淘汰最久未访问的桶
	type bucketEntry struct {
		lim       *rate.Limiter
		lastVisit time.Time
	}

	// 分片 map 减少热点锁竞争：每个 shard 持有独立的 map 和锁。
	// 64 个分片（2^6）在 key 数量 > 1000 时显著降低写锁冲突概率。
	const numShards = 64

	type shard struct {
		mu      sync.RWMutex
		buckets map[string]*bucketEntry
	}

	shards := make([]*shard, numShards)
	for i := range numShards {
		shards[i] = &shard{
			buckets: make(map[string]*bucketEntry),
		}
	}

	// hashKey 将 string key 映射到 [0, numShards) 范围内的 shard 索引。
	// 使用 FNV-1a 哈希（轻量、均匀），按位与代替取模（要求 numShards 为 2 的幂）。
	hashKey := func(key string) int {
		var h uint64 = 14695981039346656037 // FNV offset basis
		for i := range len(key) {
			h ^= uint64(key[i])
			h *= 1099511628211 // FNV prime
		}
		return int(h & (numShards - 1))
	}

	// lastCleanup 用 atomic.Int64 存储 UnixNano，避免多个 goroutine 同时读写时的数据竞态。
	// 无锁快速路径（检查是否需要清理）和有锁慢速路径（执行清理）都通过原子操作访问它。
	var lastCleanup atomic.Int64
	lastCleanup.Store(time.Now().UnixNano())

	cleanupIfNeeded := func(now time.Time) {
		nowNano := now.UnixNano()
		// 无锁快速路径：原子读，不满足间隔则直接返回
		if time.Duration(nowNano-lastCleanup.Load()) < config.CleanupInterval {
			return
		}
		// 全部 shard 的清理持独立的 per-shard 写锁，不会形成全局瓶颈
		lastCleanup.Store(nowNano)
		for i := range numShards {
			s := shards[i]
			s.mu.Lock()
			for k, v := range s.buckets {
				if now.Sub(v.lastVisit) > config.BucketTTL {
					delete(s.buckets, k)
				}
			}
			// 如果仍超过上限，淘汰最久未访问的条目（LRU），
			// 避免 map 迭代顺序不可预测导致误删高频活跃 bucket。
			maxPerShard := config.MaxBuckets/numShards + 1
			if overflow := len(s.buckets) - maxPerShard; overflow > 0 {
				for range overflow {
					var eldestKey string
					var eldestTime time.Time
					first := true
					for k, v := range s.buckets {
						if first || v.lastVisit.Before(eldestTime) {
							eldestKey = k
							eldestTime = v.lastVisit
						}
						first = false
					}
					delete(s.buckets, eldestKey)
				}
			}
			s.mu.Unlock()
		}
	}

	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			key := ""
			if keyFn != nil {
				key = keyFn(ctx)
			}

			now := time.Now()
			cleanupIfNeeded(now)

			lim := shared
			if key != "" {
				s := shards[hashKey(key)]
				s.mu.Lock()
				entry, ok := s.buckets[key]
				if ok {
					entry.lastVisit = now
					lim = entry.lim
				} else {
					entry = &bucketEntry{
						lim:       rate.NewLimiter(rate.Limit(ratePerSec), burst),
						lastVisit: now,
					}
					s.buckets[key] = entry
					lim = entry.lim
				}
				s.mu.Unlock()
			}

			if !lim.Allow() {
				logger.WithField("key", key).Warn("[RateLimit] Rate limited")
				return fmt.Errorf("rate limited")
			}
			return next(ctx)
		}
	}
}
