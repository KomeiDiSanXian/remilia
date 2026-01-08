package remilia

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewBasePlugin(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("test-plugin")

	assert.NotNil(t, plugin)
	assert.Equal(t, "test-plugin", plugin.Name())
	assert.Empty(t, plugin.matchers)
}

func TestBasePluginName(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("my-plugin")

	assert.Equal(t, "my-plugin", plugin.Name())
}

func TestBasePluginAddMatcher(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("test-plugin")

	matcher1 := &Matcher{}
	matcher2 := &Matcher{}

	plugin.AddMatcher(matcher1)
	assert.Len(t, plugin.matchers, 1)

	plugin.AddMatcher(matcher2)
	assert.Len(t, plugin.matchers, 2)
}

func TestBasePluginUnload(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("test-plugin")
	engine := NewEngine()

	engine.WithMatcherGroupBatch(func() {
		matcher1 := engine.OnC2C()
		matcher2 := engine.OnGroupAt()

		plugin.AddMatcher(matcher1)
		plugin.AddMatcher(matcher2)
	})

	assert.Len(t, plugin.matchers, 2)

	state := engine.state.Load().(*engineState)
	engineMatcherCount := len(state.matchers)
	assert.Equal(t, 2, engineMatcherCount)

	// Unload plugin
	err := plugin.Unload(engine)
	assert.NoError(t, err)

	assert.Empty(t, plugin.matchers)

	state = engine.state.Load().(*engineState)
	finalCount := len(state.matchers)
	assert.Equal(t, 0, finalCount)
}

// TestPlugin is a concrete implementation for testing
type TestPlugin struct {
	*BasePlugin
	loadCalled   bool
	unloadCalled bool
}

func NewTestPlugin(name string) *TestPlugin {
	return &TestPlugin{
		BasePlugin: NewBasePlugin(name),
	}
}

func (p *TestPlugin) Load(_ *Engine) error {
	p.loadCalled = true
	return nil
}

func (p *TestPlugin) Unload(engine *Engine) error {
	p.unloadCalled = true
	return p.BasePlugin.Unload(engine)
}

func (p *TestPlugin) Reload(engine *Engine) error {

	if err := p.Unload(engine); err != nil {
		return err
	}
	return p.Load(engine)
}

func TestNewPluginManager(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	assert.NotNil(t, pm)
	assert.Equal(t, engine, pm.engine)
	assert.NotNil(t, pm.plugins)
	assert.Empty(t, pm.plugins)
}

func TestPluginManagerRegister(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewTestPlugin("test-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	assert.Len(t, pm.plugins, 1)
	assert.True(t, plugin.loadCalled)

	// Verify plugin is stored
	stored, exists := pm.plugins["test-plugin"]
	assert.True(t, exists)
	assert.Equal(t, plugin, stored)
}

func TestPluginManagerRegister_Duplicate(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin1 := NewTestPlugin("test-plugin")
	plugin2 := NewTestPlugin("test-plugin")

	err := pm.Register(plugin1)
	assert.NoError(t, err)
	err = pm.Register(plugin2) // Should not replace existing
	assert.Error(t, err)

	assert.Len(t, pm.plugins, 1)

	// Verify first plugin is still stored
	stored, exists := pm.plugins["test-plugin"]
	assert.True(t, exists)
	assert.Equal(t, plugin1, stored)
}

func TestPluginManagerUnregister(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewTestPlugin("test-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	assert.Len(t, pm.plugins, 1)

	err = pm.Unregister("test-plugin")
	assert.NoError(t, err)

	assert.Empty(t, pm.plugins)
	assert.True(t, plugin.unloadCalled)
}

func TestPluginManagerUnregister_NotFound_Old(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	// Should not panic when unregistering non-existent plugin
	err := pm.Unregister("non-existent")
	assert.Error(t, err)
}

func TestPluginManagerGet(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewTestPlugin("test-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	// Get existing plugin
	retrieved, exists := pm.Get("test-plugin")
	assert.True(t, exists)
	assert.Equal(t, plugin, retrieved)

	// Get non-existent plugin
	retrieved2, exists2 := pm.Get("non-existent")
	assert.False(t, exists2)
	assert.Nil(t, retrieved2)
}

func TestPluginManagerList(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	// Empty list
	list := pm.List()
	assert.Empty(t, list)

	// Add plugins
	_ = pm.Register(NewTestPlugin("plugin1"))
	_ = pm.Register(NewTestPlugin("plugin2"))
	_ = pm.Register(NewTestPlugin("plugin3"))

	list = pm.List()
	assert.Len(t, list, 3)
	assert.Contains(t, list, "plugin1")
	assert.Contains(t, list, "plugin2")
	assert.Contains(t, list, "plugin3")
}

func TestPluginManagerMultipleOperations(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	// Register multiple plugins
	plugin1 := NewTestPlugin("plugin1")
	plugin2 := NewTestPlugin("plugin2")
	plugin3 := NewTestPlugin("plugin3")

	_ = pm.Register(plugin1)
	_ = pm.Register(plugin2)
	_ = pm.Register(plugin3)

	assert.Len(t, pm.List(), 3)

	// Unregister one
	_ = pm.Unregister("plugin2")
	assert.Len(t, pm.List(), 2)

	// Get remaining plugins
	_, exists := pm.Get("plugin1")
	assert.True(t, exists)

	_, exists = pm.Get("plugin2")
	assert.False(t, exists)

	_, exists = pm.Get("plugin3")
	assert.True(t, exists)
}

// Advanced test plugin with matchers
type AdvancedTestPlugin struct {
	*BasePlugin
}

func NewAdvancedTestPlugin(name string) *AdvancedTestPlugin {
	return &AdvancedTestPlugin{
		BasePlugin: NewBasePlugin(name),
	}
}

func (p *AdvancedTestPlugin) Load(engine *Engine) error {
	// Create some matchers
	matcher := engine.OnC2C().Handle(func(ctx *Context) {
		// Handler logic
	})
	p.AddMatcher(matcher)
	return nil
}

func TestAdvancedPluginWithMatchers(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewAdvancedTestPlugin("advanced-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	// Verify matcher was added to plugin
	assert.Len(t, plugin.matchers, 1)

	// Verify matcher was added to engine
	state := engine.state.Load().(*engineState)
	assert.Len(t, state.matchers, 1)

	// Unregister and verify cleanup
	err = pm.Unregister("advanced-plugin")
	assert.NoError(t, err)
	assert.Empty(t, plugin.matchers)
}

func TestPluginInterface(t *testing.T) {
	t.Parallel()
	// Verify TestPlugin implements Plugin interface
	var _ Plugin = (*TestPlugin)(nil)
	var _ Plugin = (*AdvancedTestPlugin)(nil)
	var _ Plugin = (*BasePlugin)(nil)
}

func TestPluginManagerReload_Success(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewTestPlugin("test-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)
	assert.True(t, plugin.loadCalled)

	// Reset flags
	plugin.loadCalled = false
	plugin.unloadCalled = false

	// Reload plugin
	err = pm.Reload("test-plugin")
	assert.NoError(t, err)
	assert.True(t, plugin.unloadCalled)
	assert.True(t, plugin.loadCalled)

	// Verify plugin still exists
	_, exists := pm.Get("test-plugin")
	assert.True(t, exists)
}

func TestPluginManagerReload_NotFound(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	err := pm.Reload("non-existent")
	assert.Error(t, err)
	assert.Equal(t, ErrPluginNotFound, err)
}

// Plugin that fails on load
type FailingLoadPlugin struct {
	*BasePlugin
	shouldFail bool
}

func NewFailingLoadPlugin(name string) *FailingLoadPlugin {
	return &FailingLoadPlugin{
		BasePlugin: NewBasePlugin(name),
		shouldFail: false,
	}
}

func (p *FailingLoadPlugin) Load(_ *Engine) error {
	if p.shouldFail {
		return assert.AnError
	}
	return nil
}

func (p *FailingLoadPlugin) Reload(engine *Engine) error {
	// 闈炲師瀛愭€ч噸杞斤細鍏?unload 鍐?load
	_ = p.Unload(engine)
	return p.Load(engine)
}

func TestPluginManagerRegister_LoadFails(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewFailingLoadPlugin("failing-plugin")
	plugin.shouldFail = true

	err := pm.Register(plugin)
	assert.Error(t, err)

	// Plugin should not be registered
	_, exists := pm.Get("failing-plugin")
	assert.False(t, exists)
}

func TestPluginManagerReload_LoadFails(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewFailingLoadPlugin("failing-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	// Plugin registered successfully
	_, exists := pm.Get("failing-plugin")
	assert.True(t, exists)

	// Now make it fail on reload
	plugin.shouldFail = true
	err = pm.Reload("failing-plugin")
	assert.Error(t, err)

	// Plugin should remain registered after failed reload
	// (PluginManager doesn't automatically remove failed plugins)
	_, exists = pm.Get("failing-plugin")
	assert.True(t, exists)
}

// Plugin that fails on unload
type FailingUnloadPlugin struct {
	*BasePlugin
	shouldFail bool
}

func NewFailingUnloadPlugin(name string) *FailingUnloadPlugin {
	return &FailingUnloadPlugin{
		BasePlugin: NewBasePlugin(name),
		shouldFail: false,
	}
}

func (p *FailingUnloadPlugin) Unload(engine *Engine) error {
	if p.shouldFail {
		return assert.AnError
	}
	return p.BasePlugin.Unload(engine)
}

func (p *FailingUnloadPlugin) Reload(engine *Engine) error {
	// 闈炲師瀛愭€ч噸杞斤細鍏?unload 鍐?load
	if err := p.Unload(engine); err != nil {
		return err
	}
	return p.Load(engine)
}

func TestPluginManagerUnregister_UnloadFails(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewFailingUnloadPlugin("failing-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	// Make unload fail
	plugin.shouldFail = true
	err = pm.Unregister("failing-plugin")
	assert.Error(t, err)
}

func TestPluginManagerReload_UnloadFails(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin := NewFailingUnloadPlugin("failing-plugin")
	err := pm.Register(plugin)
	assert.NoError(t, err)

	// Make unload fail
	plugin.shouldFail = true
	err = pm.Reload("failing-plugin")
	assert.Error(t, err)

	// Plugin should still exist since unload failed
	_, exists := pm.Get("failing-plugin")
	assert.True(t, exists)
}

func TestPluginManagerRegister_AlreadyExists(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	plugin1 := NewTestPlugin("test-plugin")
	err := pm.Register(plugin1)
	assert.NoError(t, err)

	plugin2 := NewTestPlugin("test-plugin")
	err = pm.Register(plugin2)
	assert.Error(t, err)
	assert.Equal(t, ErrPluginAlreadyExists, err)
}

func TestPluginManagerUnregister_NotFound(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	err := pm.Unregister("non-existent")
	assert.Error(t, err)
	assert.Equal(t, ErrPluginNotFound, err)
}

func TestBasePluginGetMatchers(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("test-plugin")

	matcher1 := &Matcher{}
	matcher2 := &Matcher{}

	plugin.AddMatcher(matcher1)
	plugin.AddMatcher(matcher2)

	matchers := plugin.GetMatchers()
	assert.Len(t, matchers, 2)

	// Verify it returns a copy
	matchers[0] = nil
	assert.NotNil(t, plugin.matchers[0])
}

func TestBasePluginConcurrency(t *testing.T) {
	t.Parallel()
	plugin := NewBasePlugin("test-plugin")

	// Test concurrent AddMatcher and GetMatchers
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			plugin.AddMatcher(&Matcher{})
			_ = plugin.GetMatchers()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Len(t, plugin.matchers, 10)
}

type ReloadableTestPlugin struct {
	*BasePlugin
	loadCount   int
	unloadCount int
	reloadErr   error
}

func NewReloadableTestPlugin(name string) *ReloadableTestPlugin {
	return &ReloadableTestPlugin{BasePlugin: NewBasePlugin(name)}
}

func (p *ReloadableTestPlugin) Load(engine *Engine) error {
	p.loadCount++
	return nil
}

func (p *ReloadableTestPlugin) Unload(engine *Engine) error {
	p.unloadCount++
	return nil
}

func (p *ReloadableTestPlugin) Reload(engine *Engine) error {
	if p.reloadErr != nil {
		return p.reloadErr
	}
	_ = p.Unload(engine)
	return p.Load(engine)
}

func TestPluginManager_Reload_DelegatesToPluginReload(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	p := NewReloadableTestPlugin("test-plugin")

	err := pm.Register(p)
	assert.NoError(t, err)
	assert.Equal(t, 1, p.loadCount)
	assert.Equal(t, 0, p.unloadCount)

	err = pm.Reload("test-plugin")
	assert.NoError(t, err)

	assert.Equal(t, 2, p.loadCount)
	assert.Equal(t, 1, p.unloadCount)
}

func TestPluginManager_Reload_PluginNotFound(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	err := pm.Reload("non-existent")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPluginNotFound)
}

func TestPluginManager_Reload_FailingPlugin(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	pm := NewPluginManager(engine)

	p := NewReloadableTestPlugin("failing-plugin")
	p.reloadErr = fmt.Errorf("reload failed")

	err := pm.Register(p)
	assert.NoError(t, err)

	err = pm.Reload("failing-plugin")
	assert.Error(t, err)

	_, exists := pm.Get("failing-plugin")
	assert.True(t, exists, "plugin should remain registered after reload failure")
}
