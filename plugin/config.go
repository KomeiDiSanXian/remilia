package plugin

import (
	"fmt"
	"maps"
	"reflect"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ConfigReader 是插件配置的只读视图。
//
// 大多数插件只需要此接口即可读取配置，
// 无需依赖完整的 Config 接口（含 Override、Reload、OnChange）。
// 可通过 `ctx.Config` 赋值给 `ConfigReader` 来收窄依赖。
type ConfigReader interface {
	// Get 获取指定 key 的原始值
	Get(key string) any

	// GetString 获取字符串配置，不存在时返回 defaultVal
	GetString(key string, defaultVal string) string

	// GetInt 获取整数配置，不存在时返回 defaultVal
	GetInt(key string, defaultVal int) int

	// GetBool 获取布尔配置，不存在时返回 defaultVal
	GetBool(key string, defaultVal bool) bool

	// GetDuration 获取时间段配置，不存在时返回 defaultVal
	GetDuration(key string, defaultVal time.Duration) time.Duration

	// GetFloat64 获取浮点数配置
	GetFloat64(key string, defaultVal float64) float64

	// GetStringSlice 获取字符串切片配置
	GetStringSlice(key string, defaultVal []string) []string

	// GetStringMap 获取字符串键 map 配置
	GetStringMap(key string, defaultVal map[string]any) map[string]any

	// GetAll 返回包含所有配置项的 map
	GetAll() map[string]any
}

// ConfigMutator 是插件配置的变更接口。
//
// 包含运行时覆盖写入（Override）、热重载（Reload）、变更监听（OnChange）。
// Manager 内部通过 ConfigurablePlugin 调用 Reload；高级插件可调用 Override 和 OnChange。
type ConfigMutator interface {
	// Override 覆盖内存中的配置值（仅本次运行有效，重启后失效）。
	// 会立即触发通过 OnChange 注册的所有监听器。
	Override(key string, value any) error

	// Reload 重载配置
	Reload() error

	// OnChange 监听配置变化
	OnChange(handler func(key string, oldVal, newVal any))
}

// Config 是插件配置的完整接口，组合 [ConfigReader] 和 [ConfigMutator]。
//
// 向后兼容：现有代码继续使用 Config，无需任何改动。
// 新代码可根据需要选择依赖 ConfigReader 或 ConfigMutator 作为更窄的接口。
type Config interface {
	ConfigReader
	ConfigMutator
}

// pluginConfig 插件配置实现
type pluginConfig struct {
	pluginName     string
	configProvider ConfigProvider // 推荐：通过 NewPluginConfigFromProvider 注入
	values         map[string]any
	overrides      map[string]any // Override 写入的值，热重载后仍然保留（叠加在 provider 值之上）
	handlers       []func(key string, oldVal, newVal any)
	mu             sync.RWMutex
}

// NewPluginConfigFromProvider 从 ConfigProvider 创建插件配置（推荐方式）。
//
// 相比 NewPluginConfig，不直接依赖 viper，可对接任意配置源。
func NewPluginConfigFromProvider(pluginName string, provider ConfigProvider) Config {
	pc := &pluginConfig{
		pluginName:     pluginName,
		configProvider: provider,
		values:         make(map[string]any),
		handlers:       make([]func(key string, oldVal, newVal any), 0),
	}
	pc.loadFromGlobal()
	return pc
}

// loadFromGlobal 从配置源加载插件配置
func (pc *pluginConfig) loadFromGlobal() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// ConfigProvider 路径（推荐方式）
	if pc.configProvider != nil {
		if settings := pc.configProvider.Sub(pc.pluginName); settings != nil {
			pc.values = settings
		} else {
			pc.values = make(map[string]any)
		}
		// 重新叠加 override，确保热重载后不会丢弃运行时覆盖的值
		maps.Copy(pc.values, pc.overrides)
		return
	}
	// 无 ConfigProvider：不清空 values，Override 值已在 values 中，Reload 是无操作
}

// Get 获取配置值
func (pc *pluginConfig) Get(key string) any {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.values[key]
}

// GetString 获取字符串配置
func (pc *pluginConfig) GetString(key string, defaultVal string) string {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	if str, ok := val.(string); ok {
		return str
	}

	return defaultVal
}

// GetInt 获取整数配置
func (pc *pluginConfig) GetInt(key string, defaultVal int) int {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return defaultVal
	}
}

// GetBool 获取布尔配置
func (pc *pluginConfig) GetBool(key string, defaultVal bool) bool {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	if b, ok := val.(bool); ok {
		return b
	}

	return defaultVal
}

// GetDuration 获取时间间隔配置
func (pc *pluginConfig) GetDuration(key string, defaultVal time.Duration) time.Duration {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	// 尝试解析字符串（如 "10s", "5m"）
	if str, ok := val.(string); ok {
		if duration, err := time.ParseDuration(str); err == nil {
			return duration
		} else {
			logger.WithError(err).Warnf("[Config] GetDuration(%q): failed to parse %q", key, str)
		}
	}

	// 尝试解析数字（纳秒）。
	// 注意补充 int：yaml.v3 把 YAML 整数解码为 int，缺了它数字时长配置会静默失效。
	switch v := val.(type) {
	case int:
		return time.Duration(v)
	case int64:
		return time.Duration(v)
	case float64:
		return time.Duration(v)
	default:
		return defaultVal
	}
}

// GetFloat64 获取浮点数配置
func (pc *pluginConfig) GetFloat64(key string, defaultVal float64) float64 {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	switch v := val.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	default:
		return defaultVal
	}
}

// GetStringSlice 获取字符串切片配置。
//
// 同时接受 []string 与 []any（yaml.v3 把 YAML 序列解码为 []any——
// 旧实现只认 []string，导致 YAML 配置的列表被静默忽略）。
// []any 中的非字符串元素按 fmt.Sprintf("%v") 转为字符串。
func (pc *pluginConfig) GetStringSlice(key string, defaultVal []string) []string {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	switch v := val.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprintf("%v", item))
			}
		}
		return out
	}

	return defaultVal
}

// GetStringMap 获取字符串键 map 配置
func (pc *pluginConfig) GetStringMap(key string, defaultVal map[string]any) map[string]any {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	if m, ok := val.(map[string]any); ok {
		return m
	}

	return defaultVal
}

// Override 在内存中覆盖配置值。
//
// 语义说明：
//   - 仅影响本插件的 Config.Get* 方法，不写入底层 viper（不影响磁盘配置文件）。
//   - 重启后失效（内存值不持久化）。
//   - 热重载后持续有效：框架在 Reload() 后会将所有 override 值重新叠加回配置，
//     确保运行时覆盖不会被配置文件静默覆盖。
//   - 会立即触发通过 OnChange 注册的所有监听器。
//
// 若需要持久化配置变更，请直接修改配置文件并调用 config.Reload()。
func (pc *pluginConfig) Override(key string, value any) error {
	pc.mu.Lock()
	oldVal := pc.values[key]
	pc.values[key] = value
	// 持久记录 override，确保热重载后仍然有效
	if pc.overrides == nil {
		pc.overrides = make(map[string]any)
	}
	pc.overrides[key] = value
	handlers := make([]func(key string, oldVal, newVal any), len(pc.handlers))
	copy(handlers, pc.handlers)
	pc.mu.Unlock()

	// 通知监听器（在锁外执行，避免死锁）
	for _, handler := range handlers {
		handler(key, oldVal, value)
	}

	return nil
}

// Reload 重载配置并通知 OnChange 监听器。
//
// 加载新配置后与旧值比较，对存在变化的 key 逐个调用 OnChange 注册的回调。
func (pc *pluginConfig) Reload() error {
	// 先取旧值，释放锁，再调用 loadFromGlobal（避免 loadFromGlobal 内部锁与 pc.mu 死锁）
	pc.mu.Lock()
	oldValues := make(map[string]any, len(pc.values))
	maps.Copy(oldValues, pc.values)
	handlers := make([]func(key string, oldVal, newVal any), len(pc.handlers))
	copy(handlers, pc.handlers)
	pc.mu.Unlock()

	pc.loadFromGlobal()

	pc.mu.RLock()
	newValues := make(map[string]any, len(pc.values))
	maps.Copy(newValues, pc.values)
	pc.mu.RUnlock()

	// 通知监听器（在锁外执行，避免死锁）
	for key, newVal := range newValues {
		oldVal, existed := oldValues[key]
		if !existed || !reflect.DeepEqual(oldVal, newVal) {
			for _, h := range handlers {
				h(key, oldVal, newVal)
			}
		}
	}
	for key, oldVal := range oldValues {
		if _, exists := newValues[key]; !exists {
			for _, h := range handlers {
				h(key, oldVal, nil)
			}
		}
	}

	return nil
}

// OnChange 监听配置变化
func (pc *pluginConfig) OnChange(handler func(key string, oldVal, newVal any)) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.handlers = append(pc.handlers, handler)
}

// GetAll 获取所有配置
func (pc *pluginConfig) GetAll() map[string]any {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	result := make(map[string]any, len(pc.values))
	maps.Copy(result, pc.values)
	return result
}
