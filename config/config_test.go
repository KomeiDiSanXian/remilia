package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBotConfig_Validate 测试 Bot 配置验证
func TestBotConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BotConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BotConfig{
				AppID:  123456,
				BotID:  789012,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "missing app_id",
			config: BotConfig{
				BotID:  789012,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "app_id",
		},
		{
			name: "missing bot_id",
			config: BotConfig{
				AppID:  123456,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "bot_id",
		},
		{
			name: "missing token",
			config: BotConfig{
				AppID:  123456,
				BotID:  789012,
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "token",
		},
		{
			name: "missing secret",
			config: BotConfig{
				AppID: 123456,
				BotID: 789012,
				Token: "test-token",
			},
			wantErr: true,
			errMsg:  "secret",
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

// TestServerConfig_Validate 测试 Server 配置验证
func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  ServerConfig{Host: "localhost", Port: 8080},
			wantErr: false,
		},
		{
			name:    "valid port range - min",
			config:  ServerConfig{Host: "0.0.0.0", Port: 1},
			wantErr: false,
		},
		{
			name:    "valid port range - max",
			config:  ServerConfig{Host: "0.0.0.0", Port: 65535},
			wantErr: false,
		},
		{
			name:    "empty host is valid",
			config:  ServerConfig{Port: 8080},
			wantErr: false,
		},
		{
			name:    "port too low",
			config:  ServerConfig{Host: "localhost", Port: 0},
			wantErr: true,
			errMsg:  "port must be between",
		},
		{
			name:    "port too high",
			config:  ServerConfig{Host: "localhost", Port: 65536},
			wantErr: true,
			errMsg:  "port must be between",
		},
		{
			name:    "negative port",
			config:  ServerConfig{Host: "localhost", Port: -1},
			wantErr: true,
			errMsg:  "port must be between",
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

// TestLogConfig_Validate 测试 Log 配置验证
func TestLogConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  LogConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid level - debug",
			config:  LogConfig{Level: "debug", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - info",
			config:  LogConfig{Level: "info", Format: "json"},
			wantErr: false,
		},
		{
			name:    "valid level - warn",
			config:  LogConfig{Level: "warn", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - error",
			config:  LogConfig{Level: "error", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - fatal",
			config:  LogConfig{Level: "fatal", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - panic",
			config:  LogConfig{Level: "panic", Format: "text"},
			wantErr: false,
		},
		{
			name:    "empty level is valid",
			config:  LogConfig{Format: "text"},
			wantErr: false,
		},
		{
			name:    "empty format is valid",
			config:  LogConfig{Level: "info"},
			wantErr: false,
		},
		{
			name:    "invalid level",
			config:  LogConfig{Level: "trace", Format: "text"},
			wantErr: true,
			errMsg:  "log.level",
		},
		{
			name:    "invalid format",
			config:  LogConfig{Level: "info", Format: "xml"},
			wantErr: true,
			errMsg:  "log.format",
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

// TestConcurrencyConfig_Validate 测试 Concurrency 配置验证
func TestConcurrencyConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ConcurrencyConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ConcurrencyConfig{
				Limit:       100,
				Policy:      "drop",
				WaitTimeout: "5s",
				EventBuffer: 1000,
			},
			wantErr: false,
		},
		{
			name: "valid policy - block",
			config: ConcurrencyConfig{
				Limit:  50,
				Policy: "block",
			},
			wantErr: false,
		},
		{
			name: "valid policy - trywait",
			config: ConcurrencyConfig{
				Limit:       50,
				Policy:      "trywait",
				WaitTimeout: "10s",
			},
			wantErr: false,
		},
		{
			name: "empty policy is valid",
			config: ConcurrencyConfig{
				Limit: 50,
			},
			wantErr: false,
		},
		{
			name: "zero limit is valid",
			config: ConcurrencyConfig{
				Limit:  0,
				Policy: "drop",
			},
			wantErr: false,
		},
		{
			name: "negative limit",
			config: ConcurrencyConfig{
				Limit:  -1,
				Policy: "drop",
			},
			wantErr: true,
			errMsg:  "concurrency.limit",
		},
		{
			name: "invalid policy",
			config: ConcurrencyConfig{
				Limit:  50,
				Policy: "reject",
			},
			wantErr: true,
			errMsg:  "concurrency.policy",
		},
		{
			name: "invalid wait_timeout",
			config: ConcurrencyConfig{
				Limit:       50,
				Policy:      "trywait",
				WaitTimeout: "invalid",
			},
			wantErr: true,
			errMsg:  "wait_timeout",
		},
		{
			name: "negative event_buffer",
			config: ConcurrencyConfig{
				Limit:       50,
				EventBuffer: -1,
			},
			wantErr: true,
			errMsg:  "event_buffer",
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

// TestRetryConfig_Validate 测试 Retry 配置验证
func TestRetryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RetryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: RetryConfig{
				Enable:      true,
				MaxAttempts: 3,
				BackoffBase: "1s",
				BackoffMax:  "30s",
			},
			wantErr: false,
		},
		{
			name: "disabled retry - no validation",
			config: RetryConfig{
				Enable:      false,
				MaxAttempts: 0,
			},
			wantErr: false,
		},
		{
			name: "enabled with minimal config",
			config: RetryConfig{
				Enable:      true,
				MaxAttempts: 1,
			},
			wantErr: false,
		},
		{
			name: "enabled but zero max_attempts",
			config: RetryConfig{
				Enable:      true,
				MaxAttempts: 0,
			},
			wantErr: true,
			errMsg:  "max_attempts",
		},
		{
			name: "invalid backoff_base",
			config: RetryConfig{
				Enable:      true,
				MaxAttempts: 3,
				BackoffBase: "invalid",
			},
			wantErr: true,
			errMsg:  "backoff_base",
		},
		{
			name: "invalid backoff_max",
			config: RetryConfig{
				Enable:      true,
				MaxAttempts: 3,
				BackoffMax:  "not-a-duration",
			},
			wantErr: true,
			errMsg:  "backoff_max",
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

// TestMiddlewareConfig_Validate 测试 Middleware 配置验证
func TestMiddlewareConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  MiddlewareConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: MiddlewareConfig{
				RateLimit:      true,
				RateLimitRate:  100,
				RateLimitBurst: 200,
			},
			wantErr: false,
		},
		{
			name: "rate limit disabled - no validation",
			config: MiddlewareConfig{
				RateLimit:      false,
				RateLimitRate:  -1, // Should not be validated
				RateLimitBurst: -1,
			},
			wantErr: false,
		},
		{
			name: "negative rate",
			config: MiddlewareConfig{
				RateLimit:     true,
				RateLimitRate: -1,
			},
			wantErr: true,
			errMsg:  "rate_limit_rate",
		},
		{
			name: "negative burst",
			config: MiddlewareConfig{
				RateLimit:      true,
				RateLimitRate:  100,
				RateLimitBurst: -1,
			},
			wantErr: true,
			errMsg:  "rate_limit_burst",
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

// TestDeadLetterConfig_Validate 测试 DeadLetter 配置验证
func TestDeadLetterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  DeadLetterConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid file target",
			config: DeadLetterConfig{
				Enable:   true,
				Target:   "file",
				FilePath: "/tmp/dlq.log",
			},
			wantErr: false,
		},
		{
			name: "valid kafka target",
			config: DeadLetterConfig{
				Enable:       true,
				Target:       "kafka",
				KafkaBrokers: []string{"localhost:9092"},
				KafkaTopic:   "dlq-topic",
			},
			// KafkaConsumer 尚未实现，配置验证应拒绝此 target
			wantErr: true,
			errMsg:  "not yet implemented",
		},
		{
			name: "valid webhook target",
			config: DeadLetterConfig{
				Enable:     true,
				Target:     "webhook",
				WebhookURL: "https://example.com/dlq",
			},
			wantErr: false,
		},
		{
			name: "disabled - no validation",
			config: DeadLetterConfig{
				Enable: false,
			},
			wantErr: false,
		},
		{
			name: "invalid target",
			config: DeadLetterConfig{
				Enable: true,
				Target: "redis",
			},
			wantErr: true,
			errMsg:  "target must be one of",
		},
		{
			name: "file target without filepath",
			config: DeadLetterConfig{
				Enable: true,
				Target: "file",
			},
			wantErr: true,
			errMsg:  "file_path is required",
		},
		{
			name: "kafka target without brokers",
			config: DeadLetterConfig{
				Enable:     true,
				Target:     "kafka",
				KafkaTopic: "topic",
			},
			wantErr: true,
			errMsg:  "not yet implemented",
		},
		{
			name: "kafka target without topic",
			config: DeadLetterConfig{
				Enable:       true,
				Target:       "kafka",
				KafkaBrokers: []string{"localhost:9092"},
			},
			wantErr: true,
			errMsg:  "not yet implemented",
		},
		{
			name: "webhook target without url",
			config: DeadLetterConfig{
				Enable: true,
				Target: "webhook",
			},
			wantErr: true,
			errMsg:  "webhook_url is required",
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

// TestWebhookConfig_Validate 测试 Webhook 配置验证
func TestWebhookConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  WebhookConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with dedup enabled",
			config: WebhookConfig{
				EventBuffer:      1000,
				DedupEnable:      true,
				Shards:           16,
				LifeWindow:       "5m",
				CleanWindow:      "1m",
				MaxEntrySize:     1024,
				HardMaxCacheSize: 10000,
			},
			wantErr: false,
		},
		{
			name: "dedup disabled - no validation",
			config: WebhookConfig{
				EventBuffer: 1000,
				DedupEnable: false,
			},
			wantErr: false,
		},
		{
			name: "negative event_buffer",
			config: WebhookConfig{
				EventBuffer: -1,
			},
			wantErr: true,
			errMsg:  "event_buffer",
		},
		{
			name: "negative shards",
			config: WebhookConfig{
				DedupEnable: true,
				Shards:      -1,
			},
			wantErr: true,
			errMsg:  "dedup_shards",
		},
		{
			name: "invalid life_window",
			config: WebhookConfig{
				DedupEnable: true,
				LifeWindow:  "invalid",
			},
			wantErr: true,
			errMsg:  "dedup_life_window",
		},
		{
			name: "invalid clean_window",
			config: WebhookConfig{
				DedupEnable: true,
				CleanWindow: "not-duration",
			},
			wantErr: true,
			errMsg:  "dedup_clean_window",
		},
		{
			name: "negative max_entry_size",
			config: WebhookConfig{
				DedupEnable:  true,
				MaxEntrySize: -1,
			},
			wantErr: true,
			errMsg:  "dedup_max_entry_size",
		},
		{
			name: "negative hard_max_cache_size",
			config: WebhookConfig{
				DedupEnable:      true,
				HardMaxCacheSize: -1,
			},
			wantErr: true,
			errMsg:  "dedup_hard_max_size",
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

// TestConfig_Validate 测试完整配置验证
func TestConfig_Validate(t *testing.T) {
	validConfig := Config{
		Bot: BotConfig{
			AppID:  123456,
			BotID:  789012,
			Token:  "test-token",
			Secret: "test-secret",
		},
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Concurrency: ConcurrencyConfig{
			Limit:  100,
			Policy: "drop",
		},
		Retry: RetryConfig{
			Enable:      true,
			MaxAttempts: 3,
		},
		Middleware: MiddlewareConfig{
			RateLimit:      true,
			RateLimitRate:  100,
			RateLimitBurst: 200,
		},
		DeadLetter: DeadLetterConfig{
			Enable:   true,
			Target:   "file",
			FilePath: "/tmp/dlq.log",
		},
		Webhook: WebhookConfig{
			EventBuffer: 1000,
		},
	}

	t.Run("valid complete config", func(t *testing.T) {
		err := validConfig.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid bot config", func(t *testing.T) {
		cfg := validConfig
		cfg.Bot.Token = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bot config")
	})

	t.Run("invalid server config", func(t *testing.T) {
		cfg := validConfig
		cfg.Server.Port = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server config")
	})

	t.Run("invalid log config", func(t *testing.T) {
		cfg := validConfig
		cfg.Log.Level = "invalid"
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log config")
	})
}

// TestSubscribe_NotifiesOnLoad 测试 Subscribe 在 Load 后触发通知
func TestSubscribe_NotifiesOnLoad(t *testing.T) {
	// 清理状态
	UnsubscribeAll()
	defer UnsubscribeAll()

	var called int
	var receivedCfg *Config

	Subscribe(func(cfg *Config) {
		called++
		receivedCfg = cfg
	})

	// 创建临时配置文件
	content := `
bot:
  app_id: 123456
  bot_id: 789012
  token: "test-token"
  secret: "test-secret"
server:
  port: 8080
`
	tmpFile := t.TempDir() + "/test_config.yaml"
	if err := writeFile(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	_, err := Load(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, 1, called)
	assert.NotNil(t, receivedCfg)
	assert.Equal(t, uint64(123456), receivedCfg.Bot.AppID)
}

// TestSubscribe_MultipleListeners 测试多个监听器
func TestSubscribe_MultipleListeners(t *testing.T) {
	UnsubscribeAll()
	defer UnsubscribeAll()

	count := 0
	Subscribe(func(cfg *Config) { count++ })
	Subscribe(func(cfg *Config) { count++ })
	Subscribe(func(cfg *Config) { count++ })

	content := `
bot:
  app_id: 111
  bot_id: 222
  token: "t"
  secret: "s"
server:
  port: 8080
`
	tmpFile := t.TempDir() + "/multi_listener_config.yaml"
	if err := writeFile(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	_, err := Load(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, 3, count)
}

// TestSubscribe_PanicInListenerDoesNotBreakLoad 测试监听器 panic 不影响 Load 流程
func TestSubscribe_PanicInListenerDoesNotBreakLoad(t *testing.T) {
	UnsubscribeAll()
	defer UnsubscribeAll()

	Subscribe(func(cfg *Config) {
		panic("listener panic")
	})

	var normalCalled bool
	Subscribe(func(cfg *Config) {
		normalCalled = true
	})

	content := `
bot:
  app_id: 111
  bot_id: 222
  token: "t"
  secret: "s"
server:
  port: 8080
`
	tmpFile := t.TempDir() + "/panic_listener_config.yaml"
	if err := writeFile(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	_, err := Load(tmpFile)
	assert.NoError(t, err, "Load should succeed even if listener panics")
	assert.True(t, normalCalled, "subsequent listeners should still be called")
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
