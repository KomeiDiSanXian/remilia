package remilia

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEngineOptions(t *testing.T) {
	t.Run("Default configuration", func(t *testing.T) {
		e := NewEngine()
		defer e.Close()

		assert.Equal(t, 5*time.Minute, e.s.tempMatcherCleanerInterval, "Default cleanup interval should be 5 minutes")
		assert.Equal(t, 1000, cap(e.s.pendingDeleteCh), "Default pending delete channel capacity should be 1000")
	})

	t.Run("WithCleanupInterval", func(t *testing.T) {
		interval := 10 * time.Minute
		e := NewEngine(WithCleanupInterval(interval))
		defer e.Close()

		assert.Equal(t, interval, e.s.tempMatcherCleanerInterval, "Cleanup interval should be configured value")
		// Verify via getter as well
		assert.Equal(t, interval, e.GetTempMatcherCleanInterval())
	})

	t.Run("WithPendingDeleteBufferSize", func(t *testing.T) {
		size := 2000
		e := NewEngine(WithPendingDeleteBufferSize(size))
		defer e.Close()

		assert.Equal(t, size, cap(e.s.pendingDeleteCh), "Pending delete channel capacity should be configured value")
	})

	t.Run("Multiple options", func(t *testing.T) {
		interval := 1 * time.Minute
		size := 500
		e := NewEngine(
			WithCleanupInterval(interval),
			WithPendingDeleteBufferSize(size),
		)
		defer e.Close()

		assert.Equal(t, interval, e.s.tempMatcherCleanerInterval)
		assert.Equal(t, size, cap(e.s.pendingDeleteCh))
	})
}

func TestWithCleanupIntervalOption(t *testing.T) {
	interval := 10 * time.Minute
	e := NewEngine(WithCleanupInterval(interval))
	assert.Equal(t, interval, e.s.tempMatcherCleanerInterval, "Cleanup interval should be configured value")
}

func TestWithPendingDeleteBufferSizeOption(t *testing.T) {
	size := 500
	e := NewEngine(WithPendingDeleteBufferSize(size))
	assert.Equal(t, size, cap(e.s.pendingDeleteCh), "Pending delete channel capacity should be configured value")
}

func TestEngineDefaultOptions(t *testing.T) {
	e := NewEngine()
	assert.Equal(t, 5*time.Minute, e.s.tempMatcherCleanerInterval, "Default cleanup interval should be 5 minutes")
	assert.Equal(t, 1000, cap(e.s.pendingDeleteCh), "Default pending delete channel capacity should be 1000")
}
