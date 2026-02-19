package audit

import (
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
)

// Middleware 创建审计日志中间件
//
// 此中间件会自动记录所有事件处理的审计日志。
//
// 使用示例:
//
//	auditLogger, _ := audit.NewLogger(config)
//	engine.Use(audit.Middleware(auditLogger))
func Middleware(logger *Logger) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			start := time.Now()

			// 提取用户信息
			author := ctx.GetAuthor()
			actor := "anonymous"
			if author != nil && author.UserOpenID != "" {
				actor = author.UserOpenID
			}

			// 提取命令信息
			var command string
			if parsed := ctx.GetParsedCommand(); parsed != nil {
				// 使用 CommandPath 的第一个元素作为命令名，或使用 Raw
				if len(parsed.CommandPath) > 0 {
					command = strings.Join(parsed.CommandPath, " ")
				} else {
					command = parsed.Raw
				}
			}

			// 执行处理器
			err := next(ctx)
			duration := time.Since(start)

			// 记录审计日志
			if command != "" {
				logger.LogCommandExecution(actor, command, err == nil, duration, err)
			} else {
				// 记录一般事件处理
				// 从 event 中提取 channel_id 和 guild_id
				metadata := map[string]any{
					"event_type": string(ctx.GetEventType()),
				}

				// 尝试从事件中提取更多信息
				event := ctx.GetEvent()
				if event != nil {
					// 可以根据需要添加更多字段
					metadata["event_id"] = event.ID
				}

				entry := &Entry{
					Level:    LevelInfo,
					Action:   Action("event." + string(ctx.GetEventType())),
					Actor:    actor,
					Result:   "success",
					Duration: duration.Milliseconds(),
					Metadata: metadata,
				}

				if err != nil {
					entry.Level = LevelError
					entry.Result = "failure"
					entry.Error = err.Error()
				}

				logger.Log(entry)
			}

			return err
		}
	}
}

// CommandMiddleware 创建命令级别的审计中间件
func CommandMiddleware(logger *Logger, commandName string) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			start := time.Now()

			author := ctx.GetAuthor()
			actor := "anonymous"
			if author != nil && author.UserOpenID != "" {
				actor = author.UserOpenID
			}

			err := next(ctx)
			duration := time.Since(start)

			logger.LogCommandExecution(actor, commandName, err == nil, duration, err)

			return err
		}
	}
}
