package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalLogger tests global logger functions
func TestGlobalLogger(t *testing.T) {
	// Initialize with default config
	err := InitDefault()
	require.NoError(t, err)

	// Test global functions don't panic
	Info("test info")
	Debug("test debug")
	Warn("test warn")
	Error("test error")

	Infof("test %s", "info")
	Debugf("test %s", "debug")
	Warnf("test %s", "warn")
	Errorf("test %s", "error")
}

// TestWithFields tests global WithFields
func TestWithFields(t *testing.T) {
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

// TestWithField tests global WithField
func TestWithField(t *testing.T) {
	err := InitDefault()
	require.NoError(t, err)

	logger := WithField("key", "value")
	require.NotNil(t, logger)
	logger.Info("test with field")
}

// TestWithError tests global WithError
func TestWithError(t *testing.T) {
	err := InitDefault()
	require.NoError(t, err)

	logger := WithError(assert.AnError)
	require.NotNil(t, logger)
	logger.Error("test with error")
}
