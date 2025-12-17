package middleware

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// Logging 记录处理耗时与错误
func Logging() remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			start := time.Now()
			err := next(ctx)
			logrus.WithError(err).WithFields(logrus.Fields{
				"latency": time.Since(start),
				"type":    ctx.GetEventType(),
			}).Info("handler")
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
func Recover() remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					// 获取堆栈信息
					stack := make([]byte, 4096)
					length := runtime.Stack(stack, false)

					// 记录详细日志
					logrus.WithFields(logrus.Fields{
						"panic":      r,
						"stack":      string(stack[:length]),
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

// Auth 简单鉴权：阻止非白名单用户（示例）
func Auth(allow func(ctx *remilia.Context) bool) remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			if !allow(ctx) {
				logrus.WithField("user", ctx.GetAuthor()).Warn("unauthorized")
				return fmt.Errorf("unauthorized")
			}
			return next(ctx)
		}
	}
}

// Timeout Handler 超时中间件
// 为 Handler 设置执行超时时间，超时后自动取消并返回错误
//
// 使用示例:
//
//	engine.Use(middleware.Timeout(5 * time.Second))
func Timeout(timeout time.Duration) remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			// 创建带超时的标准库 context
			stdCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
			defer cancel()

			// 保存原始 context
			originalCtx := ctx.Context()
			defer ctx.SetStdContext(originalCtx) // 恢复原始 context

			// 设置带超时的 context
			ctx.SetStdContext(stdCtx)

			// 创建带超时的 Context
			done := make(chan error, 1)

			// 使用 Timer 而不是 time.After，可以手动停止避免泄漏
			timer := time.NewTimer(timeout)
			defer timer.Stop() // 确保 timer 被停止，避免资源泄漏

			// 在 goroutine 中执行
			go func() {
				defer func() {
					if r := recover(); r != nil {
						// 使用 select + default 防止超时后写入阻塞
						select {
						case done <- fmt.Errorf("panic in handler: %v", r):
						default:
						}
					}
				}()

				// 执行 handler，现在 handler 可以通过 ctx.Context() 访问带超时的 context
				err := next(ctx)

				// 使用 select + default 防止超时后写入阻塞
				select {
				case done <- err:
				default:
					// 超时后，主 goroutine 已经返回，不再写入
				}
			}()

			// 等待完成或超时
			select {
			case err := <-done:
				return err
			case <-timer.C:
				logrus.WithFields(logrus.Fields{
					"timeout":    timeout,
					"event_type": ctx.GetEventType(),
				}).Warn("[Timeout] Handler execution timeout")
				return fmt.Errorf("handler timeout after %v", timeout)
			}
		}
	}
}

// Metrics 打点示例：这里只是打印，可对接 Prometheus
func Metrics() remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			start := time.Now()
			err := next(ctx)
			latency := time.Since(start)
			// 简单日志打点，生产环境可使用 PrometheusMetrics 中间件
			logrus.WithError(err).WithField("latency_ms", latency.Milliseconds()).Debug("metrics")
			return err
		}
	}
}

// ConcurrencyLimit 并发限流中间件
// maxInFlight: 最大并发数；policy: Drop=丢弃, Block=阻塞, TryWait=超时等待
// waitTimeout: TryWait 策略的超时时间
//
// 使用示例:
//
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyDrop, 0))
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyBlock, 0))
//	engine.Use(middleware.ConcurrencyLimit(100, middleware.ConcurrencyTryWait, 200*time.Millisecond))
func ConcurrencyLimit(maxInFlight int, policy ConcurrencyPolicy, waitTimeout time.Duration) remilia.HandlerMiddleware {
	if maxInFlight <= 0 {
		maxInFlight = 100 // 默认值
	}
	if waitTimeout <= 0 && policy == ConcurrencyTryWait {
		waitTimeout = 200 * time.Millisecond // 默认超时
	}

	sema := make(chan struct{}, maxInFlight)
	var dropped uint64

	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			// 尝试获取令牌
			acquired := false
			switch policy {
			case ConcurrencyDrop:
				select {
				case sema <- struct{}{}:
					acquired = true
				default:
					atomic.AddUint64(&dropped, 1)
					logrus.WithField("dropped_total", atomic.LoadUint64(&dropped)).
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
					logrus.WithField("dropped_total", atomic.LoadUint64(&dropped)).
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
//	requestID := ctx.GetState("request_id")
func RequestID() remilia.HandlerMiddleware {
	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			// 生成唯一 ID（使用时间戳 + 随机数）
			requestID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())

			// 存储到 Context
			ctx.SetState("request_id", requestID)

			// 记录日志
			logrus.WithFields(logrus.Fields{
				"request_id": requestID,
				"event_type": ctx.GetEventType(),
			}).Debug("[RequestID] Generated")

			return next(ctx)
		}
	}
}

// RateLimitTokenBucket 令牌桶限流：支持共享桶或按键（用户/群）维度
// rate: 每秒令牌数；burst: 突发容量；keyFn: 返回限流键（为空表示共享桶）
//
// 使用示例:
//
//	// 全局限流
//	engine.Use(middleware.RateLimitTokenBucket(10, 20, nil))
//
//	// 按用户限流
//	engine.Use(middleware.RateLimitTokenBucket(5, 10, func(ctx *remilia.Context) string {
//	    return ctx.GetAuthor() // 返回用户 ID
//	}))
func RateLimitTokenBucket(ratePerSec int, burst int, keyFn func(*remilia.Context) string) remilia.HandlerMiddleware {
	if ratePerSec <= 0 {
		ratePerSec = 1
	}
	if burst <= 0 {
		burst = 1
	}

	shared := rate.NewLimiter(rate.Limit(ratePerSec), burst)
	buckets := make(map[string]*rate.Limiter)
	var mu sync.RWMutex // 保护 buckets map

	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			key := ""
			if keyFn != nil {
				key = keyFn(ctx)
			}

			lim := shared
			if key != "" {
				// 先尝试读取
				mu.RLock()
				b, ok := buckets[key]
				mu.RUnlock()

				if ok {
					lim = b
				} else {
					// 不存在则创建（需要写锁）
					mu.Lock()
					// 双重检查，避免重复创建
					if b, ok := buckets[key]; ok {
						lim = b
					} else {
						b = rate.NewLimiter(rate.Limit(ratePerSec), burst)
						buckets[key] = b
						lim = b
					}
					mu.Unlock()
				}
			}

			if !lim.Allow() {
				logrus.WithField("key", key).Warn("[RateLimit] Rate limited")
				return fmt.Errorf("rate limited")
			}
			return next(ctx)
		}
	}
}
