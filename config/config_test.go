package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	// 创建临时配置文件
	content := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "test_token"
  secret: "test_secret"

server:
  host: "127.0.0.1"
  port: 9090

log:
  level: "debug"
  format: "json"
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(content)
	assert.NoError(t, err)
	tmpFile.Close()

	// 测试加载
	cfg, err := Load(tmpFile.Name())
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// 验证配置值
	assert.Equal(t, uint64(123456), cfg.Bot.AppID)
	assert.Equal(t, uint64(789012), cfg.Bot.BotID)
	assert.Equal(t, "test_token", cfg.Bot.Token)
	assert.Equal(t, "test_secret", cfg.Bot.Secret)
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestLoadInvalidFile(t *testing.T) {
	_, err := Load("non_existent_file.yaml")
	assert.Error(t, err)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{
					Port: 8080,
				},
			},
			wantErr: false,
		},
		{
			name: "missing app_id",
			config: Config{
				Bot: BotConfig{
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
			},
			wantErr: true,
			errMsg:  "app_id",
		},
		{
			name: "missing token",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
			},
			wantErr: true,
			errMsg:  "token",
		},
		{
			name: "invalid port - too low",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{
					Port: 0,
				},
			},
			wantErr: true,
			errMsg:  "port",
		},
		{
			name: "invalid port - too high",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{
					Port: 99999,
				},
			},
			wantErr: true,
			errMsg:  "port",
		},
		{
			name: "invalid log level",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Log: LogConfig{
					Level: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "level",
		},
		{
			name: "invalid log format",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Log: LogConfig{
					Format: "xml",
				},
			},
			wantErr: true,
			errMsg:  "format",
		},
		{
			name: "invalid concurrency policy",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Concurrency: ConcurrencyConfig{
					Policy: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "policy",
		},
		{
			name: "invalid concurrency wait_timeout",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Concurrency: ConcurrencyConfig{
					WaitTimeout: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "wait_timeout",
		},
		{
			name: "valid concurrency config",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Concurrency: ConcurrencyConfig{
					Limit:       100,
					Policy:      "block",
					WaitTimeout: "1s",
					EventBuffer: 1000,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid retry config - max_attempts",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Retry: RetryConfig{
					Enable:      true,
					MaxAttempts: 0,
				},
			},
			wantErr: true,
			errMsg:  "max_attempts",
		},
		{
			name: "valid retry config",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				Retry: RetryConfig{
					Enable:      true,
					MaxAttempts: 3,
					BackoffBase: "100ms",
					BackoffMax:  "5s",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid dead_letter target",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				DeadLetter: DeadLetterConfig{
					Enable: true,
					Target: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "target",
		},
		{
			name: "dead_letter file - missing file_path",
			config: Config{
				Bot: BotConfig{
					AppID:  123,
					BotID:  456,
					Token:  "token",
					Secret: "secret",
				},
				Server: ServerConfig{Port: 8080},
				DeadLetter: DeadLetterConfig{
					Enable: true,
					Target: "file",
				},
			},
			wantErr: true,
			errMsg:  "file_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGet(t *testing.T) {
	// 测试未初始化
	globalConfig = nil
	cfg := Get()
	assert.Nil(t, cfg)

	// 测试已初始化
	testCfg := &Config{
		Bot: BotConfig{
			AppID: 123,
		},
	}
	globalConfig = testCfg
	cfg = Get()
	assert.Equal(t, testCfg, cfg)
}

func TestLoadFromEnv(t *testing.T) {
	// 设置环境变量
	os.Setenv("BOT_APP_ID", "111222")
	os.Setenv("BOT_BOT_ID", "333444")
	os.Setenv("BOT_TOKEN", "env_token")
	os.Setenv("BOT_SECRET", "env_secret")
	os.Setenv("SERVER_HOST", "localhost")
	os.Setenv("SERVER_PORT", "7777")
	os.Setenv("LOG_LEVEL", "warn")
	os.Setenv("LOG_FORMAT", "json")

	defer func() {
		os.Unsetenv("BOT_APP_ID")
		os.Unsetenv("BOT_BOT_ID")
		os.Unsetenv("BOT_TOKEN")
		os.Unsetenv("BOT_SECRET")
		os.Unsetenv("SERVER_HOST")
		os.Unsetenv("SERVER_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_FORMAT")
	}()

	cfg := &Config{
		Bot: BotConfig{
			AppID:  getEnvUint64("BOT_APP_ID"),
			BotID:  getEnvUint64("BOT_BOT_ID"),
			Token:  os.Getenv("BOT_TOKEN"),
			Secret: os.Getenv("BOT_SECRET"),
		},
		Server: ServerConfig{
			Host: getEnvDefault("SERVER_HOST", "0.0.0.0"),
			Port: getEnvInt("SERVER_PORT", 8080),
		},
		Log: LogConfig{
			Level:  getEnvDefault("LOG_LEVEL", "info"),
			Format: getEnvDefault("LOG_FORMAT", "text"),
		},
	}

	assert.Equal(t, uint64(111222), cfg.Bot.AppID)
	assert.Equal(t, uint64(333444), cfg.Bot.BotID)
	assert.Equal(t, "env_token", cfg.Bot.Token)
	assert.Equal(t, "env_secret", cfg.Bot.Secret)
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 7777, cfg.Server.Port)
	assert.Equal(t, "warn", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
}
