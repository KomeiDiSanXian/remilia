package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SpanHelper 追踪辅助工具
type SpanHelper struct {
	span trace.Span
}

// StartSpan 开始一个新的 span
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, *SpanHelper) {
	ctx, span := tracer.Start(ctx, name, opts...)
	return ctx, &SpanHelper{span: span}
}

// SetAttributes 设置属性
func (h *SpanHelper) SetAttributes(attrs ...attribute.KeyValue) *SpanHelper {
	if h.span != nil {
		h.span.SetAttributes(attrs...)
	}
	return h
}

// SetAttribute 设置单个属性
func (h *SpanHelper) SetAttribute(key string, value any) *SpanHelper {
	if h.span == nil {
		return h
	}

	switch v := value.(type) {
	case string:
		h.span.SetAttributes(attribute.String(key, v))
	case int:
		h.span.SetAttributes(attribute.Int(key, v))
	case int64:
		h.span.SetAttributes(attribute.Int64(key, v))
	case bool:
		h.span.SetAttributes(attribute.Bool(key, v))
	case float64:
		h.span.SetAttributes(attribute.Float64(key, v))
	default:
		h.span.SetAttributes(attribute.String(key, fmt.Sprintf("%v", v)))
	}
	return h
}

// RecordError 记录错误
func (h *SpanHelper) RecordError(err error, opts ...trace.EventOption) *SpanHelper {
	if h.span != nil && err != nil {
		h.span.RecordError(err, opts...)
		h.span.SetStatus(codes.Error, err.Error())
	}
	return h
}

// SetStatus 设置状态
func (h *SpanHelper) SetStatus(code codes.Code, description string) *SpanHelper {
	if h.span != nil {
		h.span.SetStatus(code, description)
	}
	return h
}

// AddEvent 添加事件
func (h *SpanHelper) AddEvent(name string, opts ...trace.EventOption) *SpanHelper {
	if h.span != nil {
		h.span.AddEvent(name, opts...)
	}
	return h
}

// End 结束 span
func (h *SpanHelper) End(opts ...trace.SpanEndOption) {
	if h.span != nil {
		h.span.End(opts...)
	}
}

// Span 获取底层 span
func (h *SpanHelper) Span() trace.Span {
	return h.span
}

// 预定义的属性键
const (
	AttrEventID      = "remilia.event.id"
	AttrEventType    = "remilia.event.type"
	AttrEventContent = "remilia.event.content"
	AttrUserID       = "remilia.event.user_id"
	AttrGuildID      = "remilia.event.guild_id"
	AttrChannelID    = "remilia.event.channel_id"

	AttrMatcherName     = "remilia.matcher.name"
	AttrMatcherPriority = "remilia.matcher.priority"
	AttrMatcherSource   = "remilia.matcher.source"
	AttrMatcherGroup    = "remilia.matcher.group"

	AttrMiddlewareName = "remilia.middleware.name"
	AttrMiddlewareType = "remilia.middleware.type"

	AttrCommandName = "remilia.command.name"
	AttrCommandArgs = "remilia.command.args"

	AttrErrorType    = "remilia.error.type"
	AttrErrorMessage = "remilia.error.message"
	AttrErrorStack   = "remilia.error.stack"

	AttrDuration   = "remilia.duration_ms"
	AttrRetryCount = "remilia.retry.count"
	AttrQueueDepth = "remilia.queue.depth"
)

// EventAttributes 从事件创建属性
func EventAttributes(eventType, eventID, userID string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(AttrEventType, eventType),
	}
	if eventID != "" {
		attrs = append(attrs, attribute.String(AttrEventID, eventID))
	}
	if userID != "" {
		attrs = append(attrs, attribute.String(AttrUserID, userID))
	}
	return attrs
}

// MatcherAttributes 从匹配器创建属性
func MatcherAttributes(name, source, group string, priority uint) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(AttrMatcherName, name),
		attribute.String(AttrMatcherSource, source),
		attribute.String(AttrMatcherGroup, group),
		attribute.Int(AttrMatcherPriority, int(priority)),
	}
}

// MiddlewareAttributes 从中间件创建属性
func MiddlewareAttributes(name, mwType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(AttrMiddlewareName, name),
		attribute.String(AttrMiddlewareType, mwType),
	}
}

// ErrorAttributes 从错误创建属性
func ErrorAttributes(err error) []attribute.KeyValue {
	if err == nil {
		return nil
	}
	return []attribute.KeyValue{
		attribute.String(AttrErrorMessage, err.Error()),
		attribute.String(AttrErrorType, fmt.Sprintf("%T", err)),
	}
}
