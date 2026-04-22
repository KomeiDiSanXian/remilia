// Package config 是框架的配置聚合与反序列化层。
//
// # 职责边界
//
// config 包的唯一职责是：
//  1. 从外部来源（YAML 文件、环境变量、Viper）读取原始配置
//  2. 将原始配置反序列化为统一的 [Config] 结构体
//  3. 通过全局 atomic.Value 提供并发安全的配置访问和热重载
//  4. 向监听者广播配置变更事件
//
// config 包**不负责**将配置应用到具体基础设施组件（如初始化 logger、启动 server）。
// 各 infra 包自行读取所需的配置节并完成初始化，config 包只提供数据。
//
// # 配置结构与 infra 包的对应关系
//
//	Config.Bot          → 平台适配器（zerobot-remilia/ 等具体实现读取）
//	Config.Server       → infra/server
//	Config.Log          → infra/logger（logger.Config，包含所有日志选项）
//	Config.Concurrency  → infra/server（反压/并发控制）
//	Config.Retry        → infra/dlq（死信队列重试）
//	Config.Middleware   → middleware/ 包各组件（含降级配置）
//	Config.DeadLetter   → infra/dlq
//	Config.Webhook      → 平台 Webhook 适配器
//	Config.Token        → 平台 Token 管理器
//	Config.Engine       → core/engine（通过 config.EngineOptions() 转换为 engine.Option 列表）
//	Config.Tracing      → infra/tracing（tracing.Config，含追踪所有选项）
//	Config.Plugins      → builtin/ 各插件的自定义配置
//
// # 类型说明
//
// Log 字段直接使用 logger.Config，Tracing 字段直接使用 tracing.Config。
// 调用方需要 import 对应的 infra 子包来引用这些类型。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config 配置文件结构
type Config struct {
	Bot         BotConfig         `yaml:"bot" mapstructure:"bot"`
	Server      ServerConfig      `yaml:"server" mapstructure:"server"`
	Log         logger.Config     `yaml:"log" mapstructure:"log"`
	Concurrency ConcurrencyConfig `yaml:"concurrency" mapstructure:"concurrency"`
	Retry       RetryConfig       `yaml:"retry" mapstructure:"retry"`
	Middleware  MiddlewareConfig  `yaml:"middleware" mapstructure:"middleware"`
	DeadLetter  DeadLetterConfig  `yaml:"dead_letter" mapstructure:"dead_letter"`
	Webhook     WebhookConfig     `yaml:"webhook" mapstructure:"webhook"`
	Token       TokenConfig       `yaml:"token" mapstructure:"token"`
	Engine      EngineConfig      `yaml:"engine" mapstructure:"engine"`
	Tracing     tracing.Config    `yaml:"tracing" mapstructure:"tracing"`

	// Plugins 业务插件的扩展配置节点。
	//
	// 键为插件名称，值为该插件的配置键值对（支持任意类型）。
	// 业务插件通过此字段读取外部 API Key、开关等自定义配置，无需各自定义顶层配置结构体。
	//
	// 示例 config.yaml：
	//
	//   plugins:
	//     weather:
	//       api_key: "your-api-key"
	//       timeout: 10
	//     translate:
	//       provider: "mymemory"
	//       daily_limit: 1000
	//
	// 插件读取方式：
	//
	//   cfg, _ := config.Get()
	//   apiKey, _ := cfg.PluginString("weather", "api_key")
	//   timeout, _ := cfg.PluginInt("weather", "timeout")
	Plugins map[string]map[string]any `yaml:"plugins" mapstructure:"plugins"`
}

// PluginConfig 返回指定插件的配置 map。
// 若插件未配置，返回 nil, false。
func (c *Config) PluginConfig(pluginName string) (map[string]any, bool) {
	if c == nil || c.Plugins == nil {
		return nil, false
	}
	m, ok := c.Plugins[pluginName]
	return m, ok
}

// PluginString 读取插件配置中的字符串值。
// 若未配置或类型不匹配，返回 "", false。
func (c *Config) PluginString(pluginName, key string) (string, bool) {
	m, ok := c.PluginConfig(pluginName)
	if !ok {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// PluginInt 读取插件配置中的整数值（支持 int / int64 / float64 → int 自动转换）。
// 若未配置或类型不匹配，返回 0, false。
func (c *Config) PluginInt(pluginName, key string) (int, bool) {
	m, ok := c.PluginConfig(pluginName)
	if !ok {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// PluginBool 读取插件配置中的布尔值。
// 若未配置或类型不匹配，返回 false, false。
func (c *Config) PluginBool(pluginName, key string) (bool, bool) {
	m, ok := c.PluginConfig(pluginName)
	if !ok {
		return false, false
	}
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// BotConfig Bot 平台凭证配置。
//
// 由平台适配器（如 zerobot-remilia/ 下的具体实现）在初始化时读取。
// 框架核心不直接消费此配置，适配器通过 [Config.Bot] 取得后自行传入平台 SDK。
type BotConfig struct {
	AppID  uint64 `yaml:"app_id" mapstructure:"app_id"`
	BotID  uint64 `yaml:"bot_id" mapstructure:"bot_id"`
	Token  string `yaml:"token" mapstructure:"token"`
	Secret string `yaml:"secret" mapstructure:"secret"`
}

// ServerConfig 服务器配置。
//
// 由 infra/server 包消费，通过 server.WithConfig(cfg.Server) 应用。
type ServerConfig struct {
	Host            string `yaml:"host" mapstructure:"host"`
	Port            int    `yaml:"port" mapstructure:"port"`
	ShutdownTimeout string `yaml:"shutdown_timeout" mapstructure:"shutdown_timeout"`
}

// ConcurrencyConfig 并发/反压配置（可选）
type ConcurrencyConfig struct {
	Limit       int    `yaml:"limit" mapstructure:"limit"`
	Policy      string `yaml:"policy" mapstructure:"policy"`
	WaitTimeout string `yaml:"wait_timeout" mapstructure:"wait_timeout"`
	EventBuffer int    `yaml:"event_buffer" mapstructure:"event_buffer"`
}

// RetryConfig 重试配置（可选）
type RetryConfig struct {
	Enable      bool   `yaml:"enable" mapstructure:"enable"`
	MaxAttempts int    `yaml:"max_attempts" mapstructure:"max_attempts"`
	BackoffBase string `yaml:"backoff_base" mapstructure:"backoff_base"`
	BackoffMax  string `yaml:"backoff_max" mapstructure:"backoff_max"`
}

// MiddlewareConfig 中间件配置。
//
// 各子字段对应一个独立中间件，使用嵌套结构以避免扁平命名的歧义。
// 降级配置（Degradation）被纳入此结构，消除了原顶层 Config.Degradation 的重复定义。
type MiddlewareConfig struct {
	Logging     bool                        `yaml:"logging" mapstructure:"logging"`
	Recover     bool                        `yaml:"recover" mapstructure:"recover"`
	Metrics     bool                        `yaml:"metrics" mapstructure:"metrics"`
	Auth        AuthMiddlewareConfig        `yaml:"auth" mapstructure:"auth"`
	RateLimit   RateLimitMiddlewareConfig   `yaml:"rate_limit" mapstructure:"rate_limit"`
	Dedup       DedupMiddlewareConfig       `yaml:"dedup" mapstructure:"dedup"`
	SlowHandler SlowHandlerMiddlewareConfig `yaml:"slow_handler" mapstructure:"slow_handler"`
	// Degradation 自适应降级配置（原 Config.Degradation，整合至此处，同时支持热更新）
	Degradation DegradationConfig `yaml:"degradation" mapstructure:"degradation"`
}

// AuthMiddlewareConfig 认证中间件配置
type AuthMiddlewareConfig struct {
	Enable    bool     `yaml:"enable" mapstructure:"enable"`
	Whitelist []string `yaml:"whitelist" mapstructure:"whitelist"`
}

// RateLimitMiddlewareConfig 限流中间件配置
type RateLimitMiddlewareConfig struct {
	Enable          bool   `yaml:"enable" mapstructure:"enable"`
	Rate            int    `yaml:"rate" mapstructure:"rate"`
	Burst           int    `yaml:"burst" mapstructure:"burst"`
	MaxBuckets      int    `yaml:"max_buckets" mapstructure:"max_buckets"`
	BucketTTL       string `yaml:"bucket_ttl" mapstructure:"bucket_ttl"`
	CleanupInterval string `yaml:"cleanup_interval" mapstructure:"cleanup_interval"`
}

// DedupMiddlewareConfig 去重中间件配置
type DedupMiddlewareConfig struct {
	Enable          bool   `yaml:"enable" mapstructure:"enable"`
	MaxSize         int    `yaml:"max_size" mapstructure:"max_size"`
	DefaultTTL      string `yaml:"default_ttl" mapstructure:"default_ttl"`
	CleanupInterval string `yaml:"cleanup_interval" mapstructure:"cleanup_interval"`
}

// SlowHandlerMiddlewareConfig 慢处理器中间件配置
type SlowHandlerMiddlewareConfig struct {
	Enable    bool   `yaml:"enable" mapstructure:"enable"`
	Threshold string `yaml:"threshold" mapstructure:"threshold"`
}

// DeadLetterConfig 死信队列配置。
//
// 由 infra/dlq 包消费。
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

// EngineConfig engine 引擎配置。
//
// 由 core/engine 包消费，通过 [EngineOptions](cfg.Engine) 转换为 engine.Option 列表后应用。
// 字段含义和默认值参见 core/engine/config.go 中各 WithXxx Option 的文档。
type EngineConfig struct {
	TempMatcherCleanupInterval   string `yaml:"temp_matcher_cleanup_interval" mapstructure:"temp_matcher_cleanup_interval"`
	PendingDeleteBufferSize      int    `yaml:"pending_delete_buffer_size" mapstructure:"pending_delete_buffer_size"`
	PendingDeleteProcessInterval string `yaml:"pending_delete_process_interval" mapstructure:"pending_delete_process_interval"`
	PendingDeleteBatchSize       int    `yaml:"pending_delete_batch_size" mapstructure:"pending_delete_batch_size"`
	MatcherPoolCapacity          int    `yaml:"matcher_pool_capacity" mapstructure:"matcher_pool_capacity"`
	MatcherPoolMaxCapacity       int    `yaml:"matcher_pool_max_capacity" mapstructure:"matcher_pool_max_capacity"`
	TempMatcherShardCount        int    `yaml:"temp_matcher_shard_count" mapstructure:"temp_matcher_shard_count"`
}

// DegradationConfig 自适应降级配置。
//
// 由 middleware/ 自适应降级组件消费。
// 此类型嵌套在 [MiddlewareConfig.Degradation] 中（原顶层 Config.Degradation 已废弃）。
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

// listenerEntries、changeListenersMu、listenerIDCounter 是包级全局状态。
// 注意：在编写并行测试（t.Parallel()）时，若多个 goroutine 同时调用 Subscribe，
// 监听器会互相叠加；每个测试函数应在结束时调用 UnsubscribeAll() 或使用 token.Cancel() 清理。
// 示例（测试函数开头）：
//
//	t.Cleanup(config.UnsubscribeAll)
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

// UnsubscribeAll 移除所有配置变更监听器。
//
// 主要用于测试清理，防止监听器跨测试用例泄漏。
// 推荐在每个测试函数中注册清理：
//
//	t.Cleanup(config.UnsubscribeAll)
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

	return new(cfg), nil
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
	return new(*cfg), true
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
		Log: logger.Config{
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
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
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
