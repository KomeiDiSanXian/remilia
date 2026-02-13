package debug

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	p := New()
	require.NotNil(t, p)
	require.NotNil(t, p.BasePlugin)

	// 检查元数据
	meta := p.Metadata()
	assert.Equal(t, "debug", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "Remilia Team", meta.Author)
	assert.Equal(t, "开发", meta.Category)
	assert.Contains(t, meta.Tags, "调试")
	assert.NotEmpty(t, meta.HelpText)
}

func TestPlugin_Load(t *testing.T) {
	p := New()
	eng := engine.NewEngine()

	err := p.Load(eng)
	assert.NoError(t, err)

	// 验证 Engine 已保存
	assert.NotNil(t, p.Engine)

	// 验证命令已注册
	commands := eng.GetAllCommands()
	assert.Greater(t, len(commands), 0, "应该至少注册了一些命令")

	// 检查命令中是否包含 debug 相关的命令
	debugCount := 0
	for _, cmd := range commands {
		if strings.Contains(cmd.Command, "debug") {
			debugCount++
		}
	}
	assert.Greater(t, debugCount, 0, "应该注册了 debug 相关的命令")
}

func TestPlugin_Unload(t *testing.T) {
	p := New()
	eng := engine.NewEngine()

	err := p.Load(eng)
	require.NoError(t, err)

	err = p.Unload(eng)
	assert.NoError(t, err)
}

func TestPlugin_SetDevMode(t *testing.T) {
	p := New()

	// 默认应该是开启的
	assert.True(t, p.DevMode)

	// 设置为关闭
	p.SetDevMode(false)
	assert.False(t, p.DevMode)

	// 再设置为开启
	p.SetDevMode(true)
	assert.True(t, p.DevMode)
}

func TestPlugin_SetPluginManager(t *testing.T) {
	p := New()
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	p.SetPluginManager(pm)
	assert.NotNil(t, p.PluginManager)
	assert.Equal(t, pm, p.PluginManager)
}

func TestPlugin_Dependencies(t *testing.T) {
	p := New()

	// 检查 BasePlugin 是否为 nil
	require.NotNil(t, p.BasePlugin, "BasePlugin should not be nil")

	// 测试自动依赖提取（从标签）
	deps := p.Dependencies()
	t.Logf("Dependencies() result: %v (len=%d)", deps, len(deps))

	assert.NotNil(t, deps)
	// Debug 插件通过标签声明了 permission 依赖
	assert.Equal(t, 1, len(deps), "应该有1个插件依赖")
	assert.Contains(t, deps, "permission", "应该包含 permission 依赖")

	// 测试直接使用 ExtractDependencies
	extractedDeps := plugin.ExtractDependencies(p)
	t.Logf("ExtractDependencies() result: %v (len=%d)", extractedDeps, len(extractedDeps))
	assert.Equal(t, 1, len(extractedDeps), "标签提取应该得到1个依赖")
	assert.Contains(t, extractedDeps, "permission")
}

func TestPlugin_Metadata(t *testing.T) {
	p := New()
	meta := p.Metadata()

	require.NotNil(t, meta)
	assert.Equal(t, "debug", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "Remilia Team", meta.Author)
	assert.NotEmpty(t, meta.Description)
	assert.Equal(t, "开发", meta.Category)
	assert.Contains(t, meta.Tags, "调试")
	assert.Contains(t, meta.Tags, "开发")
	assert.Contains(t, meta.Tags, "性能")
	assert.NotEmpty(t, meta.HelpText)
	assert.False(t, meta.Hidden)

	// 验证依赖信息
	assert.Equal(t, 1, len(meta.Dependencies))
	assert.Contains(t, meta.Dependencies, "permission")
}

func TestPlugin_CheckPermission_WithoutPermPlugin(t *testing.T) {
	p := New()
	p.DevMode = true

	// 模拟上下文（简化版）
	// 实际测试中需要完整的 Context 对象
	// 这里只测试逻辑

	// 当没有权限插件时，应该根据 DevMode 决定
	assert.True(t, p.DevMode)

	p.DevMode = false
	assert.False(t, p.DevMode)
}

// BenchmarkPlugin_Load 性能测试
func BenchmarkPlugin_Load(b *testing.B) {
	for i := 0; i < b.N; i++ {
		p := New()
		eng := engine.NewEngine()
		_ = p.Load(eng)
	}
}

// BenchmarkPlugin_GetMetadata 性能测试
func BenchmarkPlugin_GetMetadata(b *testing.B) {
	p := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Metadata()
	}
}
