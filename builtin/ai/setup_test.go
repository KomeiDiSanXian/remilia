package ai

import (
	"os"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAssemblesPlugin 守卫装配根：New 的 Setup 必须产出一个可用实例。
//
// 这条路径此前几乎没有覆盖（New 本身 1.4%）：它做的事最多——打开/降级持久化、
// 组装 registry 与 skill 注册表、注册内置动作、注册 handler、恢复中断任务。
// 这里用最小配置驱动一次真实 Setup，只断言"装配成功且关键件就位"，
// 不触碰网络（APIKey 为空、不启用 embedding/记忆/工作区）。
func TestNewAssemblesPlugin(t *testing.T) {
	cfg := &setupConfig{values: map[string]any{
		"provider":       "openai",
		"model":          "gpt-4o-mini",
		"trigger_cmd":    "/ai",
		"markdown":       true,
		"memory_enabled": false,
	}}

	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	desc := New(nil)
	api, err := desc.Setup(ctx)
	require.NoError(t, err, "Setup must succeed with a minimal config")

	p, ok := api.(*Plugin)
	require.True(t, ok, "Setup must return *Plugin")

	// 关键件就位。
	assert.NotNil(t, p.cfg)
	assert.NotNil(t, p.prov, "provider must be constructed")
	assert.NotNil(t, p.sm, "session manager must be constructed")
	assert.NotNil(t, p.reg, "tool registry must be constructed")
	assert.NotNil(t, p.skillReg)
	assert.NotNil(t, p.approvals)
	assert.NotNil(t, p.attachmentClient)
	assert.Equal(t, defaultDataDir, p.dataDir, "默认数据目录")

	// 内置动作必须注册：它们是模型可见面的基线。
	for _, name := range []string{
		"create_plan", "update_plan_step",
		"send_message", "send_to",
		"set_reminder", "list_reminders", "cancel_reminder",
		"todo_add", "todo_list", "todo_done", "todo_remove",
	} {
		if _, ok := p.reg.Action(name); !ok {
			t.Errorf("builtin action %q must be registered during Setup", name)
		}
	}

	// 未启用 memory_enabled 时记忆工具不应出现（能力未装配就不该给模型看见）。
	if _, ok := p.reg.Action("memory_add"); ok {
		t.Error("memory tools must not be registered when memory is disabled")
	}
}

// TestNewHonoursDataDirOption Setup 期间就要用上注入的数据目录。
func TestNewHonoursDataDirOption(t *testing.T) {
	cfg := &setupConfig{values: map[string]any{"provider": "openai", "model": "m"}}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	dir := t.TempDir()
	desc := New(nil, WithDataDir(dir))
	api, err := desc.Setup(ctx)
	require.NoError(t, err)

	p := api.(*Plugin)
	assert.Equal(t, dir, p.dataDir)

	// 数据目录下确实建了插件自己的子目录（群策略用 LevelDB）。
	// 必须先关掉句柄再让 TempDir 清理，否则 Windows 上会因文件占用失败。
	require.NoError(t, desc.Teardown(&plugin.TeardownContext{API: api}))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "Setup must create plugin data under the injected dir")
}

// TestNewRejectsUnknownProvider 未知提供商在装配期失败，而不是运行时才暴露。
func TestNewRejectsUnknownProvider(t *testing.T) {
	cfg := &setupConfig{values: map[string]any{"provider": "no-such-provider"}}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	// provider 非法会被 config 层拦下并回落到默认 openai；因此这里直接构造
	// 一个非法的 config 值验证 newProvider 的兜底分支。
	if _, err := newProvider(&config.Config{Provider: "no-such-provider"}); err == nil {
		t.Fatal("newProvider must reject unknown providers")
	}
	// 默认 provider 必须能构造。
	prov, err := newProvider(&config.Config{Provider: config.ProviderOpenAI, Model: "m"})
	require.NoError(t, err)
	assert.NotNil(t, prov)
	_ = toolkit.CategoryGeneral
}
