package middleware

import (
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/sirupsen/logrus"
)

// SlowHandlerConfig 慢处理器检测配置
type SlowHandlerConfig struct {
	// Threshold 慢处理器阈值，超过此时间将记录警告
	Threshold time.Duration

	// Logger 自定义日志函数，如果为 nil 则使用默认 logrus
	Logger func(handlerName string, duration time.Duration, ctx *remilia.Context)

	// OnSlowHandler 慢处理器回调，可用于告警
	OnSlowHandler func(handlerName string, duration time.Duration, ctx *remilia.Context)
}

// SlowHandler 创建慢处理器检测中间件
//
// 使用示例:
//
//	engine.Use(middleware.SlowHandler(middleware.SlowHandlerConfig{
//	    Threshold: 1 * time.Second,
//	}))
func SlowHandler(config SlowHandlerConfig) remilia.HandlerMiddleware {
	// 设置默认阈值
	if config.Threshold == 0 {
		config.Threshold = 1 * time.Second
	}

	// 设置默认 Logger
	if config.Logger == nil {
		config.Logger = func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			logrus.WithFields(logrus.Fields{
				"handler":    handlerName,
				"duration":   duration,
				"event_type": ctx.GetEventType(),
			}).Warnf("[SlowHandler] Handler took %v (threshold: %v)", duration, config.Threshold)
		}
	}

	return func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			start := time.Now()

			// 执行处理器
			err := next(ctx)

			// 计算耗时
			duration := time.Since(start)

			// 检查是否超过阈值
			if duration > config.Threshold {
				// 获取处理器名称（使用事件类型作为标识）
				handlerName := string(ctx.GetEventType())

				// 记录日志
				config.Logger(handlerName, duration, ctx)

				// 调用回调
				if config.OnSlowHandler != nil {
					config.OnSlowHandler(handlerName, duration, ctx)
				}
			}

			return err
		}
	}
}

// SlowHandlerSimple 创建简单的慢处理器检测中间件
// 使用默认配置，阈值为 1 秒
//
// 使用示例:
//
//	engine.Use(middleware.SlowHandlerSimple(2 * time.Second))
func SlowHandlerSimple(threshold time.Duration) remilia.HandlerMiddleware {
	return SlowHandler(SlowHandlerConfig{
		Threshold: threshold,
	})
}
