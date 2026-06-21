// Package config 是框架的配置聚合与反序列化层。
//
// # 职责边界
//
// config 包的唯一职责是：
//  1. 从外部来源（YAML 文件、环境变量）读取原始配置
//  2. 将原始配置反序列化为统一的 [Config] 结构体
//  3. 通过全局 atomic.Value 提供并发安全的配置访问和热重载
//  4. 向监听者广播配置变更事件
//
// config 包**不负责**将配置应用到具体基础设施组件（如初始化 logger、启动 server）。
// 各 infra 包自行读取所需的配置节并完成初始化，config 包只提供数据。
//
// # 配置结构与 infra 包的对应关系
//
//	Config.Bot.QQ       → QQ 平台适配器（platform/qq）
//	Config.Bot.OneBot   → OneBot V11 适配器（platform/onebot）
//	Config.Bot.Discord  → Discord 适配器（platform/discord）
//	Config.Bot.Satori   → Satori 协议适配器（platform/satori）
//	Config.Bot.Milky    → Milky QQ 适配器（platform/milky）
//	Config.Bot.Telegram → Telegram 适配器（platform/telegram，预留）
//	Config.Bot.WeChat   → 微信适配器（platform/wechat，预留）
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
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/errutil"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
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
	Pprof   PprofConfig               `yaml:"pprof" mapstructure:"pprof"`
	API     APIConfig                 `yaml:"api" mapstructure:"api"`
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

// BotConfig 各平台适配器的配置容器。
//
// 每个字段对应一个已支持的平台的配置结构体，均使用指针 + omitempty，
// 未使用的平台不会出现在 YAML 中。
// 所有平台均未配置时，整体 BotConfig 留空（全零值），不会触发验证错误。
type BotConfig struct {
	QQ       *QQConfig       `yaml:"qq,omitempty" mapstructure:"qq,omitempty"`
	OneBot   *OneBotConfig   `yaml:"onebot,omitempty" mapstructure:"onebot,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty" mapstructure:"discord,omitempty"`
	Satori   *SatoriConfig   `yaml:"satori,omitempty" mapstructure:"satori,omitempty"`
	Milky    *MilkyConfig    `yaml:"milky,omitempty" mapstructure:"milky,omitempty"`
	Telegram *TelegramConfig `yaml:"telegram,omitempty" mapstructure:"telegram,omitempty"`
	WeChat   *WeChatConfig   `yaml:"wechat,omitempty" mapstructure:"wechat,omitempty"`
}

// QQConfig QQ 机器人平台凭证配置。
//
// 由 QQ 平台适配器（platform/qq）在初始化时读取。
// 非 QQ 平台无需填写此配置节。
type QQConfig struct {
	AppID  uint64 `yaml:"app_id" mapstructure:"app_id"`
	BotID  uint64 `yaml:"bot_id" mapstructure:"bot_id"`
	Token  string `yaml:"token" mapstructure:"token"`
	Secret string `yaml:"secret" mapstructure:"secret"`
}

// OneBotConfig OneBot V11 协议适配器配置。
type OneBotConfig struct {
	URL        string `yaml:"url,omitempty" mapstructure:"url,omitempty"`
	ListenAddr string `yaml:"listen_addr,omitempty" mapstructure:"listen_addr,omitempty"`
	Token      string `yaml:"token,omitempty" mapstructure:"token,omitempty"`
	Secret     string `yaml:"secret,omitempty" mapstructure:"secret,omitempty"`
}

// DiscordConfig Discord 平台适配器配置。
type DiscordConfig struct {
	Token string `yaml:"token" mapstructure:"token"`
}

// SatoriConfig Satori 协议适配器配置。
type SatoriConfig struct {
	ServerURL string `yaml:"server_url" mapstructure:"server_url"`
	Token     string `yaml:"token,omitempty" mapstructure:"token,omitempty"`
	Platform  string `yaml:"platform,omitempty" mapstructure:"platform,omitempty"`
	UserID    string `yaml:"user_id,omitempty" mapstructure:"user_id,omitempty"`
}

// MilkyConfig Milky QQ 协议适配器配置。
type MilkyConfig struct {
	BaseURL     string `yaml:"base_url" mapstructure:"base_url"`
	AccessToken string `yaml:"access_token,omitempty" mapstructure:"access_token,omitempty"`
}

// TelegramConfig Telegram 平台适配器配置（预留）。
type TelegramConfig struct{}

// WeChatConfig 微信平台适配器配置（预留）。
type WeChatConfig struct{}

// HasChanged 检测 BotConfig 中已启用的平台配置是否发生了变更。
// 用于决定是否需要重启 Bot（例如热重载时凭证变更）。
func (bc *BotConfig) HasChanged(other *BotConfig) bool {
	return !ptrEqual(bc.QQ, other.QQ) ||
		!ptrEqual(bc.OneBot, other.OneBot) ||
		!ptrEqual(bc.Discord, other.Discord) ||
		!ptrEqual(bc.Satori, other.Satori) ||
		!ptrEqual(bc.Milky, other.Milky)
}

// ptrEqual 比较两个同类型指针的值是否相等。
// 均为 nil 时返回 true；一个 nil 一个非 nil 时返回 false。
func ptrEqual[T comparable](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
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

	TempMatcherShardCount int `yaml:"temp_matcher_shard_count" mapstructure:"temp_matcher_shard_count"`
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

// APIConfig 管理 API 配置。
type APIConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
	Addr    string `yaml:"addr" mapstructure:"addr"`
	APIKey  string `yaml:"api_key" mapstructure:"api_key"`
}

// PprofConfig pprof 性能分析配置。
type PprofConfig struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled"`
	Addr            string `yaml:"addr" mapstructure:"addr"`
	AutoProfile     bool   `yaml:"auto_profile" mapstructure:"auto_profile"`
	ProfileInterval string `yaml:"profile_interval" mapstructure:"profile_interval"`
	ProfileDuration string `yaml:"profile_duration" mapstructure:"profile_duration"`
	OutputDir       string `yaml:"output_dir" mapstructure:"output_dir"`
	EnableMutex     bool   `yaml:"enable_mutex" mapstructure:"enable_mutex"`
	EnableBlock     bool   `yaml:"enable_block" mapstructure:"enable_block"`
}

// Manager 管理配置的加载、存储和变更通知。
//
// 支持创建独立实例以隔离多 Bot 场景下的配置，同时保持包级便捷函数
// 委托给默认全局实例，确保向后兼容。
type Manager struct {
	config    infraatomic.Value[*Config]
	listeners []listenerEntry
	mu        sync.RWMutex
	idCounter atomic.Int64
}

// NewConfigManager 创建独立的配置管理器实例。
//
// 适用于需要为不同 Bot 实例隔离配置的场景：
//
//	prodMgr := config.NewConfigManager()
//	prodCfg, _ := prodMgr.Load("prod.yaml")
//
//	stagingMgr := config.NewConfigManager()
//	stagingCfg, _ := stagingMgr.Load("staging.yaml")
func NewConfigManager() *Manager {
	return &Manager{}
}

// defaultManager 是包级便捷函数委托的全局默认实例。
var defaultManager = NewConfigManager()

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
	mgr  *Manager
	once sync.Once
}

// Cancel 注销此监听器。多次调用是安全的（幂等）。
func (t *ListenerToken) Cancel() {
	t.once.Do(func() { t.mgr.unsubscribeByID(t.id) })
}

// ---------------------------------------------------------------------------
// ConfigManager 方法
// ---------------------------------------------------------------------------

// Load 从文件加载配置并存入管理器。
func (m *Manager) Load(path string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, errutil.Wrapf(err, "config file not found: %s", path)
	}

	cfg, err := loadRaw(path)
	if err != nil {
		return nil, err
	}

	cfgCopy := *cfg
	m.config.Store(&cfgCopy)
	m.notifyListeners(&cfgCopy)

	return &cfgCopy, nil
}

// Get 获取管理器中的当前配置（需要先调用 Load）。
func (m *Manager) Get() (*Config, bool) {
	cfg := m.config.Load()
	if cfg == nil {
		return nil, false
	}
	c := new(Config)
	*c = *cfg
	if cfg.Bot.QQ != nil {
		qq := *cfg.Bot.QQ
		c.Bot.QQ = &qq
	}
	if cfg.Bot.OneBot != nil {
		ob := *cfg.Bot.OneBot
		c.Bot.OneBot = &ob
	}
	if cfg.Bot.Discord != nil {
		d := *cfg.Bot.Discord
		c.Bot.Discord = &d
	}
	if cfg.Bot.Satori != nil {
		s := *cfg.Bot.Satori
		c.Bot.Satori = &s
	}
	if cfg.Bot.Milky != nil {
		milky := *cfg.Bot.Milky
		c.Bot.Milky = &milky
	}
	if cfg.Bot.Telegram != nil {
		t := *cfg.Bot.Telegram
		c.Bot.Telegram = &t
	}
	if cfg.Bot.WeChat != nil {
		w := *cfg.Bot.WeChat
		c.Bot.WeChat = &w
	}
	if cfg.Tracing.AdaptiveSamplerConfig != nil {
		asc := *cfg.Tracing.AdaptiveSamplerConfig
		c.Tracing.AdaptiveSamplerConfig = &asc
	}
	if cfg.Tracing.Headers != nil {
		c.Tracing.Headers = make(map[string]string, len(cfg.Tracing.Headers))
		maps.Copy(c.Tracing.Headers, cfg.Tracing.Headers)
	}
	if cfg.Plugins != nil {
		c.Plugins = make(map[string]map[string]any, len(cfg.Plugins))
		for k, v := range cfg.Plugins {
			inner := make(map[string]any, len(v))
			maps.Copy(inner, v)
			c.Plugins[k] = inner
		}
	}
	return c, true
}

// MustGet 获取管理器中的当前配置，未加载时 panic。
func (m *Manager) MustGet() *Config {
	cfg, ok := m.Get()
	if !ok {
		panic("config not loaded, please call Load() first")
	}
	return cfg
}

// LoadDefault 从默认位置加载配置。
func (m *Manager) LoadDefault() (*Config, error) {
	for _, path := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(path); err == nil {
			return m.Load(path)
		}
	}

	cfg := &Config{
		Bot: BotConfig{
			QQ: &QQConfig{
				AppID:  getEnvUint64("QQ_APP_ID"),
				BotID:  getEnvUint64("QQ_BOT_ID"),
				Token:  os.Getenv("QQ_TOKEN"),
				Secret: os.Getenv("QQ_SECRET"),
			},
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

	m.config.Store(cfg)
	return cfg, nil
}

// Subscribe 注册配置变更监听器，返回可用于精确取消的 token。
func (m *Manager) Subscribe(listener ChangeListener) *ListenerToken {
	id := m.idCounter.Add(1)
	m.mu.Lock()
	m.listeners = append(m.listeners, listenerEntry{id: id, fn: listener})
	m.mu.Unlock()
	return &ListenerToken{id: id, mgr: m}
}

// UnsubscribeAll 移除所有配置变更监听器（主要用于测试清理）。
func (m *Manager) UnsubscribeAll() {
	m.mu.Lock()
	m.listeners = nil
	m.mu.Unlock()
}

// unsubscribeByID 按 ID 移除单个监听器（内部使用）
func (m *Manager) unsubscribeByID(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.listeners {
		if e.id == id {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			return
		}
	}
}

// notifyListeners 通知所有已注册的监听器配置已变更
func (m *Manager) notifyListeners(cfg *Config) {
	m.mu.RLock()
	snapshot := make([]listenerEntry, len(m.listeners))
	copy(snapshot, m.listeners)
	m.mu.RUnlock()

	for _, entry := range snapshot {
		func(fn ChangeListener) {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			fn(cfg)
		}(entry.fn)
	}
}

// ---------------------------------------------------------------------------
// 包级便捷函数 — 委托给 defaultManager，保持向后兼容
// ---------------------------------------------------------------------------

// Load 从文件加载配置（使用默认管理器）。
func Load(path string) (*Config, error) { return defaultManager.Load(path) }

// Get 获取全局配置（使用默认管理器）。
func Get() (*Config, bool) { return defaultManager.Get() }

// MustGet 获取全局配置，未加载则 panic（使用默认管理器）。
func MustGet() *Config { return defaultManager.MustGet() }

// LoadDefault 从默认位置加载配置（使用默认管理器）。
func LoadDefault() (*Config, error) { return defaultManager.LoadDefault() }

// Subscribe 注册配置变更监听器（使用默认管理器）。
func Subscribe(listener ChangeListener) *ListenerToken {
	return defaultManager.Subscribe(listener)
}

// UnsubscribeAll 移除所有配置变更监听器（使用默认管理器）。
func UnsubscribeAll() {
	defaultManager.UnsubscribeAll()
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
