package config

import (
	"os"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/stretchr/testify/assert"
)

// TestQQConfig_Validate 测试 QQ 平台配置验证
func TestQQConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  QQConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: QQConfig{
				AppID:  123456,
				BotID:  789012,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "missing app_id",
			config: QQConfig{
				BotID:  789012,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "app_id",
		},
		{
			name: "missing bot_id",
			config: QQConfig{
				AppID:  123456,
				Token:  "test-token",
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "bot_id",
		},
		{
			name: "missing token",
			config: QQConfig{
				AppID:  123456,
				BotID:  789012,
				Secret: "test-secret",
			},
			wantErr: true,
			errMsg:  "token",
		},
		{
			name: "missing secret",
			config: QQConfig{
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

// TestLogConfig_Validate 测试 Log 配置验证
func TestLogConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  logger.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid level - debug",
			config:  logger.Config{Level: "debug", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - info",
			config:  logger.Config{Level: "info", Format: "json"},
			wantErr: false,
		},
		{
			name:    "valid level - warn",
			config:  logger.Config{Level: "warn", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - error",
			config:  logger.Config{Level: "error", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - fatal",
			config:  logger.Config{Level: "fatal", Format: "text"},
			wantErr: false,
		},
		{
			name:    "valid level - panic",
			config:  logger.Config{Level: "panic", Format: "text"},
			wantErr: false,
		},
		{
			name:    "empty level is valid",
			config:  logger.Config{Format: "text"},
			wantErr: false,
		},
		{
			name:    "empty format is valid",
			config:  logger.Config{Level: "info"},
			wantErr: false,
		},
		{
			name:    "invalid level",
			config:  logger.Config{Level: "verbose", Format: "text"},
			wantErr: true,
			errMsg:  "log.level",
		},
		{
			name:    "invalid format",
			config:  logger.Config{Level: "info", Format: "xml"},
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

// TestBackpressureConfig_Validate 测试 Backpressure 配置验证
func TestBackpressureConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BackpressureConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BackpressureConfig{
				Limit:       100,
				Policy:      "drop",
				WaitTimeout: "5s",
			},
			wantErr: false,
		},
		{
			name: "valid policy - block",
			config: BackpressureConfig{
				Limit:  50,
				Policy: "block",
			},
			wantErr: false,
		},
		{
			name: "valid policy - trywait",
			config: BackpressureConfig{
				Limit:       50,
				Policy:      "trywait",
				WaitTimeout: "10s",
			},
			wantErr: false,
		},
		{
			name: "empty policy is valid",
			config: BackpressureConfig{
				Limit: 50,
			},
			wantErr: false,
		},
		{
			name: "zero limit is valid",
			config: BackpressureConfig{
				Limit:  0,
				Policy: "drop",
			},
			wantErr: false,
		},
		{
			name: "negative limit",
			config: BackpressureConfig{
				Limit:  -1,
				Policy: "drop",
			},
			wantErr: true,
			errMsg:  "backpressure.limit",
		},
		{
			name: "invalid policy",
			config: BackpressureConfig{
				Limit:  50,
				Policy: "reject",
			},
			wantErr: true,
			errMsg:  "backpressure.policy",
		},
		{
			name: "invalid wait_timeout",
			config: BackpressureConfig{
				Limit:       50,
				Policy:      "trywait",
				WaitTimeout: "invalid",
			},
			wantErr: true,
			errMsg:  "backpressure.wait_timeout",
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
				RateLimit: RateLimitMiddlewareConfig{
					Enable: true,
					Rate:   100,
					Burst:  200,
				},
			},
			wantErr: false,
		},
		{
			name: "rate limit disabled - no validation",
			config: MiddlewareConfig{
				RateLimit: RateLimitMiddlewareConfig{
					Enable: false,
					Rate:   -1, // Should not be validated
					Burst:  -1,
				},
			},
			wantErr: false,
		},
		{
			name: "negative rate",
			config: MiddlewareConfig{
				RateLimit: RateLimitMiddlewareConfig{
					Enable: true,
					Rate:   -1,
				},
			},
			wantErr: true,
			errMsg:  "rate_limit.rate",
		},
		{
			name: "negative burst",
			config: MiddlewareConfig{
				RateLimit: RateLimitMiddlewareConfig{
					Enable: true,
					Rate:   100,
					Burst:  -1,
				},
			},
			wantErr: true,
			errMsg:  "rate_limit.burst",
		},
		{
			name: "degradation cpu_threshold out of range",
			config: MiddlewareConfig{
				Degradation: DegradationConfig{
					Enable:       true,
					CPUThreshold: 150.0,
				},
			},
			wantErr: true,
			errMsg:  "cpu_threshold",
		},
		{
			name: "degradation memory_threshold out of range",
			config: MiddlewareConfig{
				Degradation: DegradationConfig{
					Enable:          true,
					MemoryThreshold: -5.0,
				},
			},
			wantErr: true,
			errMsg:  "memory_threshold",
		},
		{
			name: "valid degradation thresholds",
			config: MiddlewareConfig{
				Degradation: DegradationConfig{
					Enable:          true,
					CPUThreshold:    80.0,
					MemoryThreshold: 85.0,
				},
			},
			wantErr: false,
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
			name: "valid config",
			config: WebhookConfig{
				Port:        8080,
				EventBuffer: 1000,
			},
			wantErr: false,
		},
		{
			name: "valid with shutdown timeout",
			config: WebhookConfig{
				Port:            8080,
				ShutdownTimeout: "10s",
			},
			wantErr: false,
		},
		{
			name: "zero port is valid (not configured)",
			config: WebhookConfig{
				Port: 0,
			},
			wantErr: false,
		},
		{
			name: "port too high",
			config: WebhookConfig{
				Port: 99999,
			},
			wantErr: true,
			errMsg:  "webhook.port",
		},
		{
			name: "invalid shutdown_timeout",
			config: WebhookConfig{
				Port:            8080,
				ShutdownTimeout: "invalid",
			},
			wantErr: true,
			errMsg:  "webhook.shutdown_timeout",
		},
		{
			name: "negative event_buffer",
			config: WebhookConfig{
				Port:        8080,
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

// TestConfig_Validate 测试完整配置验证
func TestConfig_Validate(t *testing.T) {
	newValidConfig := func() Config {
		return Config{
			Bot: BotConfig{
		QQ: &QQConfig{
				AppID:  123456,
				BotID:  789012,
				Token:  "test-token",
				Secret: "test-secret",
				Webhook: WebhookConfig{
					Host:        "localhost",
					Port:        8080,
					EventBuffer: 1000,
				},
			},
			},
			Log: logger.Config{
				Level:  "info",
				Format: "json",
			},
			Middleware: MiddlewareConfig{
				RateLimit: RateLimitMiddlewareConfig{
					Enable: true,
					Rate:   100,
					Burst:  200,
				},
				Backpressure: BackpressureConfig{
					Limit:  100,
					Policy: "drop",
				},
			},
			Retry: RetryConfig{
				Enable:      true,
				MaxAttempts: 3,
			},
			DeadLetter: DeadLetterConfig{
				Enable:   true,
				Target:   "file",
				FilePath: "/tmp/dlq.log",
			},
		}
	}

	t.Run("valid complete config", func(t *testing.T) {
		cfg := newValidConfig()
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid qq config", func(t *testing.T) {
		cfg := newValidConfig()
		cfg.Bot.QQ.Token = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "qq config")
	})

	t.Run("invalid webhook port", func(t *testing.T) {
		cfg := newValidConfig()
		cfg.Bot.QQ.Webhook.Port = 99999
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "webhook.port")
	})

	t.Run("invalid log config", func(t *testing.T) {
		cfg := newValidConfig()
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
  qq:
    app_id: 123456
    bot_id: 789012
    token: "test-token"
    secret: "test-secret"
    webhook:
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
	assert.Equal(t, uint64(123456), receivedCfg.Bot.QQ.AppID)
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
  qq:
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
  qq:
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
