package main

import (
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// requestCounterMiddleware 每收到一个事件自增计数，用于调试。
func requestCounterMiddleware() eventctx.Middleware {
	var count int64
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			count++
			logger.Debugf("[showcase] req #%d", count)
			return next(ctx)
		}
	}
}

// processingTimeMiddleware 记录每个事件的处理器总耗时。
func processingTimeMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			elapsed := time.Since(start)
			cmd := ctx.GetMessageContent()
			if len(cmd) > 30 {
				cmd = cmd[:30] + "..."
			}
			logger.Infof("[perf] user=%s cmd=%q total=%v",
				ctx.GetUserID(), cmd, elapsed)
			return err
		}
	}
}
