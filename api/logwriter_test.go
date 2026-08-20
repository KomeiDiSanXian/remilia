package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLogLine(t *testing.T) {
	t.Run("parses level and message", func(t *testing.T) {
		level, msg := parseLogLine([]byte(`{"level":"info","time":"12:00:00.000","message":"hello"}`))
		assert.Equal(t, "info", level)
		assert.Equal(t, "hello", msg)
	})

	t.Run("field order independent", func(t *testing.T) {
		level, msg := parseLogLine([]byte(`{"message":"hi","level":"warn"}`))
		assert.Equal(t, "warn", level)
		assert.Equal(t, "hi", msg)
	})

	t.Run("handles escaped newline", func(t *testing.T) {
		level, msg := parseLogLine([]byte(`{"level":"error","message":"line1\nline2"}`))
		assert.Equal(t, "error", level)
		assert.Equal(t, "line1\nline2", msg)
	})

	t.Run("defaults missing level to info", func(t *testing.T) {
		level, msg := parseLogLine([]byte(`{"message":"no level"}`))
		assert.Equal(t, "info", level)
		assert.Equal(t, "no level", msg)
	})

	t.Run("non JSON falls back to raw line", func(t *testing.T) {
		level, msg := parseLogLine([]byte(`plain text line`))
		assert.Equal(t, "info", level)
		assert.Equal(t, "", msg)
	})
}
