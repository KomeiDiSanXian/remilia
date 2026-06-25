package config

import (
	"os"
	"path/filepath"
	"testing"

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
  qq:
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
  rate_limit:
    enable: true
    rate: 100
    burst: 200
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
		assert.Equal(t, uint64(123456), cfg.Bot.QQ.AppID)
		assert.Equal(t, uint64(789012), cfg.Bot.QQ.BotID)
		assert.Equal(t, "test-token", cfg.Bot.QQ.Token)
		assert.Equal(t, "test-secret", cfg.Bot.QQ.Secret)
		assert.Equal(t, "localhost", cfg.Server.Host)
		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, "info", cfg.Log.Level)
		assert.Equal(t, "json", cfg.Log.Format)
		assert.Equal(t, 100, cfg.Concurrency.Limit)
		assert.Equal(t, "drop", cfg.Concurrency.Policy)
		assert.True(t, cfg.Retry.Enable)
		assert.Equal(t, 3, cfg.Retry.MaxAttempts)

		// 验证全局配置已设置
		globalCfg, ok := Get()
		assert.True(t, ok)
		assert.Equal(t, cfg.Bot.QQ.Token, globalCfg.Bot.QQ.Token)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Load("nonexistent.yaml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config file not found")
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

		// QQ 配置部分填写：app_id 已设置但 bot_id 缺失 → 触发验证失败
		configContent := `
bot:
  qq:
    app_id: 123456
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
		"QQ_APP_ID":   os.Getenv("QQ_APP_ID"),
		"QQ_BOT_ID":   os.Getenv("QQ_BOT_ID"),
		"QQ_TOKEN":    os.Getenv("QQ_TOKEN"),
		"QQ_SECRET":   os.Getenv("QQ_SECRET"),
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
  qq:
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
		assert.Equal(t, uint64(111111), cfg.Bot.QQ.AppID)
		assert.Equal(t, uint64(222222), cfg.Bot.QQ.BotID)
	})

	t.Run("load from environment variables", func(t *testing.T) {
		// 切换到空目录（没有配置文件）
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		// 设置环境变量
		os.Setenv("QQ_APP_ID", "333333")
		os.Setenv("QQ_BOT_ID", "444444")
		os.Setenv("QQ_TOKEN", "env-token")
		os.Setenv("QQ_SECRET", "env-secret")
		os.Setenv("SERVER_PORT", "7070")

		cfg, err := LoadDefault()
		require.NoError(t, err)
		assert.Equal(t, uint64(333333), cfg.Bot.QQ.AppID)
		assert.Equal(t, uint64(444444), cfg.Bot.QQ.BotID)
		assert.Equal(t, "env-token", cfg.Bot.QQ.Token)
		assert.Equal(t, "env-secret", cfg.Bot.QQ.Secret)
		assert.Equal(t, 7070, cfg.Server.Port)
	})

	t.Run("no config file and incomplete env", func(t *testing.T) {
		// 切换到空目录
		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		os.Chdir(tmpDir)

		// 清除环境变量
		os.Unsetenv("QQ_APP_ID")
		os.Unsetenv("QQ_BOT_ID")
		os.Unsetenv("QQ_TOKEN")
		os.Unsetenv("QQ_SECRET")
		// 设置部分 qq 字段以触发验证失败（app_id 已设置但 bot_id 缺失）
		os.Setenv("QQ_APP_ID", "123456")

		_, err := LoadDefault()
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
		assert.Equal(t, 100, result) // 解析失败返回默认值
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
  qq:
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
  rate_limit:
    enable: false
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
	globalCfg, ok := Get()
	assert.True(t, ok)
	assert.Equal(t, cfg.Bot.QQ.Token, globalCfg.Bot.QQ.Token)
	assert.Equal(t, "get-test-token", globalCfg.Bot.QQ.Token)
}
