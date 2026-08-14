package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveZerologState 保存并注册清理函数以恢复 zerolog 全局状态。
// 防止并行测试间全局状态污染。
func saveZerologState(t *testing.T) {
	t.Helper()
	savedLogger := log.Logger
	savedLevel := zerolog.GlobalLevel()
	savedTimeFormat := zerolog.TimeFieldFormat
	t.Cleanup(func() {
		log.Logger = savedLogger
		zerolog.SetGlobalLevel(savedLevel)
		zerolog.TimeFieldFormat = savedTimeFormat
	})
}

// TestGlobalLogger tests global logger functions
func TestSetLevel(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	t.Run("valid level", func(t *testing.T) {
		err := SetLevel("debug")
		assert.NoError(t, err)
		Debug("this should appear after SetLevel(debug)")
	})

	t.Run("invalid level returns error", func(t *testing.T) {
		err := SetLevel("invalid")
		assert.Error(t, err)
	})

	t.Run("empty level is treated as no-level (not an error)", func(t *testing.T) {
		err := SetLevel("")
		assert.NoError(t, err)
	})
}

func TestSetTimeFormat(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	t.Run("sets custom time format", func(t *testing.T) {
		SetTimeFormat("15:04:05")
		Info("time should now show only time")
	})

	t.Run("empty string does nothing", func(t *testing.T) {
		SetTimeFormat("")
		Info("time format unchanged")
	})
}

func TestGlobalLogger(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	Info("test info")
	Debug("test debug")
	Warn("test warn")
	Error("test error")

	Infof("test %s", "info")
	Debugf("test %s", "debug")
	Warnf("test %s", "warn")
	Errorf("test %s", "error")
}

func TestWithFields(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	fields := Fields{
		"key1": "value1",
		"key2": 123,
	}

	logger := WithFields(fields)
	require.NotNil(t, logger)
	logger.Info("test with fields")
}

func TestWithField(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	logger := WithField("key", "value")
	require.NotNil(t, logger)
	logger.Info("test with field")
}

func TestWithError(t *testing.T) {
	saveZerologState(t)
	err := InitDefault()
	require.NoError(t, err)

	logger := WithError(assert.AnError)
	require.NotNil(t, logger)
	logger.Error("test with error")
}

// setupBufferLogger 将全局 logger 切换到写入内存缓冲区的实例，并返回该缓冲区。
func setupBufferLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	testLogger := zerolog.New(&buf).With().Timestamp().Logger()
	globalLogger = testLogger
	log.Logger = globalLogger
	defaultLogger.Store(&Logger{l: testLogger})
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	return &buf
}

func TestPackageLevelCaller(t *testing.T) {
	saveZerologState(t)
	buf := setupBufferLogger(t)

	Error("pkg-level-caller")
	WithError(assert.AnError).Error("with-fields-caller")
	Errorf("pkg-level-caller-%s", "f")

	out := buf.String()
	// caller 应指向真实调用方（本测试文件），而不是 logger 包内部或 testing 框架
	if strings.Contains(out, "testing.go") {
		t.Fatalf("caller resolved into testing framework, got output:\n%s", out)
	}
	if !strings.Contains(out, "logger_test.go") {
		t.Fatalf("caller should point to logger_test.go, got output:\n%s", out)
	}
}

func TestLogWithFieldsAllLevels(t *testing.T) {
	saveZerologState(t)
	buf := setupBufferLogger(t)

	lwf := WithField("key", "value")
	lwf.Trace("trace-msg")
	lwf.Tracef("trace-%s", "f")
	lwf.Debug("debug-msg")
	lwf.Debugf("debug-%s", "f")
	lwf.Info("info-msg")
	lwf.Infof("info-%s", "f")
	lwf.Warn("warn-msg")
	lwf.Warnf("warn-%s", "f")
	lwf.Error("error-msg")
	lwf.Errorf("error-%s", "f")

	out := buf.String()
	for _, want := range []string{
		"trace-msg", "trace-f",
		"debug-msg", "debug-f",
		"info-msg", "info-f",
		"warn-msg", "warn-f",
		"error-msg", "error-f",
	} {
		assert.Contains(t, out, want)
	}
}

func TestLogWithFieldsPanic(t *testing.T) {
	saveZerologState(t)
	buf := setupBufferLogger(t)

	assert.Panics(t, func() {
		WithField("key", "value").Panic("panic-msg")
	})
	assert.Contains(t, buf.String(), "panic-msg")
}

func TestInitEmptyLevelDefaultsToInfo(t *testing.T) {
	saveZerologState(t)

	var buf bytes.Buffer
	SetExtraWriter(&buf)
	defer SetExtraWriter(nil)

	require.NoError(t, Init(Config{Console: false, File: false}))

	Info("should-be-visible")
	assert.Contains(t, buf.String(), "should-be-visible")
}
