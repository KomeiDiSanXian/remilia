package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad 测试从文件加载配置
func TestLoad(t *testing.T) {
	t.Run("valid yaml config", func(t *testing.T) {
		// 创建临时配置文件
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "test-token"
  secret: "test-secret"
server:
  host: "localhost"
  port: 8080
log:
  level: "info"
  format: "json"
concurrency:
  limit: 100
  policy: "drop"
retry:
  enable: true
  max_attempts: 3
middleware:
  rate_limit: true
  rate_limit_rate: 100
  rate_limit_burst: 200
dead_letter:
  enable: false
webhook:
  event_buffer: 1000
`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		// 加载配置
		cfg, err := Load(configPath)
		require.NoError(t, err)
		assert.NotNil(t, cfg)

		// 验证配置内容
		assert.Equal(t, uint64(123456), cfg.Bot.AppID)
		assert.Equal(t, uint64(789012), cfg.Bot.BotID)
		assert.Equal(t, "test-token", cfg.Bot.Token)
		assert.Equal(t, "test-secret", cfg.Bot.Secret)
		assert.Equal(t, "localhost", cfg.Server.Host)
		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, "info", cfg.Log.Level)
		assert.Equal(t, "json", cfg.Log.Format)
		assert.Equal(t, 100, cfg.Concurrency.Limit)
		assert.Equal(t, "drop", cfg.Concurrency.Policy)
		assert.True(t, cfg.Retry.Enable)
		assert.Equal(t, 3, cfg.Retry.MaxAttempts)

		// 验证全局配置已设置
		assert.Equal(t, cfg, Get())
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Load("nonexistent.yaml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "invalid.yaml")

		invalidContent := `
bot:
  app_id: 123456
  invalid yaml content [[[
`
		err := os.WriteFile(configPath, []byte(invalidContent), 0644)
		require.NoError(t, err)

		_, err = Load(configPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse config file")
	})

	t.Run("invalid config - missing required fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
bot:
  app_id: 0
  bot_id: 0
server:
  port: 8080
`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		_, err = Load(configPath)
		assert.Error(t, err)
	})
}

// TestLoadDefault 测试默认加载
func TestLoadDefault(t *testing.T) {
	// 保存原始环境变量
	originalEnv := map[string]string{
		"BOT_APP_ID":  os.Getenv("BOT_APP_ID"),
		"BOT_BOT_ID":  os.Getenv("BOT_BOT_ID"),
		"BOT_TOKEN":   os.Getenv("BOT_TOKEN"),
		"BOT_SECRET":  os.Getenv("BOT_SECRET"),
		"SERVER_HOST": os.Getenv("SERVER_HOST"),
		"SERVER_PORT": os.Getenv("SERVER_PORT"),
		"LOG_LEVEL":   os.Getenv("LOG_LEVEL"),
		"LOG_FORMAT":  os.Getenv("LOG_FORMAT"),
	}
	defer func() {
		// 恢复环境变量
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	t.Run("load from config.yaml", func(t *testing.T) {
		// 创建临时目录并切换到该目录
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		configContent := `
bot:
  app_id: 111111
  bot_id: 222222
  token: "default-token"
  secret: "default-secret"
server:
  port: 9090
`
		err := os.WriteFile("config.yaml", []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadDefault()
		require.NoError(t, err)
		assert.Equal(t, uint64(111111), cfg.Bot.AppID)
		assert.Equal(t, uint64(222222), cfg.Bot.BotID)
	})

	t.Run("load from environment variables", func(t *testing.T) {
		// 切换到空目录（没有配置文件）
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		// 设置环境变量
		os.Setenv("BOT_APP_ID", "333333")
		os.Setenv("BOT_BOT_ID", "444444")
		os.Setenv("BOT_TOKEN", "env-token")
		os.Setenv("BOT_SECRET", "env-secret")
		os.Setenv("SERVER_PORT", "7070")

		cfg, err := LoadDefault()
		require.NoError(t, err)
		assert.Equal(t, uint64(333333), cfg.Bot.AppID)
		assert.Equal(t, uint64(444444), cfg.Bot.BotID)
		assert.Equal(t, "env-token", cfg.Bot.Token)
		assert.Equal(t, "env-secret", cfg.Bot.Secret)
		assert.Equal(t, 7070, cfg.Server.Port)
	})

	t.Run("no config file and incomplete env", func(t *testing.T) {
		// 切换到空目录
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		// 清除环境变量
		os.Unsetenv("BOT_APP_ID")
		os.Unsetenv("BOT_BOT_ID")
		os.Unsetenv("BOT_TOKEN")
		os.Unsetenv("BOT_SECRET")

		_, err := LoadDefault()
		assert.Error(t, err)
	})
}

// TestWatch 测试配置文件监听
func TestWatch(t *testing.T) {
	t.Run("watch config file changes", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "watch.yaml")

		// 初始配置
		initialContent := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "initial-token"
  secret: "initial-secret"
server:
  port: 8080
log:
  level: "info"
concurrency:
  limit: 100
retry:
  enable: false
middleware:
  rate_limit: false
dead_letter:
  enable: false
webhook:
  event_buffer: 1000
`
		err := os.WriteFile(configPath, []byte(initialContent), 0644)
		require.NoError(t, err)

		// 加载初始配置
		_, err = Load(configPath)
		require.NoError(t, err)

		// 设置监听
		updateCount := 0
		var lastConfig *Config
		stopFunc, err := Watch(configPath, func(cfg *Config) {
			updateCount++
			lastConfig = cfg
		})
		require.NoError(t, err)
		require.NotNil(t, stopFunc)
		defer stopFunc()

		// 等待监听启动
		time.Sleep(100 * time.Millisecond)

		// 修改配置文件
		updatedContent := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "updated-token"
  secret: "updated-secret"
server:
  port: 9090
log:
  level: "debug"
concurrency:
  limit: 200
retry:
  enable: false
middleware:
  rate_limit: false
dead_letter:
  enable: false
webhook:
  event_buffer: 1000
`
		err = os.WriteFile(configPath, []byte(updatedContent), 0644)
		require.NoError(t, err)

		// 等待文件监听和重载（考虑防抖延迟）
		time.Sleep(500 * time.Millisecond)

		// 验证配置已更新
		assert.Greater(t, updateCount, 0, "config should be reloaded")
		if lastConfig != nil {
			assert.Equal(t, "updated-token", lastConfig.Bot.Token)
			assert.Equal(t, 9090, lastConfig.Server.Port)
			assert.Equal(t, "debug", lastConfig.Log.Level)
			assert.Equal(t, 200, lastConfig.Concurrency.Limit)
		}
	})

	t.Run("watch empty path", func(t *testing.T) {
		_, err := Watch("", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "watch path is empty")
	})

	t.Run("watch nonexistent file", func(t *testing.T) {
		_, err := Watch("/nonexistent/path/config.yaml", nil)
		assert.Error(t, err)
	})
}

// TestLoadViper 测试使用 Viper 加载配置
func TestLoadViper(t *testing.T) {
	t.Run("load with explicit path", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "viper.yaml")

		configContent := `
bot:
  app_id: 555555
  bot_id: 666666
  token: "viper-token"
  secret: "viper-secret"
server:
  port: 8888
log:
  level: "warn"
concurrency:
  limit: 50
retry:
  enable: false
middleware:
  rate_limit: false
dead_letter:
  enable: false
webhook:
  event_buffer: 500
`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadViper(configPath)
		require.NoError(t, err)
		assert.Equal(t, uint64(555555), cfg.Bot.AppID)
		assert.Equal(t, uint64(666666), cfg.Bot.BotID)
		assert.Equal(t, "viper-token", cfg.Bot.Token)
		assert.Equal(t, 8888, cfg.Server.Port)
		assert.Equal(t, "warn", cfg.Log.Level)
	})

	t.Run("load from default path", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		configContent := `
bot:
  app_id: 777777
  bot_id: 888888
  token: "default-viper-token"
  secret: "default-viper-secret"
server:
  port: 7777
log:
  level: "error"
concurrency:
  limit: 25
retry:
  enable: false
middleware:
  rate_limit: false
dead_letter:
  enable: false
webhook:
  event_buffer: 250
`
		err := os.WriteFile("config.yaml", []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadViper("")
		require.NoError(t, err)
		assert.Equal(t, uint64(777777), cfg.Bot.AppID)
		assert.Equal(t, 7777, cfg.Server.Port)
	})

	t.Run("config file not found - use fallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "invalid.yaml")

		// 配置缺少必填字段
		configContent := `
bot:
  app_id: 0
  bot_id: 0
server:
  port: 8888
`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		_, err = LoadViper(configPath)
		assert.Error(t, err)
	})
}

// TestGetEnvHelpers 测试环境变量辅助函数
func TestGetEnvHelpers(t *testing.T) {
	// 保存原始环境变量
	originalEnv := map[string]string{
		"TEST_UINT64": os.Getenv("TEST_UINT64"),
		"TEST_INT":    os.Getenv("TEST_INT"),
		"TEST_STRING": os.Getenv("TEST_STRING"),
	}
	defer func() {
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	t.Run("getEnvUint64", func(t *testing.T) {
		os.Setenv("TEST_UINT64", "123456789")
		result := getEnvUint64("TEST_UINT64")
		assert.Equal(t, uint64(123456789), result)

		os.Unsetenv("TEST_UINT64")
		result = getEnvUint64("TEST_UINT64")
		assert.Equal(t, uint64(0), result)

		os.Setenv("TEST_UINT64", "invalid")
		result = getEnvUint64("TEST_UINT64")
		assert.Equal(t, uint64(0), result)
	})

	t.Run("getEnvInt", func(t *testing.T) {
		os.Setenv("TEST_INT", "42")
		result := getEnvInt("TEST_INT", 100)
		assert.Equal(t, 42, result)

		os.Unsetenv("TEST_INT")
		result = getEnvInt("TEST_INT", 100)
		assert.Equal(t, 100, result)

		os.Setenv("TEST_INT", "invalid")
		result = getEnvInt("TEST_INT", 100)
		assert.Equal(t, 0, result) // Sscanf 失败返回 0
	})

	t.Run("getEnvDefault", func(t *testing.T) {
		os.Setenv("TEST_STRING", "value")
		result := getEnvDefault("TEST_STRING", "default")
		assert.Equal(t, "value", result)

		os.Unsetenv("TEST_STRING")
		result = getEnvDefault("TEST_STRING", "default")
		assert.Equal(t, "default", result)
	})
}

// TestGet 测试全局配置获取
func TestGet(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "get-test-token"
  secret: "get-test-secret"
server:
  port: 8080
log:
  level: "info"
concurrency:
  limit: 100
retry:
  enable: false
middleware:
  rate_limit: false
dead_letter:
  enable: false
webhook:
  event_buffer: 1000
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// 加载配置
	cfg, err := Load(configPath)
	require.NoError(t, err)

	// 验证 Get 返回相同的配置
	globalCfg := Get()
	assert.Equal(t, cfg, globalCfg)
	assert.Equal(t, "get-test-token", globalCfg.Bot.Token)
}
