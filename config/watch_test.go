package config

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWatch_ReloadsOnWrite(t *testing.T) {
	content1 := `
bot:
  app_id: 123
  bot_id: 456
  token: "t1"
  secret: "s1"
server:
  host: "127.0.0.1"
  port: 8080
log:
  level: "info"
  format: "text"
`
	content2 := `
bot:
  app_id: 999
  bot_id: 888
  token: "t2"
  secret: "s2"
server:
  host: "0.0.0.0"
  port: 9090
log:
  level: "debug"
  format: "json"
`

	f, err := os.CreateTemp("", "cfg-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content1)
	assert.NoError(t, err)
	_ = f.Close()

	var applied *Config
	var mu sync.RWMutex
	stop, err := Watch(f.Name(), func(c *Config) {
		mu.Lock()
		applied = c
		mu.Unlock()
	})
	assert.NoError(t, err)
	defer stop()

	// 修改文件触发重载
	err = os.WriteFile(f.Name(), []byte(content2), 0644)
	assert.NoError(t, err)

	// 等待重载生效
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.RLock()
		if applied != nil && applied.Bot.AppID == 999 {
			mu.RUnlock()
			break
		}
		mu.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}

	mu.RLock()
	defer mu.RUnlock()
	assert.NotNil(t, applied)
	assert.Equal(t, uint64(999), applied.Bot.AppID)
	assert.Equal(t, "debug", applied.Log.Level)
}
