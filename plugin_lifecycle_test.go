package remilia

import (
	"errors"
	"sync"
	"testing"
)

// TestPluginLifecycleListener 是一个测试用的生命周期监听器
type TestPluginLifecycleListener struct {
	loaded   []string
	unloaded []string
	reloaded []string
	errors   map[string]error
	mu       sync.Mutex
}

func NewTestPluginLifecycleListener() *TestPluginLifecycleListener {
	return &TestPluginLifecycleListener{
		loaded:   make([]string, 0),
		unloaded: make([]string, 0),
		reloaded: make([]string, 0),
		errors:   make(map[string]error),
	}
}

func (l *TestPluginLifecycleListener) OnPluginLoaded(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loaded = append(l.loaded, name)
}

func (l *TestPluginLifecycleListener) OnPluginUnloaded(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unloaded = append(l.unloaded, name)
}

func (l *TestPluginLifecycleListener) OnPluginReloaded(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reloaded = append(l.reloaded, name)
}

func (l *TestPluginLifecycleListener) OnPluginError(name string, operation string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors[name+":"+operation] = err
}

func (l *TestPluginLifecycleListener) GetLoaded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.loaded))
	copy(result, l.loaded)
	return result
}

func (l *TestPluginLifecycleListener) GetUnloaded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.unloaded))
	copy(result, l.unloaded)
	return result
}

func (l *TestPluginLifecycleListener) GetReloaded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.reloaded))
	copy(result, l.reloaded)
	return result
}

func (l *TestPluginLifecycleListener) GetErrors() map[string]error {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make(map[string]error)
	for k, v := range l.errors {
		result[k] = v
	}
	return result
}

// simpleTestPlugin 是一个简单的测试插件
type simpleTestPlugin struct {
	*BasePlugin
}

func newSimpleTestPlugin(name string) *simpleTestPlugin {
	return &simpleTestPlugin{
		BasePlugin: NewBasePlugin(name),
	}
}

func (p *simpleTestPlugin) Load(e *Engine) error {
	return nil
}

// TestPluginLifecycleHooks 测试插件生命周期钩子
func TestPluginLifecycleHooks(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)
	listener := NewTestPluginLifecycleListener()

	// 添加监听器
	pm.AddListener(listener)

	// 创建测试插件
	plugin := newSimpleTestPlugin("test-plugin")

	// 注册插件
	if err := pm.Register(plugin); err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	// 验证 loaded 事件
	loaded := listener.GetLoaded()
	if len(loaded) != 1 || loaded[0] != "test-plugin" {
		t.Errorf("Expected OnPluginLoaded to be called for test-plugin, got: %v", loaded)
	}

	// 重载插件
	if err := pm.Reload("test-plugin"); err != nil {
		t.Fatalf("Failed to reload plugin: %v", err)
	}

	// 验证 reloaded 事件
	reloaded := listener.GetReloaded()
	if len(reloaded) != 1 || reloaded[0] != "test-plugin" {
		t.Errorf("Expected OnPluginReloaded to be called for test-plugin, got: %v", reloaded)
	}

	// 卸载插件
	if err := pm.Unregister("test-plugin"); err != nil {
		t.Fatalf("Failed to unregister plugin: %v", err)
	}

	// 验证 unloaded 事件
	unloaded := listener.GetUnloaded()
	if len(unloaded) != 1 || unloaded[0] != "test-plugin" {
		t.Errorf("Expected OnPluginUnloaded to be called for test-plugin, got: %v", unloaded)
	}

	// 验证没有错误
	errors := listener.GetErrors()
	if len(errors) != 0 {
		t.Errorf("Expected no errors, got: %v", errors)
	}
}

// failingPlugin 是一个加载失败的插件
type failingPlugin struct {
	*BasePlugin
	loadErr error
}

func newFailingPlugin(name string, loadErr error) *failingPlugin {
	return &failingPlugin{
		BasePlugin: NewBasePlugin(name),
		loadErr:    loadErr,
	}
}

func (p *failingPlugin) Load(e *Engine) error {
	return p.loadErr
}

// TestPluginLifecycleErrorHooks 测试插件生命周期错误钩子
func TestPluginLifecycleErrorHooks(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)
	listener := NewTestPluginLifecycleListener()

	// 添加监听器
	pm.AddListener(listener)

	// 创建一个加载失败的插件
	loadErr := errors.New("load failed")
	plugin := newFailingPlugin("fail-plugin", loadErr)

	// 尝试注册插件（应该失败）
	err := pm.Register(plugin)
	if err == nil {
		t.Fatal("Expected plugin registration to fail")
	}

	// 验证没有 loaded 事件
	loaded := listener.GetLoaded()
	if len(loaded) != 0 {
		t.Errorf("Expected no OnPluginLoaded event for failed plugin, got: %v", loaded)
	}

	// 验证有错误事件
	errors := listener.GetErrors()
	if len(errors) != 1 {
		t.Fatalf("Expected 1 error event, got %d", len(errors))
	}
	if _, exists := errors["fail-plugin:load"]; !exists {
		t.Errorf("Expected error for fail-plugin:load, got: %v", errors)
	}
}

// TestPluginLifecycleMultipleListeners 测试多个监听器
func TestPluginLifecycleMultipleListeners(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)
	listener1 := NewTestPluginLifecycleListener()
	listener2 := NewTestPluginLifecycleListener()

	// 添加两个监听器
	pm.AddListener(listener1)
	pm.AddListener(listener2)

	// 创建测试插件
	plugin := newSimpleTestPlugin("multi-plugin")

	// 注册插件
	if err := pm.Register(plugin); err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	// 验证两个监听器都收到了事件
	loaded1 := listener1.GetLoaded()
	loaded2 := listener2.GetLoaded()

	if len(loaded1) != 1 || loaded1[0] != "multi-plugin" {
		t.Errorf("Expected listener1 to receive OnPluginLoaded, got: %v", loaded1)
	}
	if len(loaded2) != 1 || loaded2[0] != "multi-plugin" {
		t.Errorf("Expected listener2 to receive OnPluginLoaded, got: %v", loaded2)
	}

	// 移除一个监听器
	pm.RemoveListener(listener1)

	// 卸载插件
	if err := pm.Unregister("multi-plugin"); err != nil {
		t.Fatalf("Failed to unregister plugin: %v", err)
	}

	// 验证只有 listener2 收到卸载事件
	unloaded1 := listener1.GetUnloaded()
	unloaded2 := listener2.GetUnloaded()

	if len(unloaded1) != 0 {
		t.Errorf("Expected listener1 (removed) to not receive OnPluginUnloaded, got: %v", unloaded1)
	}
	if len(unloaded2) != 1 || unloaded2[0] != "multi-plugin" {
		t.Errorf("Expected listener2 to receive OnPluginUnloaded, got: %v", unloaded2)
	}
}

// reloadFailPlugin 是一个重载时会失败的插件
type reloadFailPlugin struct {
	*BasePlugin
	shouldFail bool
	mu         sync.Mutex
}

func newReloadFailPlugin(name string) *reloadFailPlugin {
	return &reloadFailPlugin{
		BasePlugin: NewBasePlugin(name),
		shouldFail: false,
	}
}

func (p *reloadFailPlugin) Load(e *Engine) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Debug logging
	if testing.Verbose() {
		println("reloadFailPlugin.Load called, shouldFail =", p.shouldFail)
	}
	if p.shouldFail {
		return errors.New("reload load failed")
	}
	return nil
}

func (p *reloadFailPlugin) Reload(engine *Engine) error {
	// Use the base plugin's Reload logic which will call our overridden Load/Unload
	return p.BasePlugin.Reload(engine)
}

func (p *reloadFailPlugin) SetShouldFail(fail bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shouldFail = fail
}

// unloadFailPlugin 是一个卸载时会失败的插件
type unloadFailPlugin struct {
	*BasePlugin
}

func newUnloadFailPlugin(name string) *unloadFailPlugin {
	return &unloadFailPlugin{
		BasePlugin: NewBasePlugin(name),
	}
}

func (p *unloadFailPlugin) Unload(e *Engine) error {
	return errors.New("unload failed")
}

// TestPluginLifecycleUnloadError 测试卸载失败的钩子
func TestPluginLifecycleUnloadError(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)
	listener := NewTestPluginLifecycleListener()

	pm.AddListener(listener)

	// 创建一个卸载会失败的插件
	plugin := newUnloadFailPlugin("unload-fail-plugin")

	// 首次注册成功
	if err := pm.Register(plugin); err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	// 尝试卸载插件（应该失败）
	err := pm.Unregister("unload-fail-plugin")
	if err == nil {
		t.Fatal("Expected plugin unload to fail")
	}

	// 验证有 loaded 事件（首次注册）
	loaded := listener.GetLoaded()
	if len(loaded) != 1 {
		t.Errorf("Expected 1 loaded event, got %d", len(loaded))
	}

	// 验证没有 unloaded 事件（因为失败了）
	unloaded := listener.GetUnloaded()
	if len(unloaded) != 0 {
		t.Errorf("Expected no unloaded event for failed unload, got: %v", unloaded)
	}

	// 验证有错误事件
	errors := listener.GetErrors()
	if len(errors) == 0 {
		t.Error("Expected error event for failed unload")
	}
	if _, exists := errors["unload-fail-plugin:unload"]; !exists {
		t.Errorf("Expected error for unload-fail-plugin:unload, got: %v", errors)
	}
}
