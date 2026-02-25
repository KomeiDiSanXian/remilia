package plugintest_test

import (
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMockPluginInfo_IsLoaded(t *testing.T) {
	info := &plugintest.MockPluginInfo{
		LoadedPlugins: map[string]bool{"storage": true, "permission": true},
	}
	assert.True(t, info.IsLoaded("storage"))
	assert.True(t, info.IsLoaded("permission"))
	assert.False(t, info.IsLoaded("nonexistent"))
}
func TestMockPluginInfo_NilMaps_Safe(t *testing.T) {
	info := &plugintest.MockPluginInfo{}
	assert.False(t, info.IsLoaded("anything"))
	assert.False(t, info.IsDisabled("anything"))
	assert.Nil(t, info.GetStatus("anything"))
	assert.Nil(t, info.List())
	assert.Equal(t, 0, info.Count())
	meta, ok := info.GetMetadata("anything")
	assert.Nil(t, meta)
	assert.False(t, ok)
	assert.Nil(t, info.ListWithMetadata())
	assert.Nil(t, info.GetLoadOrder())
	inst, ok := info.Get("anything")
	assert.Nil(t, inst)
	assert.False(t, ok)
	assert.Nil(t, info.Coordinator())
}
func TestMockPluginInfo_GetMetadata(t *testing.T) {
	info := &plugintest.MockPluginInfo{
		Plugins: map[string]*plugin.Metadata{
			"storage": {Name: "storage", Version: "1.0.0"},
		},
	}
	meta, ok := info.GetMetadata("storage")
	require.True(t, ok)
	assert.Equal(t, "storage", meta.Name)
	_, ok = info.GetMetadata("missing")
	assert.False(t, ok)
}
func TestNewSetupContextWithInfo(t *testing.T) {
	info := &plugintest.MockPluginInfo{
		LoadedPlugins: map[string]bool{"storage": true},
		LoadOrder:     []string{"storage", "myplugin"},
	}
	ctx := plugintest.NewSetupContextWithInfo("myplugin", info, nil)
	defer plugintest.StopSetupContext(ctx)
	require.NotNil(t, ctx.Info)
	assert.True(t, ctx.Info.IsLoaded("storage"))
	assert.False(t, ctx.Info.IsLoaded("missing"))
	assert.Equal(t, []string{"storage", "myplugin"}, ctx.Info.GetLoadOrder())
}
func TestMockPluginInfo_Statuses(t *testing.T) {
	info := &plugintest.MockPluginInfo{
		Statuses: map[string]*plugin.Status{
			"storage": {Name: "storage", State: plugin.Loaded},
		},
	}
	s := info.GetStatus("storage")
	require.NotNil(t, s)
	assert.Equal(t, plugin.Loaded, s.State)
	assert.Nil(t, info.GetStatus("missing"))
}
