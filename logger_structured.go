package remilia

import (
	"time"

	"github.com/sirupsen/logrus"
)

// 统一的日志字段名称
const (
	// 组件相关
	LogFieldComponent = "component" // 组件名称
	LogFieldSource    = "source"    // 来源（global/plugin）

	// 事件相关
	LogFieldEventID   = "event_id"   // 事件 ID
	LogFieldEventType = "event_type" // 事件类型
	LogFieldUserID    = "user_id"    // 用户 ID
	LogFieldGuildID   = "guild_id"   // 群组 ID
	LogFieldChannelID = "channel_id" // 频道 ID

	// 请求相关
	LogFieldRequestID = "request_id" // 请求 ID
	LogFieldLatency   = "latency"    // 延迟（毫秒）
	LogFieldAttempt   = "attempt"    // 尝试次数

	// Matcher 相关
	LogFieldMatcher  = "matcher"  // Matcher 名称
	LogFieldPriority = "priority" // 优先级

	// Plugin 相关
	LogFieldPlugin = "plugin" // 插件名称

	// 错误相关
	LogFieldError      = "error"       // 错误信息
	LogFieldErrorType  = "error_type"  // 错误类型
	LogFieldStackTrace = "stack_trace" // 堆栈跟踪

	// 性能相关
	LogFieldCacheSize = "cache_size" // 缓存大小
	LogFieldCacheHit  = "cache_hit"  // 缓存命中
	LogFieldQueueSize = "queue_size" // 队列大小

	// 其他
	LogFieldAction = "action" // 操作动作
	LogFieldStatus = "status" // 状态
	LogFieldReason = "reason" // 原因
)

// StructuredLogger 结构化日志记录器
//
// 提供统一的日志接口和字段管理，便于日志查询和分析。
type StructuredLogger struct {
	entry *logrus.Entry
}

// NewLogger 创建新的日志记录器
//
// 使用示例：
//
//	logger := NewLogger("engine")
//	logger.Info("engine started")
func NewLogger(component string) *StructuredLogger {
	return &StructuredLogger{
		entry: logrus.WithField(LogFieldComponent, component),
	}
}

// WithContext 添加 Context 相关字段
//
// 自动提取事件 ID、类型等信息。
func (l *StructuredLogger) WithContext(ctx *Context) *StructuredLogger {
	if ctx == nil {
		return l
	}

	fields := logrus.Fields{}

	// 访问 event 字段
	if ctx.event != nil {
		// 事件 ID
		if ctx.event.ID != "" {
			fields[LogFieldEventID] = string(ctx.event.ID)
		}

		// 事件类型
		if ctx.event.Type != "" {
			fields[LogFieldEventType] = ctx.event.Type
		}
	}

	// 请求 ID（如果有）
	if reqID, ok := ctx.GetState(LogFieldRequestID); ok {
		if reqIDStr, ok := reqID.(string); ok {
			fields[LogFieldRequestID] = reqIDStr
		}
	}

	// Matcher 信息
	if ctx.matcher != nil {
		fields[LogFieldMatcher] = ctx.matcher.Source
		fields[LogFieldPriority] = ctx.matcher.priority
	}

	return &StructuredLogger{
		entry: l.entry.WithFields(fields),
	}
}

// WithFields 添加自定义字段
func (l *StructuredLogger) WithFields(fields logrus.Fields) *StructuredLogger {
	return &StructuredLogger{
		entry: l.entry.WithFields(fields),
	}
}

// WithField 添加单个字段
func (l *StructuredLogger) WithField(key string, value interface{}) *StructuredLogger {
	return &StructuredLogger{
		entry: l.entry.WithField(key, value),
	}
}

// WithError 添加错误字段
func (l *StructuredLogger) WithError(err error) *StructuredLogger {
	if err == nil {
		return l
	}
	return &StructuredLogger{
		entry: l.entry.WithError(err),
	}
}

// WithLatency 添加延迟字段（自动转换为毫秒）
func (l *StructuredLogger) WithLatency(duration time.Duration) *StructuredLogger {
	return l.WithField(LogFieldLatency, duration.Milliseconds())
}

// WithMatcher 添加 Matcher 相关字段
func (l *StructuredLogger) WithMatcher(matcher *Matcher) *StructuredLogger {
	if matcher == nil {
		return l
	}
	return l.WithFields(logrus.Fields{
		LogFieldMatcher:  matcher.Source,
		LogFieldPriority: matcher.priority,
	})
}

// WithPlugin 添加插件相关字段
func (l *StructuredLogger) WithPlugin(pluginName string) *StructuredLogger {
	return l.WithField(LogFieldPlugin, pluginName)
}

// WithAction 添加操作字段
func (l *StructuredLogger) WithAction(action string) *StructuredLogger {
	return l.WithField(LogFieldAction, action)
}

// WithStatus 添加状态字段
func (l *StructuredLogger) WithStatus(status string) *StructuredLogger {
	return l.WithField(LogFieldStatus, status)
}

// 日志级别方法

func (l *StructuredLogger) Debug(msg string) {
	l.entry.Debug(msg)
}

func (l *StructuredLogger) Debugf(format string, args ...interface{}) {
	l.entry.Debugf(format, args...)
}

func (l *StructuredLogger) Info(msg string) {
	l.entry.Info(msg)
}

func (l *StructuredLogger) Infof(format string, args ...interface{}) {
	l.entry.Infof(format, args...)
}

func (l *StructuredLogger) Warn(msg string) {
	l.entry.Warn(msg)
}

func (l *StructuredLogger) Warnf(format string, args ...interface{}) {
	l.entry.Warnf(format, args...)
}

func (l *StructuredLogger) Error(msg string) {
	l.entry.Error(msg)
}

func (l *StructuredLogger) Errorf(format string, args ...interface{}) {
	l.entry.Errorf(format, args...)
}

func (l *StructuredLogger) Fatal(msg string) {
	l.entry.Fatal(msg)
}

func (l *StructuredLogger) Fatalf(format string, args ...interface{}) {
	l.entry.Fatalf(format, args...)
}

// 全局日志实例（按组件分类）
var (
	engineLogger     = NewLogger("engine")
	contextLogger    = NewLogger("context")
	matcherLogger    = NewLogger("matcher")
	pluginLogger     = NewLogger("plugin")
	middlewareLogger = NewLogger("middleware")
	botLogger        = NewLogger("bot")
	deadLetterLogger = NewLogger("deadletter")
)

// GetEngineLogger 获取引擎日志记录器
func GetEngineLogger() *StructuredLogger {
	return engineLogger
}

// GetContextLogger 获取 Context 日志记录器
func GetContextLogger() *StructuredLogger {
	return contextLogger
}

// GetMatcherLogger 获取 Matcher 日志记录器
func GetMatcherLogger() *StructuredLogger {
	return matcherLogger
}

// GetPluginLogger 获取插件日志记录器
func GetPluginLogger() *StructuredLogger {
	return pluginLogger
}

// GetMiddlewareLogger 获取中间件日志记录器
func GetMiddlewareLogger() *StructuredLogger {
	return middlewareLogger
}

// GetBotLogger 获取 Bot 日志记录器
func GetBotLogger() *StructuredLogger {
	return botLogger
}

// GetDeadLetterLogger 获取死信日志记录器
func GetDeadLetterLogger() *StructuredLogger {
	return deadLetterLogger
}
