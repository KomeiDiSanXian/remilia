package plugin

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// Config 插件配置接口
type Config interface {
	Get(key string) any
	GetString(key string, defaultVal string) string
	GetInt(key string, defaultVal int) int
	GetBool(key string, defaultVal bool) bool
	GetDuration(key string, defaultVal time.Duration) time.Duration
	// GetFloat64 获取浮点数配置
	GetFloat64(key string, defaultVal float64) float64
	// GetStringSlice 获取字符串切片配置
	GetStringSlice(key string, defaultVal []string) []string
	// GetStringMap 获取字符串键 map 配置
	GetStringMap(key string, defaultVal map[string]any) map[string]any

	// Override 覆盖内存中的配置值（仅本次运行有效，重启后失效）。
	// 会立即触发通过 OnChange 注册的所有监听器。
	Override(key string, value any) error

	// Set 是 Override 的别名，已废弃，请使用 Override。
	//
	// Deprecated: 使用 Override 替代，语义更清晰（仅内存覆盖，不持久化）。
	Set(key string, value any) error

	// Reload 重载配置
	Reload() error

	// OnChange 监听配置变化
	OnChange(handler func(key string, oldVal, newVal any))

	// GetAll 返回一个包含所有配置项的 map
	GetAll() map[string]any
}

// pluginConfig 插件配置实现
type pluginConfig struct {
	pluginName string
	viper      *viper.Viper
	values     map[string]any
	overrides  map[string]any // Override 写入的值，热重载后仍然保留（叠加在 viper 值之上）
	handlers   []func(key string, oldVal, newVal any)
	mu         sync.RWMutex
}

// NewPluginConfig 创建插件配置
func NewPluginConfig(pluginName string, globalViper *viper.Viper) Config {
	pc := &pluginConfig{
		pluginName: pluginName,
		viper:      globalViper,
		values:     make(map[string]any),
		handlers:   make([]func(key string, oldVal, newVal any), 0),
	}

	// 从全局配置加载插件配置
	pc.loadFromGlobal()

	return pc
}

// loadFromGlobal 从全局配置加载插件配置
func (pc *pluginConfig) loadFromGlobal() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.viper == nil {
		return
	}

	// 读取 plugins.<pluginName> 下的所有配置
	prefix := fmt.Sprintf("plugins.%s", pc.pluginName)
	settings := pc.viper.Sub(prefix)
	if settings != nil {
		pc.values = settings.AllSettings()
	}

	// 重新叠加 override，确保热重载不会丢弃运行时覆盖的值
	for k, v := range pc.overrides {
		pc.values[k] = v
	}
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
		}
	}

	// 尝试解析数字（纳秒）
	switch v := val.(type) {
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

// GetStringSlice 获取字符串切片配置
func (pc *pluginConfig) GetStringSlice(key string, defaultVal []string) []string {
	val := pc.Get(key)
	if val == nil {
		return defaultVal
	}

	if slice, ok := val.([]string); ok {
		return slice
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

// Set 设置配置值（仅覆盖内存，不持久化）
//
// Deprecated: 使用 Override 替代。
func (pc *pluginConfig) Set(key string, value any) error {
	return pc.Override(key, value)
}

// Reload 重载配置
func (pc *pluginConfig) Reload() error {
	pc.loadFromGlobal()
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
