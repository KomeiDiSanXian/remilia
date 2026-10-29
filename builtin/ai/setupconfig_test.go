package ai

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// setupConfig 是最小 ConfigReader 实现：只按 key 返回原始值。
//
// Setup 路径只需要 Get/GetString 等读取方法；这里刻意不缓存任何派生状态，
// 让"配置未提供时走默认值"的分支保持真实。
type setupConfig struct {
	values map[string]any
}

func (c *setupConfig) Get(key string) any { return c.values[key] }

func (c *setupConfig) GetString(key, def string) string {
	if v, ok := c.values[key].(string); ok {
		return v
	}
	return def
}

func (c *setupConfig) GetInt(key string, def int) int {
	switch v := c.values[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

func (c *setupConfig) GetBool(key string, def bool) bool {
	if v, ok := c.values[key].(bool); ok {
		return v
	}
	return def
}

func (c *setupConfig) GetDuration(key string, def time.Duration) time.Duration {
	switch v := c.values[key].(type) {
	case time.Duration:
		return v
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func (c *setupConfig) GetFloat64(key string, def float64) float64 {
	switch v := c.values[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return def
}

func (c *setupConfig) GetStringSlice(key string, def []string) []string { return def }
func (c *setupConfig) GetStringMap(key string, def map[string]any) map[string]any {
	return def
}
func (c *setupConfig) GetAll() map[string]any { return c.values }

// ConfigMutator：Setup 路径不写入/不重载配置，保持 no-op。
func (c *setupConfig) Override(key string, value any) error {
	c.values[key] = value
	return nil
}
func (c *setupConfig) Reload() error                                         { return nil }
func (c *setupConfig) OnChange(handler func(key string, oldVal, newVal any)) {}

// setupConfig 必须满足插件配置契约。
var _ plugin.Config = (*setupConfig)(nil)
