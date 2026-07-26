package ai

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// mockConfig implements plugin.Config for testing.
// Only needed methods are implemented; unsupported methods return zero values.
type mockConfig struct {
	values map[string]any
}

func (m *mockConfig) Get(key string) any {
	if m.values == nil {
		return nil
	}
	return m.values[key]
}

func (m *mockConfig) GetString(key string, defaultVal string) string {
	if v := m.Get(key); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func (m *mockConfig) GetInt(key string, defaultVal int) int {
	if v := m.Get(key); v != nil {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return defaultVal
}

func (m *mockConfig) GetBool(key string, defaultVal bool) bool {
	if v := m.Get(key); v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func (m *mockConfig) GetDuration(key string, defaultVal time.Duration) time.Duration {
	if v := m.Get(key); v != nil {
		switch d := v.(type) {
		case time.Duration:
			return d
		case string:
			if dur, err := time.ParseDuration(d); err == nil {
				return dur
			}
		}
	}
	return defaultVal
}

func (m *mockConfig) GetFloat64(key string, defaultVal float64) float64 {
	if v := m.Get(key); v != nil {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return defaultVal
}

func (m *mockConfig) GetStringSlice(key string, defaultVal []string) []string {
	return defaultVal
}

func (m *mockConfig) GetStringMap(key string, defaultVal map[string]any) map[string]any {
	return defaultVal
}

func (m *mockConfig) GetAll() map[string]any {
	return nil
}

// ConfigMutator methods - no-op for tests
func (m *mockConfig) Override(key string, value any) error { return nil }
func (m *mockConfig) Reload() error                         { return nil }
func (m *mockConfig) OnChange(handler func(key string, oldVal, newVal any)) {}

// Ensure mockConfig implements plugin.Config.
var _ plugin.Config = (*mockConfig)(nil)
