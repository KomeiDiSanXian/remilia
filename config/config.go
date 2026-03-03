package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config 配置文件结构
type Config struct {
	Bot         BotConfig         `yaml:"bot" mapstructure:"bot"`
	Server      ServerConfig      `yaml:"server" mapstructure:"server"`
	Log         LogConfig         `yaml:"log" mapstructure:"log"`
	Concurrency ConcurrencyConfig `yaml:"concurrency" mapstructure:"concurrency"`
	Retry       RetryConfig       `yaml:"retry" mapstructure:"retry"`
	Middleware  MiddlewareConfig  `yaml:"middleware" mapstructure:"middleware"`
	DeadLetter  DeadLetterConfig  `yaml:"dead_letter" mapstructure:"dead_letter"`
	Webhook     WebhookConfig     `yaml:"webhook" mapstructure:"webhook"`
	Token       TokenConfig       `yaml:"token" mapstructure:"token"`
	Engine      EngineConfig      `yaml:"engine" mapstructure:"engine"`
	Degradation DegradationConfig `yaml:"degradation" mapstructure:"degradation"`
	Tracing     TracingConfig     `yaml:"tracing" mapstructure:"tracing"`
}

// BotConfig Bot 配置
type BotConfig struct {
	AppID  uint64 `yaml:"app_id" mapstructure:"app_id"`
	BotID  uint64 `yaml:"bot_id" mapstructure:"bot_id"`
	Token  string `yaml:"token" mapstructure:"token"`
	Secret string `yaml:"secret" mapstructure:"secret"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host            string `yaml:"host" mapstructure:"host"`
	Port            int    `yaml:"port" mapstructure:"port"`
	ShutdownTimeout string `yaml:"shutdown_timeout" mapstructure:"shutdown_timeout"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `yaml:"level" mapstructure:"level"`
	Format string `yaml:"format" mapstructure:"format"`
}

// ConcurrencyConfig 并发/反压配置（可选）
type ConcurrencyConfig struct {
	Limit       int    `yaml:"limit" mapstructure:"limit"`
	Policy      string `yaml:"policy" mapstructure:"policy"`
	WaitTimeout string `yaml:"wait_timeout" mapstructure:"wait_timeout"`
	EventBuffer int    `yaml:"event_buffer" mapstructure:"event_buffer"`
}

// RetryConfig 重试与死信队列配置（可选）
type RetryConfig struct {
	Enable      bool   `yaml:"enable" mapstructure:"enable"`
	MaxAttempts int    `yaml:"max_attempts" mapstructure:"max_attempts"`
	BackoffBase string `yaml:"backoff_base" mapstructure:"backoff_base"`
	BackoffMax  string `yaml:"backoff_max" mapstructure:"backoff_max"`
}

// MiddlewareConfig 中间件开关（可选）
type MiddlewareConfig struct {
	Logging                  bool     `yaml:"logging" mapstructure:"logging"`
	Recover                  bool     `yaml:"recover" mapstructure:"recover"`
	Auth                     bool     `yaml:"auth" mapstructure:"auth"`
	AuthWhitelist            []string `yaml:"auth_whitelist" mapstructure:"auth_whitelist"`
	RateLimit                bool     `yaml:"rate_limit" mapstructure:"rate_limit"`
	RateLimitRate            int      `yaml:"rate_limit_rate" mapstructure:"rate_limit_rate"`
	RateLimitBurst           int      `yaml:"rate_limit_burst" mapstructure:"rate_limit_burst"`
	RateLimitMaxBuckets      int      `yaml:"rate_limit_max_buckets" mapstructure:"rate_limit_max_buckets"`
	RateLimitBucketTTL       string   `yaml:"rate_limit_bucket_ttl" mapstructure:"rate_limit_bucket_ttl"`
	RateLimitCleanupInterval string   `yaml:"rate_limit_cleanup_interval" mapstructure:"rate_limit_cleanup_interval"`
	DedupEnable              bool     `yaml:"dedup_enable" mapstructure:"dedup_enable"`
	DedupMaxSize             int      `yaml:"dedup_max_size" mapstructure:"dedup_max_size"`
	DedupDefaultTTL          string   `yaml:"dedup_default_ttl" mapstructure:"dedup_default_ttl"`
	DedupCleanupInterval     string   `yaml:"dedup_cleanup_interval" mapstructure:"dedup_cleanup_interval"`
	SlowHandlerEnable        bool     `yaml:"slow_handler_enable" mapstructure:"slow_handler_enable"`
	SlowHandlerThreshold     string   `yaml:"slow_handler_threshold" mapstructure:"slow_handler_threshold"`
	Metrics                  bool     `yaml:"metrics" mapstructure:"metrics"`
	// DegradationCPUThreshold CPU 使用率阈值（百分比，0-100），用于自适应降级热更新
	DegradationCPUThreshold float64 `yaml:"degradation_cpu_threshold" mapstructure:"degradation_cpu_threshold"`
	// DegradationMemoryThreshold 内存使用率阈值（百分比，0-100），用于自适应降级热更新
	DegradationMemoryThreshold float64 `yaml:"degradation_memory_threshold" mapstructure:"degradation_memory_threshold"`
}

// DeadLetterConfig 死信队列配置
type DeadLetterConfig struct {
	Enable       bool     `yaml:"enable" mapstructure:"enable"`
	Target       string   `yaml:"target" mapstructure:"target"`
	FilePath     string   `yaml:"file_path" mapstructure:"file_path"`
	KafkaBrokers []string `yaml:"kafka_brokers" mapstructure:"kafka_brokers"`
	KafkaTopic   string   `yaml:"kafka_topic" mapstructure:"kafka_topic"`
	WebhookURL   string   `yaml:"webhook_url" mapstructure:"webhook_url"`
}

// TokenConfig Token 管理器配置
type TokenConfig struct {
	RetryDelay      string  `yaml:"retry_delay" mapstructure:"retry_delay"`
	RefreshAdvance  string  `yaml:"refresh_advance" mapstructure:"refresh_advance"`
	MinRefreshRatio float64 `yaml:"min_refresh_ratio" mapstructure:"min_refresh_ratio"`
}

// WebhookConfig Webhook 配置
type WebhookConfig struct {
	EventBuffer        int    `yaml:"event_buffer" mapstructure:"event_buffer"`
	WorkerCount        int    `yaml:"worker_count" mapstructure:"worker_count"`
	DedupEnable        bool   `yaml:"dedup_enable" mapstructure:"dedup_enable"`
	Shards             int    `yaml:"shards" mapstructure:"shards"`
	LifeWindow         string `yaml:"life_window" mapstructure:"life_window"`
	CleanWindow        string `yaml:"clean_window" mapstructure:"clean_window"`
	MaxEntrySize       int    `yaml:"max_entry_size" mapstructure:"max_entry_size"`
	HardMaxCacheSize   int    `yaml:"hard_max_cache_size" mapstructure:"hard_max_cache_size"`
	MaxEntriesInWindow int    `yaml:"max_entries_in_window" mapstructure:"max_entries_in_window"`
}

// EngineConfig engine 引擎配置
type EngineConfig struct {
	TempMatcherCleanupInterval   string `yaml:"temp_matcher_cleanup_interval" mapstructure:"temp_matcher_cleanup_interval"`
	PendingDeleteBufferSize      int    `yaml:"pending_delete_buffer_size" mapstructure:"pending_delete_buffer_size"`
	PendingDeleteProcessInterval string `yaml:"pending_delete_process_interval" mapstructure:"pending_delete_process_interval"`
	PendingDeleteBatchSize       int    `yaml:"pending_delete_batch_size" mapstructure:"pending_delete_batch_size"`
	MatcherPoolCapacity          int    `yaml:"matcher_pool_capacity" mapstructure:"matcher_pool_capacity"`
	MatcherPoolMaxCapacity       int    `yaml:"matcher_pool_max_capacity" mapstructure:"matcher_pool_max_capacity"`
	TempMatcherShardCount        int    `yaml:"temp_matcher_shard_count" mapstructure:"temp_matcher_shard_count"`
}

// DegradationConfig 自适应降级配置
type DegradationConfig struct {
	Enable             bool    `yaml:"enable" mapstructure:"enable"`
	CPUThreshold       float64 `yaml:"cpu_threshold" mapstructure:"cpu_threshold"`
	MemoryThreshold    float64 `yaml:"memory_threshold" mapstructure:"memory_threshold"`
	LatencyThreshold   string  `yaml:"latency_threshold" mapstructure:"latency_threshold"`
	MonitorInterval    string  `yaml:"monitor_interval" mapstructure:"monitor_interval"`
	RecoveryInterval   string  `yaml:"recovery_interval" mapstructure:"recovery_interval"`
	DelayQueueSize     int     `yaml:"delay_queue_size" mapstructure:"delay_queue_size"`
	GoroutineThreshold int     `yaml:"goroutine_threshold" mapstructure:"goroutine_threshold"`
	Strategy           string  `yaml:"strategy" mapstructure:"strategy"`
}

// TracingConfig 分布式追踪配置
type TracingConfig struct {
	// Enable 是否启用追踪
	Enable bool `yaml:"enable" mapstructure:"enable"`

	// ServiceName 服务名称
	ServiceName string `yaml:"service_name" mapstructure:"service_name"`

	// ServiceVersion 服务版本
	ServiceVersion string `yaml:"service_version" mapstructure:"service_version"`

	// Environment 环境（dev, staging, prod）
	Environment string `yaml:"environment" mapstructure:"environment"`

	// Exporter 导出器类型（otlp, zipkin, stdout）
	Exporter string `yaml:"exporter" mapstructure:"exporter"`

	// Endpoint 追踪后端地址
	// OTLP (Tempo/Grafana): http://localhost:4318
	// Zipkin: http://localhost:9411/api/v2/spans
	Endpoint string `yaml:"endpoint" mapstructure:"endpoint"`

	// SamplingRate 采样率 (0.0 - 1.0)
	SamplingRate float64 `yaml:"sampling_rate" mapstructure:"sampling_rate"`

	// IncludeEventDetail 是否包含事件详情
	IncludeEventDetail bool `yaml:"include_event_detail" mapstructure:"include_event_detail"`

	// Headers 额外的 HTTP 头（用于 OTLP 认证）
	Headers map[string]string `yaml:"headers" mapstructure:"headers"`
}

var globalConfig atomic.Value // stores *Config

// ChangeListener 配置变更监听器函数类型
// 当配置通过 Load 或 Reload 更新时被调用
type ChangeListener func(newCfg *Config)

// listenerEntry 带 ID 的监听器条目
type listenerEntry struct {
	id int64
	fn ChangeListener
}

// ListenerToken 配置监听器注销凭证
// 调用 Cancel() 可精确移除对应的监听器，而不影响其他监听器。
type ListenerToken struct {
	id   int64
	once sync.Once
}

// Cancel 注销此监听器。多次调用是安全的（幂等）。
func (t *ListenerToken) Cancel() {
	t.once.Do(func() { unsubscribeByID(t.id) })
}

var (
	listenerEntries   []listenerEntry
	changeListenersMu sync.RWMutex
	listenerIDCounter atomic.Int64
)

// Subscribe 注册配置变更监听器，返回可用于精确取消的 token。
// 每次 Load 成功后会按注册顺序调用所有监听器。
//
// 使用示例:
//
//	token := config.Subscribe(func(cfg *config.Config) {
//	    rateLimiter.Update(cfg.Middleware.RateLimitRate)
//	})
//	// 插件卸载时：
//	defer token.Cancel()
func Subscribe(listener ChangeListener) *ListenerToken {
	id := listenerIDCounter.Add(1)
	changeListenersMu.Lock()
	listenerEntries = append(listenerEntries, listenerEntry{id: id, fn: listener})
	changeListenersMu.Unlock()
	return &ListenerToken{id: id}
}

// unsubscribeByID 按 ID 移除单个监听器（内部使用）
func unsubscribeByID(id int64) {
	changeListenersMu.Lock()
	defer changeListenersMu.Unlock()
	for i, e := range listenerEntries {
		if e.id == id {
			listenerEntries = append(listenerEntries[:i], listenerEntries[i+1:]...)
			return
		}
	}
}

// UnsubscribeAll 移除所有配置变更监听器（主要用于测试清理）
func UnsubscribeAll() {
	changeListenersMu.Lock()
	listenerEntries = nil
	changeListenersMu.Unlock()
}

// notifyListeners 通知所有已注册的监听器配置已变更
func notifyListeners(cfg *Config) {
	changeListenersMu.RLock()
	snapshot := make([]listenerEntry, len(listenerEntries))
	copy(snapshot, listenerEntries)
	changeListenersMu.RUnlock()

	for _, entry := range snapshot {
		func(fn ChangeListener) {
			defer func() {
				if r := recover(); r != nil {
					// 监听器 panic 不应影响配置加载流程
					_ = r
				}
			}()
			fn(cfg)
		}(entry.fn)
	}
}

// loadRaw 从文件读取并解析配置，不更新全局状态也不触发监听器。
// 内部使用，供 Watcher 调用以避免双重通知（修复 B7）。
func loadRaw(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errutil.Wrapf(err, "failed to read config file")
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errutil.Wrapf(err, "failed to parse config file")
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfgCopy := cfg
	return &cfgCopy, nil
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errutil.Wrapf(err, "failed to read config file")
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errutil.Wrapf(err, "failed to parse config file")
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfgCopy := cfg
	globalConfig.Store(&cfgCopy)
	notifyListeners(&cfgCopy)

	return &cfgCopy, nil
}

// Get 获取全局配置（需要先调用 Load）
// 返回配置副本和是否已加载的标志
func Get() (*Config, bool) {
	v := globalConfig.Load()
	if v == nil {
		return nil, false
	}
	cfg, ok := v.(*Config)
	if !ok || cfg == nil {
		return nil, false
	}
	cfgCopy := *cfg
	return &cfgCopy, true
}

// MustGet 获取全局配置，如果未加载则 panic
// 仅在确定已加载配置的场景下使用
func MustGet() *Config {
	cfg, ok := Get()
	if !ok {
		panic("config not loaded, please call config.Load() first")
	}
	return cfg
}

// LoadDefault 尝试从默认位置加载配置
// 按优先级查找: ./config.yaml -> ./config.yml -> 环境变量
func LoadDefault() (*Config, error) {
	for _, path := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
	}

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
			Level:  strings.ToLower(getEnvDefault("LOG_LEVEL", "info")),
			Format: strings.ToLower(getEnvDefault("LOG_FORMAT", "text")),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("no config file found and environment variables incomplete: %w", err)
	}

	globalConfig.Store(cfg)
	return cfg, nil
}

// LoadViper 使用 Viper 加载配置，支持 yaml/json/env，优先顺序：显式路径 -> 默认路径 -> 环境变量
func LoadViper(path string) (*Config, error) {
	v := viper.New()

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
	}

	v.SetEnvPrefix("REMILIA")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("load config via viper failed: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config via viper failed: %w", err)
	}

	if cfg.Bot.AppID == 0 || cfg.Bot.BotID == 0 || cfg.Bot.Token == "" || cfg.Bot.Secret == "" {
		if fallback, err := LoadDefault(); err == nil {
			cfg = *fallback
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	globalConfig.Store(&cfg)
	return &cfg, nil
}

// 辅助函数
func getEnvUint64(key string) uint64 {
	val := os.Getenv(key)
	if val == "" {
		return 0
	}
	var result uint64
	_, _ = fmt.Sscanf(val, "%d", &result)
	return result
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	var result int
	_, _ = fmt.Sscanf(val, "%d", &result)
	return result
}

func getEnvDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
