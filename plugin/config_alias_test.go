package plugin

// config_alias_test.go — 插件配置与全局配置不得互为别名的回归测试
//
// 回归背景：YAMLConfigProvider.Sub 直接返回 config.Config.Plugins 中的活 map，
// pluginConfig.values 与 Override 的就地写入会反向改写全局配置（其它读者、
// 持久化都受影响），且该写入不受 pluginConfig 的锁保护——与其它读配置的
// goroutine 并发即并发 map 写。

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
		for i := 0; i < 300; i++ {
			_ = pc.Override("k", "v")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = provider.Sub("demo")
			_, _ = cfg.PluginString("demo", "k")
		}
	}()
	wg.Wait()

	assert.Equal(t, "original", provider.Sub("demo")["k"])
}
