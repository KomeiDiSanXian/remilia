package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Concurrency_Parse(t *testing.T) {
	content := []byte(`
bot:
  app_id: 1
  bot_id: 2
  token: "t"
  secret: "s"
server:
  host: "127.0.0.1"
  port: 8080
log:
  level: "info"
  format: "text"
concurrency:
  limit: 5
  policy: trywait
  wait_timeout: "150ms"
  event_buffer: 64
`)
	tmp, err := os.CreateTemp("", "cfg-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(content)
	assert.NoError(t, err)
	_ = tmp.Close()

	cfg, err := Load(tmp.Name())
	assert.NoError(t, err)
	assert.Equal(t, 5, cfg.Concurrency.Limit)
	assert.Equal(t, "trywait", cfg.Concurrency.Policy)
	assert.Equal(t, "150ms", cfg.Concurrency.WaitTimeout)
	assert.Equal(t, 64, cfg.Concurrency.EventBuffer)

	if d, err := time.ParseDuration(cfg.Concurrency.WaitTimeout); err == nil {
		assert.Equal(t, 150*time.Millisecond, d)
	}
}
