package middleware

import (
	"context"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
			return err != nil && !engine.IsBlockError(err)
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
				delay := min(cfg.BackoffBase*time.Duration(1<<uint(attempt)), cfg.BackoffMax)

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
					return engine.NewBlockError("retry canceled")
				}

				// 再次检查 context 是否取消（在实际执行前）
				select {
				case <-ctx.Context().Done():
					logger.WithFields(logger.Fields{
						"attempt":    attempt + 1,
						"event_type": ctx.GetEventType(),
					}).Warn("[Retry] Context canceled before retry attempt")
					return engine.NewBlockError("retry canceled")
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
// 使用示例:
//
//	// 1. 创建死信队列 channel
//	deadLetterCh := make(chan core.DeadLetterItem, 128)
//
//	// 2. 启动消费者处理死信
//	go func() {
//	    for item := range deadLetterCh {
//	        consumer := core.FileDeadLetterConsumer{Path: "deadletter.log"}
//	        consumer.Consume(item)
//	    }
//	}()
//
//	// 3. 使用中间件
//	engine.Use(middleware.RetryWithDeadLetter(
//	    middleware.RetryConfig{MaxAttempts: 3, ...},
//	    deadLetterCh,
//	))
func RetryWithDeadLetter(cfg RetryConfig, deadLetterCh chan engine.DeadLetterItem) eventctx.Middleware {
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
			return err != nil && !engine.IsBlockError(err)
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

				item := engine.DeadLetterItem{
					Event:   ctx.GetEvent(),
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
					logger.WithError(err).
						WithField("event_type", ctx.GetEventType()).
						Error("[Retry] Dead letter queue full, dropping event")
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
