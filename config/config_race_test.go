package config

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestConfigRaceCondition tests concurrent access to config during hot reload
// This test should be run with -race flag to detect data races
func TestConfigRaceCondition(t *testing.T) {
	// Create a temporary config file
	tmpFile := t.TempDir() + "/config.yaml"
	configContent := `
bot:
  app_id: 12345
  bot_id: 67890
  token: "test_token"
  secret: "test_secret"
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  format: "json"
`
	err := os.WriteFile(tmpFile, []byte(configContent), 0644)
	assert.NoError(t, err)

	// Load initial config
	_, err = Load(tmpFile)
	assert.NoError(t, err)

	// Simulate concurrent reads and writes
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Start multiple readers
	for range 10 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					cfg, ok := Get()
					if ok {
						_ = cfg.Bot.AppID
					}
					time.Sleep(time.Millisecond)
				}
			}
		})
	}

	// Start a writer (simulating hot reload)
	wg.Go(func() {
		for range 5 {
			time.Sleep(10 * time.Millisecond)
			_, _ = Load(tmpFile)
		}
	})

	// Let them run for a bit
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestGetBeforeLoad tests that Get() returns false when config is not loaded
func TestGetBeforeLoad(t *testing.T) {
	// Reset default manager config
	defaultManager.config.Store((*Config)(nil))

	cfg, ok := Get()
	assert.False(t, ok, "Get() should return false when config not loaded")
	assert.Nil(t, cfg, "Get() should return nil config when not loaded")
}

// TestMustGetPanic tests that MustGet panics when config is not loaded
func TestMustGetPanic(t *testing.T) {
	// Reset default manager config
	defaultManager.config.Store((*Config)(nil))

	assert.Panics(t, func() {
		MustGet()
	}, "MustGet should panic when config not loaded")
}

// TestGetReturnsCopy tests that Get() returns a copy to prevent external modification
func TestGetReturnsCopy(t *testing.T) {
	tmpFile := t.TempDir() + "/config.yaml"
	configContent := `
bot:
  app_id: 12345
  bot_id: 67890
  token: "test_token"
  secret: "test_secret"
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  format: "json"
`
	err := os.WriteFile(tmpFile, []byte(configContent), 0644)
	assert.NoError(t, err)

	_, err = Load(tmpFile)
	assert.NoError(t, err)

	cfg1, ok1 := Get()
	assert.True(t, ok1)

	// Modify the returned config
	cfg1.Bot.AppID = 99999

	// Get config again
	cfg2, ok2 := Get()
	assert.True(t, ok2)

	// Original should not be modified
	assert.Equal(t, uint64(12345), cfg2.Bot.AppID, "Get() should return a copy, external modification should not affect it")
}
