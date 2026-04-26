package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int           // 最大重试次数
	BackoffBase time.Duration // 初始退避时间
	BackoffMax  time.Duration // 最大退避时间

	// 可选：判断是否应该重试的函数
	ShouldRetry func(err error) bool
}

// Retry 重试中间件
// 当处理器返回错误时，会自动重试指定次数，使用指数退避策略
//
// 使用示例:
//
//	engine.Use(middleware.Retry(middleware.RetryConfig{
//	    MaxAttempts: 3,
//	    BackoffBase: 200 * time.Millisecond,
//	    BackoffMax: 2 * time.Second,
//	}))
func Retry(cfg RetryConfig) eventctx.Middleware {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = 200 * time.Millisecond
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = 2 * time.Second
	}
	if cfg.ShouldRetry == nil {
		// 默认所有错误都重试（除了 BlockError）
		cfg.ShouldRetry = func(err error) bool {
			return err != nil && !errutil.IsBlockError(err)
		}
	}

	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			var lastErr error

			// 尝试执行（包括首次执行）
			for attempt := 0; attempt <= cfg.MaxAttempts; attempt++ {
				ctx.SetRetryAttempt(attempt)

				err := next(ctx)

				// 成功执行
				if err == nil {
					if attempt > 0 {
						logger.WithFields(logger.Fields{
							"attempt":    attempt,
							"event_type": ctx.GetEventType(),
						}).Info("[Retry] Succeeded after retry")
					}
					return nil
				}

				lastErr = err

				// 检查是否应该重试
				if !cfg.ShouldRetry(err) {
					logger.WithError(err).
						WithField("attempt", attempt).
						Debug("[Retry] Error not retryable")
					return err
				}

				// 达到最大重试次数
				if attempt >= cfg.MaxAttempts {
					logger.WithError(err).
						WithFields(logger.Fields{
							"max_attempts": cfg.MaxAttempts,
							"event_type":   ctx.GetEventType(),
						}).Warn("[Retry] Max attempts reached")
					return err
				}

				// 计算退避时间（指数退避）
				// 防止 attempt >= 63 时 1<<uint(attempt) 整数溢出，将移位上界限为 62
				const maxBackoffShift = 62
				shift := min(uint(attempt), maxBackoffShift)
				delay := min(cfg.BackoffBase*time.Duration(1<<shift), cfg.BackoffMax)

				logger.WithError(err).
					WithFields(logger.Fields{
						"attempt": attempt + 1,
						"delay":   delay,
					}).Debug("[Retry] Retrying after delay")

				// 等待后重试
				if !sleepWithContext(ctx.Context(), delay) {
					logger.WithFields(logger.Fields{
						"attempt":    attempt + 1,
						"event_type": ctx.GetEventType(),
					}).Warn("[Retry] Context canceled during backoff")
					// 修复 #16：返回 ctx.Err() 而非 BlockError，
					// 语义更准确，调用方可用 errors.Is(err, context.Canceled) 判断
					if ctxErr := ctx.Context().Err(); ctxErr != nil {
						return ctxErr
					}
					return errutil.NewBlockError("retry canceled")
				}

				// 再次检查 context 是否取消（在实际执行前）
				select {
				case <-ctx.Context().Done():
					logger.WithFields(logger.Fields{
						"attempt":    attempt + 1,
						"event_type": ctx.GetEventType(),
					}).Warn("[Retry] Context canceled before retry attempt")
					// 修复 #16：同上，返回 ctx.Err()
					if ctxErr := ctx.Context().Err(); ctxErr != nil {
						return ctxErr
					}
					return errutil.NewBlockError("retry canceled")
				default:
					// Context 仍然有效，继续重试
				}
			}

			return lastErr
		}
	}
}

// RetryWithDeadLetter 带死信队列的重试中间件
// 超过最大重试次数后，将事件发送到死信队列
//
// deadLetterCh 的元素类型为 infra/dlq.PlatformEventItem（即 dlq.Item[platform.Event]），
// 平台无关，符合分层原则：middleware → infra/dlq（而非 middleware → core/engine）。
//
// 使用示例:
//
//	// 1. 创建死信队列 channel
//	deadLetterCh := make(chan dlq.PlatformEventItem, 128)
//
//	// 2. 启动消费者处理死信
//	go func() {
//	    for item := range deadLetterCh {
//	        // 处理死信条目
//	    }
//	}()
//
//	// 3. 使用中间件
//	engine.Use(middleware.RetryWithDeadLetter(
//	    middleware.RetryConfig{MaxAttempts: 3, ...},
//	    deadLetterCh,
//	))
// retryDroppedCount 是全局的死信队列丢弃计数，用于基础可观测性。
// 可通过 prometheus 采集暴露在 /metrics 端点上的指标做补充。
var retryDroppedCount atomic.Int64

// RetryDroppedCount 返回死信队列满时丢弃的事件总数。
func RetryDroppedCount() int64 {
	return retryDroppedCount.Load()
}

func RetryWithDeadLetter(cfg RetryConfig, deadLetterCh chan dlq.Item[platform.Event]) eventctx.Middleware {
	// 初始化默认值
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = 200 * time.Millisecond
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = 2 * time.Second
	}
	if cfg.ShouldRetry == nil {
		cfg.ShouldRetry = func(err error) bool {
			return err != nil && !errutil.IsBlockError(err)
		}
	}

	retryMw := Retry(cfg)

	return func(next eventctx.Handler) eventctx.Handler {
		wrapped := retryMw(next)

		return func(ctx *eventctx.Context) error {
			err := wrapped(ctx)

			// 如果最终还是失败，发送到死信队列
			if err != nil && cfg.ShouldRetry(err) {
				attempt, _ := ctx.GetRetryAttempt()

				source := ctx.GetMatcherSource()

				item := dlq.Item[platform.Event]{
					Data:    ctx.GetPlatformEvent(),
					Err:     err,
					Attempt: attempt,
					Source:  source,
				}

				// 非阻塞发送到死信队列
				select {
				case deadLetterCh <- item:
					logger.WithFields(logger.Fields{
						"event_type": ctx.GetEventType(),
						"source":     source,
						"attempts":   attempt,
					}).Warn("[Retry] Event sent to dead letter queue")
				default:
					retryDroppedCount.Add(1)
					logger.WithError(err).
						WithFields(logger.Fields{
							"event_type":    ctx.GetEventType(),
							"source":        source,
							"total_dropped": retryDroppedCount.Load(),
						}).Error("[Retry] Dead letter queue full, dropping event (consider increasing channel buffer size)")
				}
			}

			return err
		}
	}
}

// ErrorHandler 错误处理中间件
// 捕获并处理所有错误，可以用于统一的错误日志、监控、告警等
//
// 使用示例:
//
//	engine.Use(middleware.ErrorHandler(func(ctx *core.Context, err error) {
//	    log.WithError(err).Error("Handler failed")
//	    // 发送告警、记录指标等
//	}))
func ErrorHandler(handler func(ctx *eventctx.Context, err error)) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			err := next(ctx)
			if err != nil {
				handler(ctx, err)
			}
			return err
		}
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ConfigurableRetry wraps Retry with a mutable config for hot-reload support.
// Pre-builds the middleware closure once to avoid per-request allocation.
type ConfigurableRetry struct {
	mu         sync.RWMutex
	cfg        RetryConfig
	middleware eventctx.Middleware // 预构建的中间件，仅 config 变更时重建
}

// NewConfigurableRetry creates a hot-reloadable retry middleware wrapper.
// Pre-builds the middleware function to avoid per-request closure allocation.
func NewConfigurableRetry(cfg RetryConfig) *ConfigurableRetry {
	cr := &ConfigurableRetry{cfg: cfg}
	cr.rebuildMiddleware()
	return cr
}

// rebuildMiddleware 预构建中间件闭包
func (cr *ConfigurableRetry) rebuildMiddleware() {
	cfg := cr.cfg
	cr.middleware = Retry(cfg)
}

// UpdateConfig updates the retry configuration at runtime (thread-safe).
func (cr *ConfigurableRetry) UpdateConfig(cfg RetryConfig) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if cfg.MaxAttempts > 0 {
		cr.cfg.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.BackoffBase > 0 {
		cr.cfg.BackoffBase = cfg.BackoffBase
	}
	if cfg.BackoffMax > 0 {
		cr.cfg.BackoffMax = cfg.BackoffMax
	}
	cr.rebuildMiddleware()
	logger.Info("[ConfigurableRetry] Config updated via hot-reload")
}

// Middleware returns the eventctx.Middleware function (pre-built, no per-request allocation).
func (cr *ConfigurableRetry) Middleware() eventctx.Middleware {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.middleware
}
