package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcher_NewWatcher(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpFile := createTempConfigFile(t, validConfig)
		defer os.Remove(tmpFile)

		watcher, err := NewWatcher(tmpFile)
		require.NoError(t, err)
		require.NotNil(t, watcher)
		defer watcher.Stop()

		cfg := watcher.GetConfig()
		assert.NotNil(t, cfg)
		assert.Equal(t, uint64(123456), cfg.Bot.AppID)
	})

	t.Run("invalid_path", func(t *testing.T) {
		_, err := NewWatcher("/nonexistent/config.yaml")
		assert.Error(t, err)
	})

	t.Run("invalid_config", func(t *testing.T) {
		tmpFile := createTempConfigFile(t, invalidConfig)
		defer os.Remove(tmpFile)

		_, err := NewWatcher(tmpFile)
		assert.Error(t, err)
	})
}

func TestWatcher_FileChangeDetection(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile, WithDebounceDelay(50*time.Millisecond))
	require.NoError(t, err)
	defer watcher.Stop()

	// Track reload calls
	reloadCalled := make(chan bool, 1)
	watcher.AddCallback(func(old, new *Config) error {
		select {
		case reloadCalled <- true:
		default:
		}
		return nil
	})

	watcher.Start()

	// Modify the config file
	newConfig := `
bot:
  app_id: 999999
  bot_id: 888888
  token: "new-token"
  secret: "new-secret"

server:
  host: "0.0.0.0"
  port: 9090

log:
  level: "debug"
  format: "json"
`
	err = os.WriteFile(tmpFile, []byte(newConfig), 0644)
	require.NoError(t, err)

	// Wait for reload
	select {
	case <-reloadCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Reload callback not called within timeout")
	}

	// Verify new config is loaded
	cfg := watcher.GetConfig()
	assert.Equal(t, uint64(999999), cfg.Bot.AppID)
	assert.Equal(t, uint64(888888), cfg.Bot.BotID)
	assert.Equal(t, "new-token", cfg.Bot.Token)
	assert.Equal(t, 9090, cfg.Server.Port)
}

func TestWatcher_CallbackExecution(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile)
	require.NoError(t, err)
	defer watcher.Stop()

	t.Run("multiple_callbacks", func(t *testing.T) {
		var callback1Called, callback2Called bool

		watcher.AddCallback(func(old, new *Config) error {
			callback1Called = true
			return nil
		})

		watcher.AddCallback(func(old, new *Config) error {
			callback2Called = true
			return nil
		})

		err := watcher.ForceReload()
		assert.NoError(t, err)
		assert.True(t, callback1Called)
		assert.True(t, callback2Called)
	})

	t.Run("callback_rejection", func(t *testing.T) {
		watcher.AddCallback(func(old, new *Config) error {
			return assert.AnError
		})

		oldCfg := watcher.GetConfig()
		err := watcher.ForceReload()
		assert.Error(t, err)

		// Config should not change
		assert.Equal(t, oldCfg, watcher.GetConfig())
	})
}

func TestWatcher_ValidateOnly(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile, WithValidateOnly(true))
	require.NoError(t, err)
	defer watcher.Stop()

	oldCfg := watcher.GetConfig()

	// Modify config
	newConfig := `
bot:
  app_id: 999999
  bot_id: 888888
  token: "new-token"
  secret: "new-secret"

server:
  host: "0.0.0.0"
  port: 9090

log:
  level: "info"
  format: "text"
`
	err = os.WriteFile(tmpFile, []byte(newConfig), 0644)
	require.NoError(t, err)

	// Force reload
	err = watcher.ForceReload()
	assert.NoError(t, err)

	// Config should NOT change in validate-only mode
	assert.Equal(t, oldCfg, watcher.GetConfig())
}

func TestWatcher_Stats(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile)
	require.NoError(t, err)
	defer watcher.Stop()

	// Initial stats
	stats := watcher.GetStats()
	assert.Equal(t, int64(0), stats.ReloadCount)
	assert.Equal(t, int64(0), stats.FailedCount)
	assert.False(t, stats.LastReloadTime.IsZero())

	// Successful reload
	err = watcher.ForceReload()
	assert.NoError(t, err)

	stats = watcher.GetStats()
	assert.Equal(t, int64(1), stats.ReloadCount)
	assert.Equal(t, int64(0), stats.FailedCount)

	// Failed reload
	watcher.AddCallback(func(old, new *Config) error {
		return assert.AnError
	})

	err = watcher.ForceReload()
	assert.Error(t, err)

	stats = watcher.GetStats()
	assert.Equal(t, int64(1), stats.ReloadCount)
	assert.Equal(t, int64(1), stats.FailedCount)
}

func TestWatcher_Debounce(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile, WithDebounceDelay(200*time.Millisecond))
	require.NoError(t, err)
	defer watcher.Stop()

	var reloadCount atomic.Int32
	watcher.AddCallback(func(old, new *Config) error {
		reloadCount.Add(1)
		return nil
	})

	watcher.Start()

	// Write multiple times quickly
	for range 5 {
		err = os.WriteFile(tmpFile, []byte(validConfig), 0644)
		require.NoError(t, err)
		time.Sleep(30 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(400 * time.Millisecond)

	// Should only reload once due to debouncing
	assert.LessOrEqual(t, reloadCount.Load(), int32(2), "Expected at most 2 reloads due to debouncing")
}

func TestWatcher_Stop(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile)
	require.NoError(t, err)

	watcher.Start()

	// Stop should be clean
	err = watcher.Stop()
	assert.NoError(t, err)

	// Multiple stops should be safe (may return error from fsnotify, which is ok)
	_ = watcher.Stop()
}

func TestWatcher_ConcurrentAccess(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	watcher, err := NewWatcher(tmpFile)
	require.NoError(t, err)
	defer watcher.Stop()

	done := make(chan bool)

	// Concurrent reads
	for range 10 {
		go func() {
			for range 100 {
				_ = watcher.GetConfig()
			}
			done <- true
		}()
	}

	// Concurrent writes
	for range 5 {
		go func() {
			for range 10 {
				_ = watcher.ForceReload()
				time.Sleep(10 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for range 15 {
		<-done
	}
}

func TestWatchWithAutoRestart(t *testing.T) {
	tmpFile := createTempConfigFile(t, validConfig)
	defer os.Remove(tmpFile)

	restartCalled := false
	restartFunc := func(cfg *Config) error {
		restartCalled = true
		return nil
	}

	watcher, err := WatchWithAutoRestart(tmpFile, restartFunc)
	require.NoError(t, err)
	defer watcher.Stop()

	// Change Bot config (requires restart)
	newConfig := `
bot:
  app_id: 999999
  bot_id: 888888
  token: "new-token"
  secret: "new-secret"

server:
  host: "0.0.0.0"
  port: 8080

log:
  level: "info"
  format: "text"
`
	err = os.WriteFile(tmpFile, []byte(newConfig), 0644)
	require.NoError(t, err)

	err = watcher.ForceReload()
	assert.NoError(t, err)
	assert.True(t, restartCalled)
}

// Helper functions

func createTempConfigFile(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)

	return tmpFile
}

const validConfig = `
bot:
  app_id: 123456
  bot_id: 654321
  token: "test-token"
  secret: "test-secret"

server:
  host: "0.0.0.0"
  port: 8080

log:
  level: "info"
  format: "text"

concurrency:
  limit: 100
  policy: "drop"
  wait_timeout: "5s"
  event_buffer: 1000

retry:
  enable: true
  max_attempts: 3
  backoff_base: "1s"
  backoff_max: "30s"

middleware:
  logging: true
  recover: true
  auth:
    enable: false
  rate_limit:
    enable: false
  metrics: true

dead_letter:
  enable: false

webhook:
  event_buffer: 1000
  dedup_enable: true
  dedup_shards: 16
  dedup_life_window: "5m"
  dedup_clean_window: "1m"
  dedup_max_entry_size: 10000
  dedup_hard_max_size: 100000
`

const invalidConfig = `
bot:
  app_id: 0  # Invalid: must be non-zero
  token: ""  # Invalid: must be non-empty

server:
  port: 99999  # Invalid: out of range
`
