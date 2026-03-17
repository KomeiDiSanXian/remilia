package middleware

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// DeadLetter 创建死信队列中间件
// 当后续 Handler 返回错误时，自动将事件投递到死信队列
//
// 注意：建议将此中间件放在重试中间件（Retry）的外层，
// 这样只有在重试耗尽并最终返回错误时，才会进入死信队列。
func DeadLetter(q *dlq.PayloadQueue) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			err := next(ctx)
			if err != nil && q != nil {
				source := ctx.GetMatcherSource()
				attempts, _ := ctx.GetRetryAttempt()

				item := dlq.PayloadItem{
					Data:    ctx.GetEvent(),
					Err:     err,
					Source:  source,
					Attempt: attempts,
				}

				_ = q.Enqueue(item)
				logger.WithError(err).WithFields(logger.Fields{
					"event_id": item.Data.ID,
					"source":   source,
				}).Warn("[DeadLetter] Event sent to dead letter queue")
			}
			return err
		}
	}
}
