package plugin

// config_alias_test.go — 插件配置读取/写入边界的回归测试
//
// 回归背景（别名）：
//   - YAMLConfigProvider.Sub 直接返回 config.Config.Plugins 中的活 map，
//     pluginConfig.values 与 Override 的就地写入会反向改写全局配置（其它读者、
//     持久化都受影响），且该写入不受 pluginConfig 的锁保护——与其它读配置的
//     goroutine 并发即并发 map 写。
//   - GetStringSlice/GetStringMap 直接把内部切片与 map 交给调用方，GetAll 只做
//     浅拷贝，调用方改写返回值会反向污染插件配置。

import (
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDemoConfig() *config.Config {
	return &config.Config{
		Plugins: map[string]map[string]any{
			"demo": {
				"k":      "original",
				"nested": map[string]any{"n": 1},
				"list":   []any{map[string]any{"x": 1}},
			},
		},
	}
}

func TestYAMLConfigProvider_SubReturnsDeepCopy(t *testing.T) {
	cfg := newDemoConfig()
	provider := NewYAMLConfigProvider(cfg)

	settings := provider.Sub("demo")
	require.NotNil(t, settings)

	// 顶层写
	settings["k"] = "mutated"
	// 嵌套 map 写
	nested, ok := settings["nested"].(map[string]any)
	require.True(t, ok)
	nested["n"] = 999
	// 嵌套切片里的 map 写
	list, ok := settings["list"].([]any)
	require.True(t, ok)
	list[0].(map[string]any)["x"] = 42

	again := provider.Sub("demo")
	assert.Equal(t, "original", again["k"], "顶层改动不得回写全局配置")
	assert.Equal(t, 1, again["nested"].(map[string]any)["n"], "嵌套 map 改动不得回写")
	assert.Equal(t, 1, again["list"].([]any)[0].(map[string]any)["x"], "切片内 map 改动不得回写")
}

func TestPluginConfig_OverrideDoesNotMutateGlobalConfig(t *testing.T) {
	cfg := newDemoConfig()
	provider := NewYAMLConfigProvider(cfg)
	pc := NewPluginConfigFromProvider("demo", provider)

	require.NoError(t, pc.Override("k", "overridden"))

	assert.Equal(t, "overridden", pc.GetString("k", ""), "插件视角应看到覆盖值")
	assert.Equal(t, "original", provider.Sub("demo")["k"], "全局配置不得被插件覆盖改写")

	// 热重载后覆盖值仍生效，且不写回配置源
	require.NoError(t, pc.Reload())
	assert.Equal(t, "overridden", pc.GetString("k", ""))
	assert.Equal(t, "original", provider.Sub("demo")["k"])
}

func TestPluginConfig_OverrideIsRaceFreeWithGlobalReaders(t *testing.T) {
	cfg := newDemoConfig()
	provider := NewYAMLConfigProvider(cfg)
	pc := NewPluginConfigFromProvider("demo", provider)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 300 {
			_ = pc.Override("k", "v")
		}
	}()
	go func() {
		defer wg.Done()
		for range 300 {
			_ = provider.Sub("demo")
			_, _ = cfg.PluginString("demo", "k")
		}
	}()
	wg.Wait()

	assert.Equal(t, "original", provider.Sub("demo")["k"])
}

// TestPluginConfig_GetDoesNotLeakInternalReferences 读取接口不得把内部引用交给
// 调用方：旧实现的 GetStringSlice/GetStringMap 直接返回内部切片与 map，GetAll
// 只做浅拷贝——调用方改写返回值会反向污染插件配置（甚至影响到 Reload 的差量比较）。
func TestPluginConfig_GetDoesNotLeakInternalReferences(t *testing.T) {
	pc := &pluginConfig{
		values: map[string]any{
			"list":   []string{"a"},
			"mapped": map[string]any{"k": "v"},
			"nested": map[string]any{"inner": map[string]any{"k": "v"}},
		},
		handlers: []func(string, any, any){},
	}

	list := pc.GetStringSlice("list", nil)
	require.Len(t, list, 1)
	list[0] = "mutated"
	assert.Equal(t, []string{"a"}, pc.GetStringSlice("list", nil), "切片必须是副本")

	mapped := pc.GetStringMap("mapped", nil)
	mapped["k"] = "mutated"
	assert.Equal(t, "v", pc.GetStringMap("mapped", nil)["k"], "map 必须是副本")

	all := pc.GetAll()
	require.Contains(t, all, "nested")
	all["nested"].(map[string]any)["k"] = "mutated"
	all["nested"].(map[string]any)["inner"].(map[string]any)["k"] = "mutated"
	assert.Equal(t, "v", pc.GetStringMap("nested", nil)["inner"].(map[string]any)["k"],
		"GetAll 必须是深拷贝")
}
