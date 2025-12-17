package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/fsnotify/fsnotify"
)

// Config 配置文件结构
type Config struct {
	Bot         BotConfig         `yaml:"bot"`
	Server      ServerConfig      `yaml:"server"`
	Log         LogConfig         `yaml:"log"`
	Concurrency ConcurrencyConfig `yaml:"concurrency"`
	Retry       RetryConfig       `yaml:"retry"`
	Middleware  MiddlewareConfig  `yaml:"middleware"`
	DeadLetter  DeadLetterConfig  `yaml:"dead_letter"`
	Webhook     WebhookConfig     `yaml:"webhook"`
}

// BotConfig Bot 配置
type BotConfig struct {
	AppID  uint64 `yaml:"app_id"`
	BotID  uint64 `yaml:"bot_id"`
	Token  string `yaml:"token"`
	Secret string `yaml:"secret"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// ConcurrencyConfig 并发/反压配置（可选）
type ConcurrencyConfig struct {
	Limit       int    `yaml:"limit"`        // 最大并发；<=0 表示不限制
	Policy      string `yaml:"policy"`       // drop|block|trywait
	WaitTimeout string `yaml:"wait_timeout"` // 例如 "200ms"、"1s"
	EventBuffer int    `yaml:"event_buffer"` // webhook eventChan 缓冲大小
}

// RetryConfig 重试与死信队列配置（可选）
type RetryConfig struct {
	Enable      bool   `yaml:"enable"`
	MaxAttempts int    `yaml:"max_attempts"`
	BackoffBase string `yaml:"backoff_base"` // "200ms"
	BackoffMax  string `yaml:"backoff_max"`  // "2s"
}

// MiddlewareConfig 中间件开关（可选）
type MiddlewareConfig struct {
	Logging        bool     `yaml:"logging"`
	Recover        bool     `yaml:"recover"`
	Auth           bool     `yaml:"auth"`
	AuthWhitelist  []string `yaml:"auth_whitelist"` // 允许的用户ID/群ID
	RateLimit      bool     `yaml:"rate_limit"`
	RateLimitRate  int      `yaml:"rate_limit_rate"`  // 每秒令牌
	RateLimitBurst int      `yaml:"rate_limit_burst"` // 峰值突发
	Metrics        bool     `yaml:"metrics"`
}

// DeadLetterConfig 死信队列配置
type DeadLetterConfig struct {
	Enable       bool     `yaml:"enable"`
	Target       string   `yaml:"target"` // file|kafka|webhook
	FilePath     string   `yaml:"file_path"`
	KafkaBrokers []string `yaml:"kafka_brokers"`
	KafkaTopic   string   `yaml:"kafka_topic"`
	WebhookURL   string   `yaml:"webhook_url"`
}

// WebhookConfig webhook 配置
type WebhookConfig struct {
	EventBuffer      int    `yaml:"event_buffer"`
	DedupEnable      bool   `yaml:"dedup_enable"`
	Shards           int    `yaml:"dedup_shards"`
	LifeWindow       string `yaml:"dedup_life_window"`    // e.g., "5m"
	CleanWindow      string `yaml:"dedup_clean_window"`   // e.g., "1m"
	MaxEntrySize     int    `yaml:"dedup_max_entry_size"` // bytes
	HardMaxCacheSize int    `yaml:"dedup_hard_max_size"`  // MB
}

var globalConfig *Config

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// 验证必填字段
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	globalConfig = &cfg
	return &cfg, nil
}

// Get 获取全局配置（需要先调用 Load）
func Get() *Config {
	return globalConfig
}

// Validate 验证配置的有效性
//
// 验证规则：
// - Bot 配置：必填字段验证
// - Server 配置：端口范围验证
// - Log 配置：级别和格式验证
// - Concurrency 配置：参数范围验证
// - Retry 配置：参数有效性验证
// - Middleware 配置：参数合理性验证
func (c *Config) Validate() error {
	// 验证 Bot 配置
	if err := c.Bot.Validate(); err != nil {
		return fmt.Errorf("invalid bot config: %w", err)
	}

	// 验证 Server 配置
	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("invalid server config: %w", err)
	}

	// 验证 Log 配置
	if err := c.Log.Validate(); err != nil {
		return fmt.Errorf("invalid log config: %w", err)
	}

	// 验证 Concurrency 配置
	if err := c.Concurrency.Validate(); err != nil {
		return fmt.Errorf("invalid concurrency config: %w", err)
	}

	// 验证 Retry 配置
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("invalid retry config: %w", err)
	}

	// 验证 Middleware 配置
	if err := c.Middleware.Validate(); err != nil {
		return fmt.Errorf("invalid middleware config: %w", err)
	}

	// 验证 DeadLetter 配置
	if err := c.DeadLetter.Validate(); err != nil {
		return fmt.Errorf("invalid dead_letter config: %w", err)
	}

	// 验证 Webhook 配置
	if err := c.Webhook.Validate(); err != nil {
		return fmt.Errorf("invalid webhook config: %w", err)
	}

	return nil
}

// Validate 验证 Bot 配置
func (bc *BotConfig) Validate() error {
	if bc.AppID == 0 {
		return fmt.Errorf("bot.app_id is required and must be non-zero")
	}
	if bc.BotID == 0 {
		return fmt.Errorf("bot.bot_id is required and must be non-zero")
	}
	if bc.Token == "" {
		return fmt.Errorf("bot.token is required and cannot be empty")
	}
	if bc.Secret == "" {
		return fmt.Errorf("bot.secret is required and cannot be empty")
	}
	return nil
}

// Validate 验证 Server 配置
func (sc *ServerConfig) Validate() error {
	if sc.Port < 1 || sc.Port > 65535 {
		return fmt.Errorf("server.port must be between 1-65535, got %d", sc.Port)
	}
	// Host 允许为空，默认为 0.0.0.0
	return nil
}

// Validate 验证 Log 配置
func (lc *LogConfig) Validate() error {
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true, "panic": true,
	}
	if lc.Level != "" && !validLevels[lc.Level] {
		return fmt.Errorf("log.level must be one of [debug, info, warn, error, fatal, panic], got '%s'", lc.Level)
	}

	validFormats := map[string]bool{
		"text": true, "json": true,
	}
	if lc.Format != "" && !validFormats[lc.Format] {
		return fmt.Errorf("log.format must be one of [text, json], got '%s'", lc.Format)
	}

	return nil
}

// Validate 验证 Concurrency 配置
func (cc *ConcurrencyConfig) Validate() error {
	if cc.Limit < 0 {
		return fmt.Errorf("concurrency.limit must be >= 0, got %d", cc.Limit)
	}

	validPolicies := map[string]bool{
		"drop": true, "block": true, "trywait": true, "": true,
	}
	if !validPolicies[cc.Policy] {
		return fmt.Errorf("concurrency.policy must be one of [drop, block, trywait], got '%s'", cc.Policy)
	}

	// 验证 WaitTimeout 格式（如果设置）
	if cc.WaitTimeout != "" {
		if _, err := time.ParseDuration(cc.WaitTimeout); err != nil {
			return fmt.Errorf("concurrency.wait_timeout is not a valid duration: %w", err)
		}
	}

	if cc.EventBuffer < 0 {
		return fmt.Errorf("concurrency.event_buffer must be >= 0, got %d", cc.EventBuffer)
	}

	return nil
}

// Validate 验证 Retry 配置
func (rc *RetryConfig) Validate() error {
	if rc.Enable {
		if rc.MaxAttempts < 1 {
			return fmt.Errorf("retry.max_attempts must be >= 1 when retry is enabled, got %d", rc.MaxAttempts)
		}

		// 验证 BackoffBase 格式
		if rc.BackoffBase != "" {
			if _, err := time.ParseDuration(rc.BackoffBase); err != nil {
				return fmt.Errorf("retry.backoff_base is not a valid duration: %w", err)
			}
		}

		// 验证 BackoffMax 格式
		if rc.BackoffMax != "" {
			if _, err := time.ParseDuration(rc.BackoffMax); err != nil {
				return fmt.Errorf("retry.backoff_max is not a valid duration: %w", err)
			}
		}
	}
	return nil
}

// Validate 验证 Middleware 配置
func (mc *MiddlewareConfig) Validate() error {
	if mc.RateLimit {
		if mc.RateLimitRate < 0 {
			return fmt.Errorf("middleware.rate_limit_rate must be >= 0, got %d", mc.RateLimitRate)
		}
		if mc.RateLimitBurst < 0 {
			return fmt.Errorf("middleware.rate_limit_burst must be >= 0, got %d", mc.RateLimitBurst)
		}
	}
	return nil
}

// Validate 验证 DeadLetter 配置
func (dlc *DeadLetterConfig) Validate() error {
	if dlc.Enable {
		validTargets := map[string]bool{
			"file": true, "kafka": true, "webhook": true,
		}
		if !validTargets[dlc.Target] {
			return fmt.Errorf("dead_letter.target must be one of [file, kafka, webhook], got '%s'", dlc.Target)
		}

		// 根据 target 类型验证相关配置
		switch dlc.Target {
		case "file":
			if dlc.FilePath == "" {
				return fmt.Errorf("dead_letter.file_path is required when target is 'file'")
			}
		case "kafka":
			if len(dlc.KafkaBrokers) == 0 {
				return fmt.Errorf("dead_letter.kafka_brokers is required when target is 'kafka'")
			}
			if dlc.KafkaTopic == "" {
				return fmt.Errorf("dead_letter.kafka_topic is required when target is 'kafka'")
			}
		case "webhook":
			if dlc.WebhookURL == "" {
				return fmt.Errorf("dead_letter.webhook_url is required when target is 'webhook'")
			}
		}
	}
	return nil
}

// Validate 验证 Webhook 配置
func (wc *WebhookConfig) Validate() error {
	if wc.EventBuffer < 0 {
		return fmt.Errorf("webhook.event_buffer must be >= 0, got %d", wc.EventBuffer)
	}

	if wc.DedupEnable {
		// 验证分片数
		if wc.Shards < 0 {
			return fmt.Errorf("webhook.dedup_shards must be >= 0, got %d", wc.Shards)
		}

		// 验证 LifeWindow 格式
		if wc.LifeWindow != "" {
			if _, err := time.ParseDuration(wc.LifeWindow); err != nil {
				return fmt.Errorf("webhook.dedup_life_window is not a valid duration: %w", err)
			}
		}

		// 验证 CleanWindow 格式
		if wc.CleanWindow != "" {
			if _, err := time.ParseDuration(wc.CleanWindow); err != nil {
				return fmt.Errorf("webhook.dedup_clean_window is not a valid duration: %w", err)
			}
		}

		// 验证 MaxEntrySize
		if wc.MaxEntrySize < 0 {
			return fmt.Errorf("webhook.dedup_max_entry_size must be >= 0, got %d", wc.MaxEntrySize)
		}

		// 验证 HardMaxCacheSize
		if wc.HardMaxCacheSize < 0 {
			return fmt.Errorf("webhook.dedup_hard_max_size must be >= 0, got %d", wc.HardMaxCacheSize)
		}
	}

	return nil
}

// LoadDefault 尝试从默认位置加载配置
// 按优先级查找: ./config.yaml -> ./config.yml -> 环境变量
func LoadDefault() (*Config, error) {
	// 尝试从文件加载
	for _, path := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
	}

	// 尝试从环境变量加载
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

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("no config file found and environment variables incomplete: %w", err)
	}

	globalConfig = cfg
	return cfg, nil
}

// Watch 监听配置文件变更并热重载
// path: 配置文件路径
// apply: 成功重载后的回调（将新的配置传入），可用于应用到运行中的 Bot/Engine
// 返回停止函数：调用后停止监听
func Watch(path string, apply func(*Config)) (func() error, error) {
	if path == "" {
		return nil, fmt.Errorf("watch path is empty")
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}
	if err := w.Add(path); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("failed to add watch on %s: %w", path, err)
	}

	stop := make(chan struct{})

	go func() {
		defer w.Close()
		// 简单抖动消除：文件事件合并
		var timer *time.Timer
		debounce := func() {
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(200*time.Millisecond, func() {
				// 执行重载
				if cfg, err := Load(path); err == nil {
					if apply != nil {
						apply(cfg)
					}
				}
			})
		}

		for {
			select {
			case <-stop:
				return
			case ev := <-w.Events:
				// 关注写入/重命名/创建事件
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					debounce()
				}
			case err := <-w.Errors:
				_ = err // 可选：记录日志
			}
		}
	}()

	return func() error { close(stop); return nil }, nil
}

// 辅助函数
func getEnvUint64(key string) uint64 {
	val := os.Getenv(key)
	if val == "" {
		return 0
	}
	var result uint64
	fmt.Sscanf(val, "%d", &result)
	return result
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var result int
	fmt.Sscanf(val, "%d", &result)
	return result
}

func getEnvDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
