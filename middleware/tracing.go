package middleware

import (
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
)

// TracingConfig 追踪中间件配置
type TracingConfig struct {
	// TracerName Tracer 名称
	TracerName string

	// SpanNameFunc 自定义 Span 名称函数
	SpanNameFunc func(*context.Context) string

	// IncludeEventDetail 是否包含事件详情（可能包含敏感信息）
	IncludeEventDetail bool

	// MaxContentLength 事件内容最大长度（防止 span 过大）
	MaxContentLength int
}

// DefaultTracingConfig 返回默认配置
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{
		TracerName:         "remilia",
		IncludeEventDetail: false,
		MaxContentLength:   200,
		SpanNameFunc: func(ctx *context.Context) string {
			eventType := ctx.GetEventType()
			if eventType == "" {
				return "event.process"
			}
			return fmt.Sprintf("event.%s", eventType)
		},
	}
}

// Tracing 创建追踪中间件
//
// 使用示例：
//
//	// 基本使用
//	engine.Use(middleware.Tracing(middleware.DefaultTracingConfig()))
//
//	// 自定义配置
//	config := middleware.TracingConfig{
//		TracerName: "my-bot",
//		IncludeEventDetail: true,
//		SpanNameFunc: func(ctx *context.Context) string {
//			return fmt.Sprintf("cmd.%s", ctx.GetString("command"))
//		},
//	}
//	engine.Use(middleware.Tracing(config))
func Tracing(config TracingConfig) context.Middleware {
	tracer := otel.Tracer(config.TracerName)

	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			// 从 context 中提取 trace context（如果存在）
			stdCtx := ctx.Context()

			// 生成 span 名称
			spanName := "event.process"
			if config.SpanNameFunc != nil {
				spanName = config.SpanNameFunc(ctx)
			}

			// 开始 span
			newCtx, span := tracer.Start(stdCtx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			// 注入 trace context 到 remilia context
			ctx.SetStdContext(newCtx)

			// 设置基本属性
			attrs := []attribute.KeyValue{
				attribute.String(tracing.AttrEventType, ctx.GetEventType()),
			}

			// 添加事件 ID（平台无关路径）
			if pe := ctx.GetPlatformEvent(); pe != nil && pe.ID() != "" {
				attrs = append(attrs, attribute.String(tracing.AttrEventID, pe.ID()))
			}

			// 添加用户信息
			if senderID := ctx.GetSenderInfo().ID; senderID != "" {
				attrs = append(attrs, attribute.String(tracing.AttrUserID, senderID))
			}

			// 添加 matcher 信息
			if source := ctx.GetMatcherSource(); source != "" {
				attrs = append(attrs, attribute.String(tracing.AttrMatcherSource, source))
			}

			// 添加事件内容（可选，可能包含敏感信息）
			if config.IncludeEventDetail {
				content := ctx.GetMessageContent()
				if len(content) > config.MaxContentLength {
					content = content[:config.MaxContentLength] + "..."
				}
				if content != "" {
					attrs = append(attrs, attribute.String(tracing.AttrEventContent, content))
				}
			}

			span.SetAttributes(attrs...)

			// 记录开始事件
			span.AddEvent("event.received")

			// 执行下一个处理器
			startTime := time.Now()
			err := next(ctx)
			duration := time.Since(startTime)

			// 记录执行时间
			span.SetAttributes(
				attribute.Float64(tracing.AttrDuration, float64(duration.Milliseconds())),
			)

			// 处理错误
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.SetAttributes(tracing.ErrorAttributes(err)...)

				logger.WithError(err).WithFields(logger.Fields{
					"trace_id": span.SpanContext().TraceID().String(),
					"span_id":  span.SpanContext().SpanID().String(),
					"duration": duration,
				}).Error("[Tracing] Event processing failed")
			} else {
				span.SetStatus(codes.Ok, "")
				span.AddEvent("event.processed")
			}

			return err
		}
	}
}

// TracingNamed 创建命名的追踪中间件（用于追踪特定的中间件）
//
// 使用示例：
//
//	func MyMiddleware() context.Middleware {
//		return TracingNamed("my-middleware", func(next context.Handler) context.Handler {
//			return func(ctx *context.Context) error {
//				// 中间件逻辑
//				return next(ctx)
//			}
//		})
//	}
func TracingNamed(name string, mw context.Middleware) context.Middleware {
	tracer := otel.Tracer("remilia.middleware")

	return func(next context.Handler) context.Handler {
		wrapped := mw(next)
		return func(ctx *context.Context) error {
			stdCtx := ctx.Context()

			// 创建子 span
			newCtx, span := tracer.Start(stdCtx, fmt.Sprintf("middleware.%s", name))
			defer span.End()

			ctx.SetStdContext(newCtx)

			span.SetAttributes(
				attribute.String(tracing.AttrMiddlewareName, name),
			)

			startTime := time.Now()
			err := wrapped(ctx)
			duration := time.Since(startTime)

			span.SetAttributes(
				attribute.Float64(tracing.AttrDuration, float64(duration.Milliseconds())),
			)

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return err
		}
	}
}

// TracingHandler 为 Handler 添加追踪（用于追踪特定的命令处理器）
//
// 使用示例：
//
//	engine.OnCommand("/ping").Handle(
//		middleware.TracingHandler("ping", func(ctx *context.Context) error {
//			return ctx.Reply("Pong!")
//		}),
//	)
func TracingHandler(name string, handler context.Handler) context.Handler {
	tracer := otel.Tracer("remilia.handler")

	return func(ctx *context.Context) error {
		stdCtx := ctx.Context()

		// 创建子 span
		newCtx, span := tracer.Start(stdCtx, fmt.Sprintf("handler.%s", name))
		defer span.End()

		ctx.SetStdContext(newCtx)

		span.SetAttributes(
			attribute.String("remilia.handler.name", name),
		)

		// 如果是命令，添加命令信息
		if cmd := ctx.GetParsedCommand(); cmd != nil {
			// 使用 CommandPath 的第一个元素作为命令名
			if len(cmd.CommandPath) > 0 {
				span.SetAttributes(
					attribute.String(tracing.AttrCommandName, cmd.CommandPath[0]),
				)
			}
			// 如果有 Arguments，将其键列表作为参数
			if len(cmd.Arguments) > 0 {
				args := make([]string, 0, len(cmd.Arguments))
				for k := range cmd.Arguments {
					args = append(args, k)
				}
				span.SetAttributes(
					attribute.StringSlice(tracing.AttrCommandArgs, args),
				)
			}
		}

		startTime := time.Now()
		err := handler(ctx)
		duration := time.Since(startTime)

		span.SetAttributes(
			attribute.Float64(tracing.AttrDuration, float64(duration.Milliseconds())),
		)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// GetTraceID 获取当前 trace ID（用于日志关联）
func GetTraceID(ctx *context.Context) string {
	if ctx == nil {
		return ""
	}
	span := trace.SpanFromContext(ctx.Context())
	if span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// GetSpanID 获取当前 span ID
func GetSpanID(ctx *context.Context) string {
	if ctx == nil {
		return ""
	}
	span := trace.SpanFromContext(ctx.Context())
	if span.SpanContext().IsValid() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}
