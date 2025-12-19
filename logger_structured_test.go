package remilia

import (
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestNewLogger 测试创建日志记录器
func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-component")

	assert.NotNil(t, logger)
	assert.NotNil(t, logger.entry)

	// 验证字段
	assert.Equal(t, "test-component", logger.entry.Data[LogFieldComponent])
}

// TestLoggerWithContext 测试添加 Context 字段
func TestLoggerWithContext(t *testing.T) {
	logger := NewLogger("test")

	// 创建测试 Context
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-id",
	}
	ctx := NewContext(event, nil)

	// 添加 Context 字段
	ctxLogger := logger.WithContext(ctx)

	// 验证字段
	assert.Equal(t, "test-event-id", ctxLogger.entry.Data[LogFieldEventID])
	assert.Equal(t, dto.C2CMessageCreate, ctxLogger.entry.Data[LogFieldEventType])
}

// TestLoggerWithContextNil 测试 nil Context
func TestLoggerWithContextNil(t *testing.T) {
	logger := NewLogger("test")
	ctxLogger := logger.WithContext(nil)

	// 应该返回原始 logger
	assert.NotNil(t, ctxLogger)
}

// TestLoggerWithFields 测试添加自定义字段
func TestLoggerWithFields(t *testing.T) {
	logger := NewLogger("test")

	fields := logrus.Fields{
		"custom_field": "custom_value",
		"count":        42,
	}

	fieldLogger := logger.WithFields(fields)

	assert.Equal(t, "custom_value", fieldLogger.entry.Data["custom_field"])
	assert.Equal(t, 42, fieldLogger.entry.Data["count"])
}

// TestLoggerWithField 测试添加单个字段
func TestLoggerWithField(t *testing.T) {
	logger := NewLogger("test")
	fieldLogger := logger.WithField("key", "value")

	assert.Equal(t, "value", fieldLogger.entry.Data["key"])
}

// TestLoggerWithError 测试添加错误字段
func TestLoggerWithError(t *testing.T) {
	logger := NewLogger("test")
	err := errors.New("test error")

	errLogger := logger.WithError(err)

	assert.Equal(t, err, errLogger.entry.Data[logrus.ErrorKey])
}

// TestLoggerWithErrorNil 测试添加 nil 错误
func TestLoggerWithErrorNil(t *testing.T) {
	logger := NewLogger("test")
	errLogger := logger.WithError(nil)

	// 不应该添加错误字段
	assert.NotContains(t, errLogger.entry.Data, logrus.ErrorKey)
}

// TestLoggerWithLatency 测试添加延迟字段
func TestLoggerWithLatency(t *testing.T) {
	logger := NewLogger("test")
	latency := 250 * time.Millisecond

	latencyLogger := logger.WithLatency(latency)

	assert.Equal(t, int64(250), latencyLogger.entry.Data[LogFieldLatency])
}

// TestLoggerWithMatcher 测试添加 Matcher 字段
func TestLoggerWithMatcher(t *testing.T) {
	logger := NewLogger("test")

	matcher := &Matcher{
		Source:   "test-matcher",
		priority: 10,
	}

	matcherLogger := logger.WithMatcher(matcher)

	assert.Equal(t, "test-matcher", matcherLogger.entry.Data[LogFieldMatcher])
	assert.Equal(t, uint(10), matcherLogger.entry.Data[LogFieldPriority])
}

// TestLoggerWithMatcherNil 测试 nil Matcher
func TestLoggerWithMatcherNil(t *testing.T) {
	logger := NewLogger("test")
	matcherLogger := logger.WithMatcher(nil)

	assert.NotContains(t, matcherLogger.entry.Data, LogFieldMatcher)
	assert.NotContains(t, matcherLogger.entry.Data, LogFieldPriority)
}

// TestLoggerWithPlugin 测试添加插件字段
func TestLoggerWithPlugin(t *testing.T) {
	logger := NewLogger("test")
	pluginLogger := logger.WithPlugin("test-plugin")

	assert.Equal(t, "test-plugin", pluginLogger.entry.Data[LogFieldPlugin])
}

// TestLoggerWithAction 测试添加操作字段
func TestLoggerWithAction(t *testing.T) {
	logger := NewLogger("test")
	actionLogger := logger.WithAction("create")

	assert.Equal(t, "create", actionLogger.entry.Data[LogFieldAction])
}

// TestLoggerWithStatus 测试添加状态字段
func TestLoggerWithStatus(t *testing.T) {
	logger := NewLogger("test")
	statusLogger := logger.WithStatus("success")

	assert.Equal(t, "success", statusLogger.entry.Data[LogFieldStatus])
}

// TestLoggerChaining 测试链式调用
func TestLoggerChaining(t *testing.T) {
	logger := NewLogger("test")

	chainedLogger := logger.
		WithField("field1", "value1").
		WithField("field2", "value2").
		WithAction("update").
		WithStatus("processing")

	assert.Equal(t, "value1", chainedLogger.entry.Data["field1"])
	assert.Equal(t, "value2", chainedLogger.entry.Data["field2"])
	assert.Equal(t, "update", chainedLogger.entry.Data[LogFieldAction])
	assert.Equal(t, "processing", chainedLogger.entry.Data[LogFieldStatus])
}

// TestLoggerLevels 测试日志级别方法
func TestLoggerLevels(t *testing.T) {
	logger := NewLogger("test")

	// 这些方法不应该 panic
	assert.NotPanics(t, func() {
		logger.Debug("debug message")
		logger.Debugf("debug %s", "formatted")
		logger.Info("info message")
		logger.Infof("info %s", "formatted")
		logger.Warn("warn message")
		logger.Warnf("warn %s", "formatted")
		logger.Error("error message")
		logger.Errorf("error %s", "formatted")
	})
}

// TestGlobalLoggers 测试全局日志实例
func TestGlobalLoggers(t *testing.T) {
	assert.NotNil(t, GetEngineLogger())
	assert.NotNil(t, GetContextLogger())
	assert.NotNil(t, GetMatcherLogger())
	assert.NotNil(t, GetPluginLogger())
	assert.NotNil(t, GetMiddlewareLogger())
	assert.NotNil(t, GetBotLogger())
	assert.NotNil(t, GetDeadLetterLogger())

	// 验证组件名称
	assert.Equal(t, "engine", GetEngineLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "context", GetContextLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "matcher", GetMatcherLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "plugin", GetPluginLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "middleware", GetMiddlewareLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "bot", GetBotLogger().entry.Data[LogFieldComponent])
	assert.Equal(t, "deadletter", GetDeadLetterLogger().entry.Data[LogFieldComponent])
}

// TestLogFieldConstants 测试日志字段常量
func TestLogFieldConstants(t *testing.T) {
	// 确保常量已定义
	assert.NotEmpty(t, LogFieldComponent)
	assert.NotEmpty(t, LogFieldSource)
	assert.NotEmpty(t, LogFieldEventID)
	assert.NotEmpty(t, LogFieldEventType)
	assert.NotEmpty(t, LogFieldUserID)
	assert.NotEmpty(t, LogFieldGuildID)
	assert.NotEmpty(t, LogFieldChannelID)
	assert.NotEmpty(t, LogFieldRequestID)
	assert.NotEmpty(t, LogFieldLatency)
	assert.NotEmpty(t, LogFieldAttempt)
	assert.NotEmpty(t, LogFieldMatcher)
	assert.NotEmpty(t, LogFieldPriority)
	assert.NotEmpty(t, LogFieldPlugin)
	assert.NotEmpty(t, LogFieldError)
	assert.NotEmpty(t, LogFieldAction)
	assert.NotEmpty(t, LogFieldStatus)
}

// TestLoggerWithContextFullFields 测试完整的 Context 字段
func TestLoggerWithContextFullFields(t *testing.T) {
	logger := NewLogger("test")

	// 创建带有所有字段的 Context
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "event-123",
	}
	ctx := NewContext(event, nil)

	// 设置 request_id
	ctx.SetState(LogFieldRequestID, "req-456")

	// 设置 matcher
	matcher := &Matcher{
		Source:   "test-source",
		priority: 20,
	}
	ctx.matcher = matcher

	// 添加 Context 字段
	ctxLogger := logger.WithContext(ctx)

	// 验证所有字段
	assert.Equal(t, "event-123", ctxLogger.entry.Data[LogFieldEventID])
	assert.Equal(t, dto.C2CMessageCreate, ctxLogger.entry.Data[LogFieldEventType])
	assert.Equal(t, "req-456", ctxLogger.entry.Data[LogFieldRequestID])
	assert.Equal(t, "test-source", ctxLogger.entry.Data[LogFieldMatcher])
	assert.Equal(t, uint(20), ctxLogger.entry.Data[LogFieldPriority])
}

// BenchmarkNewLogger 基准测试创建日志记录器
func BenchmarkNewLogger(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewLogger("test")
	}
}

// BenchmarkLoggerWithContext 基准测试添加 Context
func BenchmarkLoggerWithContext(b *testing.B) {
	logger := NewLogger("test")
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event",
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithContext(ctx)
	}
}

// BenchmarkLoggerWithFields 基准测试添加字段
func BenchmarkLoggerWithFields(b *testing.B) {
	logger := NewLogger("test")
	fields := logrus.Fields{
		"field1": "value1",
		"field2": "value2",
		"field3": "value3",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithFields(fields)
	}
}

// BenchmarkLoggerChaining 基准测试链式调用
func BenchmarkLoggerChaining(b *testing.B) {
	logger := NewLogger("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.
			WithField("field1", "value1").
			WithField("field2", "value2").
			WithAction("test").
			WithStatus("ok")
	}
}
