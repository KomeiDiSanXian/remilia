package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLogger tests creating a new logger
func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-component")
	require.NotNil(t, logger)
	require.NotNil(t, logger.entry)

	// Verify component field is set
	assert.Contains(t, logger.entry.Data, LogFieldComponent)
	assert.Equal(t, "test-component", logger.entry.Data[LogFieldComponent])
}

// TestStructuredLogger_WithField tests adding a single field
func TestStructuredLogger_WithField(t *testing.T) {
	logger := NewLogger("test")
	newLogger := logger.WithField("key", "value")

	assert.NotNil(t, newLogger)
	assert.Contains(t, newLogger.entry.Data, "key")
	assert.Equal(t, "value", newLogger.entry.Data["key"])

	// Original logger should not be modified
	assert.NotContains(t, logger.entry.Data, "key")
}

// TestStructuredLogger_WithFields tests adding multiple fields
func TestStructuredLogger_WithFields(t *testing.T) {
	logger := NewLogger("test")
	fields := logrus.Fields{
		"field1": "value1",
		"field2": 123,
		"field3": true,
	}

	newLogger := logger.WithFields(fields)

	assert.NotNil(t, newLogger)
	assert.Equal(t, "value1", newLogger.entry.Data["field1"])
	assert.Equal(t, 123, newLogger.entry.Data["field2"])
	assert.Equal(t, true, newLogger.entry.Data["field3"])
}

// TestStructuredLogger_WithError tests adding error field
func TestStructuredLogger_WithError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		hasError bool
	}{
		{
			name:     "with error",
			err:      errors.New("test error"),
			hasError: true,
		},
		{
			name:     "with nil error",
			err:      nil,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger("test")
			newLogger := logger.WithError(tt.err)

			assert.NotNil(t, newLogger)
			if tt.hasError {
				assert.Contains(t, newLogger.entry.Data, logrus.ErrorKey)
			} else {
				// Nil error should return same logger
				assert.NotContains(t, newLogger.entry.Data, logrus.ErrorKey)
			}
		})
	}
}

// TestStructuredLogger_WithLatency tests adding latency field
func TestStructuredLogger_WithLatency(t *testing.T) {
	logger := NewLogger("test")
	duration := 150 * time.Millisecond

	newLogger := logger.WithLatency(duration)

	assert.NotNil(t, newLogger)
	assert.Contains(t, newLogger.entry.Data, LogFieldLatency)
	assert.Equal(t, int64(150), newLogger.entry.Data[LogFieldLatency])
}

// TestStructuredLogger_WithMatcher tests adding matcher fields
func TestStructuredLogger_WithMatcher(t *testing.T) {
	t.Run("with valid matcher", func(t *testing.T) {
		logger := NewLogger("test")
		matcher := &engine.Matcher{
			Source: "test-matcher",
		}

		newLogger := logger.WithMatcher(matcher)

		assert.NotNil(t, newLogger)
		assert.Contains(t, newLogger.entry.Data, LogFieldMatcher)
		assert.Equal(t, "test-matcher", newLogger.entry.Data[LogFieldMatcher])
	})

	t.Run("with nil matcher", func(t *testing.T) {
		logger := NewLogger("test")
		newLogger := logger.WithMatcher(nil)

		assert.NotNil(t, newLogger)
		assert.NotContains(t, newLogger.entry.Data, LogFieldMatcher)
	})
}

// TestStructuredLogger_WithPlugin tests adding plugin field
func TestStructuredLogger_WithPlugin(t *testing.T) {
	logger := NewLogger("test")
	newLogger := logger.WithPlugin("test-plugin")

	assert.NotNil(t, newLogger)
	assert.Contains(t, newLogger.entry.Data, LogFieldPlugin)
	assert.Equal(t, "test-plugin", newLogger.entry.Data[LogFieldPlugin])
}

// TestStructuredLogger_WithAction tests adding action field
func TestStructuredLogger_WithAction(t *testing.T) {
	logger := NewLogger("test")
	newLogger := logger.WithAction("process")

	assert.NotNil(t, newLogger)
	assert.Contains(t, newLogger.entry.Data, LogFieldAction)
	assert.Equal(t, "process", newLogger.entry.Data[LogFieldAction])
}

// TestStructuredLogger_WithStatus tests adding status field
func TestStructuredLogger_WithStatus(t *testing.T) {
	logger := NewLogger("test")
	newLogger := logger.WithStatus("success")

	assert.NotNil(t, newLogger)
	assert.Contains(t, newLogger.entry.Data, LogFieldStatus)
	assert.Equal(t, "success", newLogger.entry.Data[LogFieldStatus])
}

// TestStructuredLogger_WithContext tests adding context fields
func TestStructuredLogger_WithContext(t *testing.T) {
	t.Run("with nil context", func(t *testing.T) {
		logger := NewLogger("test")
		newLogger := logger.WithContext(nil)

		assert.NotNil(t, newLogger)
		// Should return logger without changes
	})

	t.Run("with context containing event", func(t *testing.T) {
		logger := NewLogger("test")

		event := &dto.Payload{
			ID:   "event-123",
			Type: "MESSAGE_CREATE",
		}

		ctx := context.NewContext(event, nil)
		newLogger := logger.WithContext(ctx)

		assert.NotNil(t, newLogger)
		assert.Contains(t, newLogger.entry.Data, LogFieldEventID)
		assert.Equal(t, "event-123", newLogger.entry.Data[LogFieldEventID])
		assert.Contains(t, newLogger.entry.Data, LogFieldEventType)
		eventType := newLogger.entry.Data[LogFieldEventType].(dto.EventType)
		assert.Equal(t, "MESSAGE_CREATE", string(eventType))
	})

	t.Run("with context containing matcher source", func(t *testing.T) {
		// Skip: matcher source is set internally by engine
		// Cannot be directly tested without full engine setup
		t.Skip("Matcher source requires engine integration")
	})

	t.Run("with context containing request ID", func(t *testing.T) {
		logger := NewLogger("test")

		event := &dto.Payload{ID: "test"}
		ctx := context.NewContext(event, nil)
		ctx.Set(LogFieldRequestID, "req-123")

		newLogger := logger.WithContext(ctx)

		assert.Contains(t, newLogger.entry.Data, LogFieldRequestID)
		assert.Equal(t, "req-123", newLogger.entry.Data[LogFieldRequestID])
	})
}

// TestStructuredLogger_LogLevels tests different log levels
func TestStructuredLogger_LogLevels(t *testing.T) {
	// Set logrus to JSON format for easier parsing
	oldFormatter := logrus.StandardLogger().Formatter
	oldOutput := logrus.StandardLogger().Out
	oldLevel := logrus.StandardLogger().Level

	defer func() {
		logrus.StandardLogger().Formatter = oldFormatter
		logrus.StandardLogger().Out = oldOutput
		logrus.StandardLogger().Level = oldLevel
	}()

	var buf bytes.Buffer
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)

	logger := NewLogger("test-component")

	tests := []struct {
		name    string
		logFunc func()
		level   string
		message string
	}{
		{
			name:    "Debug",
			logFunc: func() { logger.Debug("debug message") },
			level:   "debug",
			message: "debug message",
		},
		{
			name:    "Debugf",
			logFunc: func() { logger.Debugf("debug %s", "formatted") },
			level:   "debug",
			message: "debug formatted",
		},
		{
			name:    "Info",
			logFunc: func() { logger.Info("info message") },
			level:   "info",
			message: "info message",
		},
		{
			name:    "Infof",
			logFunc: func() { logger.Infof("info %d", 123) },
			level:   "info",
			message: "info 123",
		},
		{
			name:    "Warn",
			logFunc: func() { logger.Warn("warn message") },
			level:   "warning",
			message: "warn message",
		},
		{
			name:    "Warnf",
			logFunc: func() { logger.Warnf("warn %v", true) },
			level:   "warning",
			message: "warn true",
		},
		{
			name:    "Error",
			logFunc: func() { logger.Error("error message") },
			level:   "error",
			message: "error message",
		},
		{
			name:    "Errorf",
			logFunc: func() { logger.Errorf("error %s", "test") },
			level:   "error",
			message: "error test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			require.NoError(t, err)

			assert.Equal(t, tt.level, logEntry["level"])
			assert.Equal(t, tt.message, logEntry["msg"])
			assert.Equal(t, "test-component", logEntry[LogFieldComponent])
		})
	}
}

// TestStructuredLogger_Chaining tests method chaining
func TestStructuredLogger_Chaining(t *testing.T) {
	logger := NewLogger("test")

	newLogger := logger.
		WithField("field1", "value1").
		WithField("field2", "value2").
		WithAction("test-action").
		WithStatus("success")

	assert.NotNil(t, newLogger)
	assert.Equal(t, "value1", newLogger.entry.Data["field1"])
	assert.Equal(t, "value2", newLogger.entry.Data["field2"])
	assert.Equal(t, "test-action", newLogger.entry.Data[LogFieldAction])
	assert.Equal(t, "success", newLogger.entry.Data[LogFieldStatus])
}

// TestGlobalLoggers tests global logger instances
func TestGlobalLoggers(t *testing.T) {
	tests := []struct {
		name      string
		getLogger func() *StructuredLogger
		component string
	}{
		{
			name:      "GetEngineLogger",
			getLogger: GetEngineLogger,
			component: "engine",
		},
		{
			name:      "GetContextLogger",
			getLogger: GetContextLogger,
			component: "context",
		},
		{
			name:      "GetMatcherLogger",
			getLogger: GetMatcherLogger,
			component: "matcher",
		},
		{
			name:      "GetPluginLogger",
			getLogger: GetPluginLogger,
			component: "plugin",
		},
		{
			name:      "GetMiddlewareLogger",
			getLogger: GetMiddlewareLogger,
			component: "middleware",
		},
		{
			name:      "GetBotLogger",
			getLogger: GetBotLogger,
			component: "bot",
		},
		{
			name:      "GetDeadLetterLogger",
			getLogger: GetDeadLetterLogger,
			component: "deadletter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := tt.getLogger()
			require.NotNil(t, logger)
			assert.Equal(t, tt.component, logger.entry.Data[LogFieldComponent])
		})
	}
}

// TestLogFieldConstants tests that all log field constants are defined
func TestLogFieldConstants(t *testing.T) {
	constants := map[string]string{
		"LogFieldComponent":  LogFieldComponent,
		"LogFieldSource":     LogFieldSource,
		"LogFieldEventID":    LogFieldEventID,
		"LogFieldEventType":  LogFieldEventType,
		"LogFieldUserID":     LogFieldUserID,
		"LogFieldGuildID":    LogFieldGuildID,
		"LogFieldChannelID":  LogFieldChannelID,
		"LogFieldRequestID":  LogFieldRequestID,
		"LogFieldLatency":    LogFieldLatency,
		"LogFieldAttempt":    LogFieldAttempt,
		"LogFieldMatcher":    LogFieldMatcher,
		"LogFieldPriority":   LogFieldPriority,
		"LogFieldPlugin":     LogFieldPlugin,
		"LogFieldError":      LogFieldError,
		"LogFieldErrorType":  LogFieldErrorType,
		"LogFieldStackTrace": LogFieldStackTrace,
		"LogFieldCacheSize":  LogFieldCacheSize,
		"LogFieldCacheHit":   LogFieldCacheHit,
		"LogFieldQueueSize":  LogFieldQueueSize,
		"LogFieldAction":     LogFieldAction,
		"LogFieldStatus":     LogFieldStatus,
		"LogFieldReason":     LogFieldReason,
	}

	for name, value := range constants {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, value, "constant %s should not be empty", name)
		})
	}
}

// TestStructuredLogger_ImmutabilityPattern tests immutability pattern
func TestStructuredLogger_ImmutabilityPattern(t *testing.T) {
	original := NewLogger("test")

	// Add field to new logger
	modified := original.WithField("new_field", "value")

	// Original should not have the new field
	assert.NotContains(t, original.entry.Data, "new_field")

	// Modified should have the new field
	assert.Contains(t, modified.entry.Data, "new_field")

	// Both should have component field
	assert.Contains(t, original.entry.Data, LogFieldComponent)
	assert.Contains(t, modified.entry.Data, LogFieldComponent)
}

// TestStructuredLogger_ComplexScenario tests a complex logging scenario
func TestStructuredLogger_ComplexScenario(t *testing.T) {
	var buf bytes.Buffer
	oldFormatter := logrus.StandardLogger().Formatter
	oldOutput := logrus.StandardLogger().Out
	oldLevel := logrus.StandardLogger().Level

	defer func() {
		logrus.StandardLogger().Formatter = oldFormatter
		logrus.StandardLogger().Out = oldOutput
		logrus.StandardLogger().Level = oldLevel
	}()

	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.InfoLevel)

	// Create context with event
	event := &dto.Payload{
		ID:   "event-456",
		Type: "GROUP_AT_MESSAGE_CREATE",
	}
	ctx := context.NewContext(event, nil)
	ctx.Set(LogFieldRequestID, "req-789")

	// Create logger with multiple fields
	logger := NewLogger("api").
		WithContext(ctx).
		WithLatency(250 * time.Millisecond).
		WithAction("process_message").
		WithStatus("success")

	logger.Info("message processed successfully")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "message processed successfully", logEntry["msg"])
	assert.Equal(t, "api", logEntry[LogFieldComponent])
	assert.Equal(t, "event-456", logEntry[LogFieldEventID])
	assert.Equal(t, "GROUP_AT_MESSAGE_CREATE", logEntry[LogFieldEventType])
	assert.Equal(t, "req-789", logEntry[LogFieldRequestID])
	assert.Equal(t, float64(250), logEntry[LogFieldLatency])
	assert.Equal(t, "process_message", logEntry[LogFieldAction])
	assert.Equal(t, "success", logEntry[LogFieldStatus])
}

// BenchmarkNewLogger benchmarks logger creation
func BenchmarkNewLogger(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewLogger("benchmark")
	}
}

// BenchmarkWithField benchmarks adding a single field
func BenchmarkWithField(b *testing.B) {
	logger := NewLogger("benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = logger.WithField("key", "value")
	}
}

// BenchmarkWithFields benchmarks adding multiple fields
func BenchmarkWithFields(b *testing.B) {
	logger := NewLogger("benchmark")
	fields := logrus.Fields{
		"field1": "value1",
		"field2": 123,
		"field3": true,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = logger.WithFields(fields)
	}
}

// BenchmarkWithContext benchmarks adding context fields
func BenchmarkWithContext(b *testing.B) {
	logger := NewLogger("benchmark")
	event := &dto.Payload{
		ID:   "bench-event",
		Type: "TEST",
	}
	ctx := context.NewContext(event, nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = logger.WithContext(ctx)
	}
}

// BenchmarkLogInfo benchmarks Info logging
func BenchmarkLogInfo(b *testing.B) {
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetFormatter(&logrus.JSONFormatter{})

	logger := NewLogger("benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}

// BenchmarkComplexLogging benchmarks complex logging scenario
func BenchmarkComplexLogging(b *testing.B) {
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetFormatter(&logrus.JSONFormatter{})

	event := &dto.Payload{
		ID:   "bench-event",
		Type: "TEST",
	}
	ctx := context.NewContext(event, nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		logger := NewLogger("benchmark").
			WithContext(ctx).
			WithLatency(100 * time.Millisecond).
			WithAction("test").
			WithStatus("success")
		logger.Info("complex log message")
	}
}
