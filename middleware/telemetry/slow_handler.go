package telemetry

import (
	stdctx "context"
	"errors"
	"time"

	appconfig "github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// SlowHandlerConfig 慢处理器检测配置
type SlowHandlerConfig struct {
	// Threshold 慢处理器阈值，超过此时间将记录警告
	Threshold time.Duration

	// Logger 自定义日志函数，如果为 nil 则使用默认日志记录方式
	Logger func(handlerName string, duration time.Duration, ctx *context.Context)

	// OnSlowHandler 慢处理器回调，可用于告警
	OnSlowHandler func(handlerName string, duration time.Duration, ctx *context.Context)
}

// SlowHandler 创建慢处理器检测中间件
//
// 使用示例:
//
//	engine.Use(middleware.SlowHandler(middleware.SlowHandlerConfig{
//	    Threshold: 1 * time.Second,
//	}))
func SlowHandler(config SlowHandlerConfig) context.Middleware {
	// 设置默认阈值
	if config.Threshold == 0 {
		config.Threshold = 1 * time.Second
	}

	// 设置默认 Logger
	if config.Logger == nil {
		config.Logger = func(handlerName string, duration time.Duration, ctx *context.Context) {
			logger.WithFields(logger.Fields{
				"handler":    handlerName,
				"duration":   duration,
				"event_type": ctx.GetEventType(),
			}).Warnf("[SlowHandler] Handler took %v (threshold: %v)", duration, config.Threshold)
		}
	}

	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			// 在执行 handler 之前注入 deadline，使 handler 内部可通过
			// ctx.Context().Done() 感知"被监控"并主动缩短路径。
			// 此 deadline 仅用于监控提示，不强制执行——若 handler 因 deadline
			// 返回 deadline 超时错误，此处会将其屏蔽，避免影响正常处理结果。
			stdCtx, cancel := stdctx.WithTimeout(ctx.Context(), config.Threshold)
			defer cancel()
			originalCtx := ctx.Context()
			ctx.SetStdContext(stdCtx)
			defer ctx.SetStdContext(originalCtx)

			start := time.Now()

			// 执行处理器
			err := next(ctx)

			// 计算耗时
			duration := time.Since(start)

			// 检查是否超过阈值
			if duration > config.Threshold {
				// 获取处理器名称（使用事件类型作为标识）
				handlerName := ctx.GetEventType()

				// 记录日志
				config.Logger(handlerName, duration, ctx)

				// 调用回调
				if config.OnSlowHandler != nil {
					config.OnSlowHandler(handlerName, duration, ctx)
				}
			}

			// SlowHandler 仅用于监控。仅屏蔽由本中间件注入的监控 deadline 触发的
			// 超时错误：当返回错误是 DeadlineExceeded 且父 context 仍未取消/超时时，
			// 说明超时来自本中间件的 Threshold，可安全屏蔽；其余（真实业务错误、
			// 父级取消/超时）一律透传，避免把失败静默成功导致丢事件。
			if err != nil {
				if errors.Is(err, stdctx.DeadlineExceeded) && originalCtx.Err() == nil {
					return nil
				}
				return err
			}
			return nil
		}
	}
}

// SlowHandlerSimple 创建简单的慢处理器检测中间件
// 使用默认配置，阈值为 1 秒
//
// 使用示例:
//
//	engine.Use(middleware.SlowHandlerSimple(2 * time.Second))
func SlowHandlerSimple(threshold time.Duration) context.Middleware {
	return SlowHandler(SlowHandlerConfig{
		Threshold: threshold,
	})
}

// SlowHandlerFromConfig 从配置创建慢处理器检测中间件
//
// 使用示例：
//
//	cfg, _ := config.LoadDefault()
//	engine.Use(middleware.SlowHandlerFromConfig(cfg.Middleware))
func SlowHandlerFromConfig(cfg appconfig.MiddlewareConfig) context.Middleware {
	threshold := 1 * time.Second
	if cfg.SlowHandler.Threshold != "" {
		if d, err := time.ParseDuration(cfg.SlowHandler.Threshold); err == nil {
			threshold = d
		} else {
			logger.WithError(err).Warn("[SlowHandler] Invalid slow_handler.threshold config, using default 1s")
		}
	}

	logger.Infof("[SlowHandler] Config: threshold=%v", threshold)

	return SlowHandler(SlowHandlerConfig{
		Threshold: threshold,
	})
}
