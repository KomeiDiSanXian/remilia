package logger

import (
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
