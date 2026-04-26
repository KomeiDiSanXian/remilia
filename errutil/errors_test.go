package errutil_test

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/stretchr/testify/assert"
)

// ─── BlockError ─────────────────────────────────────────────────────────

func TestBlockError_NewBlockError(t *testing.T) {
	err := errutil.NewBlockError("blocked")
	assert.Equal(t, "blocked", err.Error())
}

func TestBlockError_IsBlockError(t *testing.T) {
	t.Run("BlockError returns true", func(t *testing.T) {
		assert.True(t, errutil.IsBlockError(errutil.NewBlockError("x")))
	})

	t.Run("other error returns false", func(t *testing.T) {
		assert.False(t, errutil.IsBlockError(io.EOF))
	})

	t.Run("nil returns false", func(t *testing.T) {
		assert.False(t, errutil.IsBlockError(nil))
	})

	t.Run("through wrapping", func(t *testing.T) {
		inner := errutil.NewBlockError("x")
		wrapped := errutil.Wrap(inner, "outer")
		assert.True(t, errutil.IsBlockError(wrapped),
			"IsBlockError should unwrap to find BlockError")
	})
}

// ─── RecoverError ───────────────────────────────────────────────────────

func TestRecoverError_NoPanic(t *testing.T) {
	err := errutil.RecoverError()
	assert.Nil(t, err, "RecoverError without panic should return nil")
}

// ─── ValidationError ────────────────────────────────────────────────────

func TestValidationError_New(t *testing.T) {
	err := errutil.NewValidationError("field", "reason")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "field")
	assert.Contains(t, err.Error(), "reason")
}

func TestValidationError_IsSentinel(t *testing.T) {
	err := errutil.NewValidationError("f", "r")
	assert.True(t, errors.Is(err, errutil.ErrConfigFieldInvalid))
}

func TestValidationError_AsStruct(t *testing.T) {
	err := errutil.NewValidationError("f", "r")
	var ve *errutil.ValidationError
	if assert.True(t, errors.As(err, &ve)) {
		assert.Equal(t, "f", ve.Field)
		assert.Equal(t, "r", ve.Reason)
	}
}

// ─── ConfigError ────────────────────────────────────────────────────────

func TestConfigError_New(t *testing.T) {
	err := errutil.NewConfigError("key", "reason")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "key")
	assert.Contains(t, err.Error(), "reason")
}

func TestConfigError_IsSentinel(t *testing.T) {
	err := errutil.NewConfigError("k", "r")
	assert.True(t, errors.Is(err, errutil.ErrConfigInvalid))
}

func TestConfigError_AsStruct(t *testing.T) {
	err := errutil.NewConfigError("k", "r")
	var ce *errutil.ConfigError
	if assert.True(t, errors.As(err, &ce)) {
		assert.Equal(t, "k", ce.Key)
		assert.Equal(t, "r", ce.Reason)
	}
}

// ─── PluginError ────────────────────────────────────────────────────────

func TestPluginError_New(t *testing.T) {
	err := errutil.NewPluginError("plugin", "msg")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "plugin")
	assert.Contains(t, err.Error(), "msg")
}

func TestPluginError_IsSentinel(t *testing.T) {
	err := errutil.NewPluginError("p", "m")
	assert.True(t, errors.Is(err, errutil.ErrPluginLoadFailed))
}

func TestPluginError_AsStruct(t *testing.T) {
	err := errutil.NewPluginError("p", "m")
	var pe *errutil.PluginError
	if assert.True(t, errors.As(err, &pe)) {
		assert.Equal(t, "p", pe.PluginName)
		assert.Equal(t, "m", pe.Message)
	}
}

// ─── Stack ──────────────────────────────────────────────────────────────

func TestStack_EnableDisable(t *testing.T) {
	_ = os.Unsetenv("REMILIA_STACK_TRACE")
	errutil.EnableStackTrace(false)

	t.Run("default state is disabled", func(t *testing.T) {
		errutil.EnableStackTrace(false)
		assert.False(t, errutil.IsStackTraceEnabled())
	})

	t.Run("enable then ShouldCaptureStack returns true", func(t *testing.T) {
		errutil.EnableStackTrace(true)
		assert.True(t, errutil.ShouldCaptureStack())
		errutil.EnableStackTrace(false)
	})

	t.Run("disable then ShouldCaptureStack returns false", func(t *testing.T) {
		errutil.EnableStackTrace(false)
		assert.False(t, errutil.ShouldCaptureStack())
	})

	t.Run("IsStackTraceEnabled returns current state without side effects", func(t *testing.T) {
		errutil.EnableStackTrace(false)
		assert.False(t, errutil.IsStackTraceEnabled())
		errutil.EnableStackTrace(true)
		assert.True(t, errutil.IsStackTraceEnabled())
		errutil.EnableStackTrace(false)
	})

	errutil.EnableStackTrace(false)
}

func TestStack_CaptureStack(t *testing.T) {
	errutil.EnableStackTrace(true)
	defer errutil.EnableStackTrace(false)

	t.Run("returns non-empty string", func(t *testing.T) {
		s := errutil.CaptureStack()
		assert.NotEmpty(t, s)
	})

	t.Run("contains file:line pattern", func(t *testing.T) {
		s := errutil.CaptureStack()
		assert.Contains(t, s, ".go:")
	})

	t.Run("does not contain runtime frames", func(t *testing.T) {
		s := errutil.CaptureStack()
		assert.False(t, strings.Contains(s, "/runtime/"),
			"stack trace should not contain runtime frames")
	})

	t.Run("no stack trace available not returned from test context", func(t *testing.T) {
		s := errutil.CaptureStack()
		assert.NotEqual(t, "no stack trace available", s,
			"expected at least one non-runtime frame")
	})
}

// ─── Wrappers (complements existing wrapper_test.go) ─────────────────────

func TestWrapWithContext_EmptyContext(t *testing.T) {
	inner := errutil.New("inner")
	err := errutil.WrapWithContext(inner, "msg", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "inner")
	assert.Contains(t, err.Error(), "msg")
	assert.False(t, strings.Contains(err.Error(), "[context: ]"),
		"empty context should not produce context bracket")
}

func TestWrapWithContext_ErrorsIs(t *testing.T) {
	inner := errutil.New("inner")
	err := errutil.WrapWithContext(inner, "msg", "ctx")
	assert.True(t, errors.Is(err, inner))
}
