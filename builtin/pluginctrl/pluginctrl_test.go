package pluginctrl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

func TestPluginDescriptor(t *testing.T) {
	d := pluginctrl.New(pluginctrl.WithSuperUsers("admin1"))
	assert.Equal(t, "pluginctrl", d.Name)
	assert.Contains(t, d.OptionalDeps, "storage")
}

func TestIsEnabled_Default(t *testing.T) {
	p := newTestPlugin()
	// 未设置时默认开启
	assert.True(t, p.IsEnabled("group1", "weather"))
	assert.True(t, p.IsEnabled("group2", "news"))
}

func TestIsEnabled_GroupToggle(t *testing.T) {
	p := newTestPlugin()
	// 关闭群1的 weather 插件
	err := p.SetGroupEnabled("group1", "weather", false)
	require.NoError(t, err)
	assert.False(t, p.IsEnabled("group1", "weather"))
	// 其他群不受影响
	assert.True(t, p.IsEnabled("group2", "weather"))
}

func TestIsEnabled_GlobalOverride(t *testing.T) {
	p := newTestPlugin()
	// 群1 开启了 weather，但全局关闭
	err := p.SetGroupEnabled("group1", "weather", true)
	require.NoError(t, err)
	err = p.SetGlobalEnabled("weather", false)
	require.NoError(t, err)
	// 全局覆盖优先
	assert.False(t, p.IsEnabled("group1", "weather"))
	assert.False(t, p.IsEnabled("group2", "weather"))
}

func TestIsSuperUser(t *testing.T) {
	p := newTestPlugin()
	assert.True(t, p.IsSuperUser("admin1"))
	assert.False(t, p.IsSuperUser("normal-user"))
}

func TestGroupList(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetGroupEnabled("group1", "weather", false))
	require.NoError(t, p.SetGroupEnabled("group1", "news", true))

	list := p.GroupList("group1")
	assert.Len(t, list, 2)
}

func TestPersistence(t *testing.T) {
	db, err := storage.NewMemory()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&pluginctrl.GroupPluginState{}))

	p := newTestPluginWithStorage(db)
	require.NoError(t, p.SetGroupEnabled("g1", "chat", false))
	require.NoError(t, p.SetGlobalEnabled("nsfw", false))

	// 模拟重启：新建插件并从同一DB加载
	p2 := newTestPluginWithStorage(db)
	p2.LoadFromDB()
	assert.False(t, p2.IsEnabled("g1", "chat"))
	assert.False(t, p2.IsEnabled("g999", "nsfw")) // 全局关闭
}

// ----- helpers -----

func newTestPlugin() *pluginctrl.Plugin {
	return pluginctrl.NewPlugin(
		pluginctrl.WithSuperUsers("admin1"),
	)
}

func newTestPluginWithStorage(db storage.Client) *pluginctrl.Plugin {
	return pluginctrl.NewPluginWithStorage(
		db,
		pluginctrl.WithSuperUsers("admin1"),
	)
}
