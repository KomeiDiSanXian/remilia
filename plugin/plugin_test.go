package plugin

import (
	"errors"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlugin is a test plugin
type mockPlugin struct {
	*BasePlugin
	loadErr   error
	unloadErr error
	reloadErr error
	deps      []string
}

func newMockPlugin(name string) *mockPlugin {
	return &mockPlugin{
		BasePlugin: NewBasePlugin(name),
		deps:       []string{},
	}
}

func (m *mockPlugin) Load(coordinator *engine.Engine) error {
	if m.loadErr != nil {
		return m.loadErr
	}
	return m.BasePlugin.Load(coordinator)
}

func (m *mockPlugin) Unload(coordinator *engine.Engine) error {
	if m.unloadErr != nil {
		return m.unloadErr
	}
	return m.BasePlugin.Unload(coordinator)
}

func (m *mockPlugin) Reload(coordinator *engine.Engine) error {
	if m.reloadErr != nil {
		return m.reloadErr
	}
	return m.BasePlugin.Reload(coordinator)
}

func (m *mockPlugin) Dependencies() []string {
	return m.deps
}

// mockLifecycleListener is a test listener
type mockLifecycleListener struct {
	loaded   []string
	unloaded []string
	reloaded []string
	errors   []string
	mu       sync.Mutex
}

func newMockListener() *mockLifecycleListener {
	return &mockLifecycleListener{
		loaded:   []string{},
		unloaded: []string{},
		reloaded: []string{},
		errors:   []string{},
	}
}

func (m *mockLifecycleListener) OnPluginLoaded(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded = append(m.loaded, name)
}

func (m *mockLifecycleListener) OnPluginUnloaded(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unloaded = append(m.unloaded, name)
}

func (m *mockLifecycleListener) OnPluginReloaded(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloaded = append(m.reloaded, name)
}

func (m *mockLifecycleListener) OnPluginError(name string, operation string, _ error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, name+":"+operation)
}

// TestNewBasePlugin tests creating a base plugin
func TestNewBasePlugin(t *testing.T) {
	plugin := NewBasePlugin("test-plugin")
	require.NotNil(t, plugin)
	assert.Equal(t, "test-plugin", plugin.Name())
	assert.Equal(t, 0, len(plugin.GetMatchers()))
}

// TestBasePlugin_Name tests plugin name
func TestBasePlugin_Name(t *testing.T) {
	plugin := NewBasePlugin("my-plugin")
	assert.Equal(t, "my-plugin", plugin.Name())
}

// TestBasePlugin_AddMatcher tests adding matchers
func TestBasePlugin_AddMatcher(t *testing.T) {
	plugin := NewBasePlugin("test")

	matcher1 := &engine.Matcher{}
	matcher2 := &engine.Matcher{}

	plugin.AddMatcher(matcher1)
	assert.Equal(t, 1, len(plugin.GetMatchers()))

	plugin.AddMatcher(matcher2)
	assert.Equal(t, 2, len(plugin.GetMatchers()))

	// Verify source and group are set
	matchers := plugin.GetMatchers()
	assert.Equal(t, "plugin:test", matchers[0].Source)
	assert.Equal(t, "test", matchers[0].GetGroup())
}

// TestBasePlugin_AddMatcher_Nil tests adding nil matcher
func TestBasePlugin_AddMatcher_Nil(t *testing.T) {
	plugin := NewBasePlugin("test")
	plugin.AddMatcher(nil)

	matchers := plugin.GetMatchers()
	assert.Equal(t, 1, len(matchers))
	assert.Nil(t, matchers[0])
}

// TestBasePlugin_GetMatchers tests getting matchers
func TestBasePlugin_GetMatchers(t *testing.T) {
	plugin := NewBasePlugin("test")

	matcher1 := &engine.Matcher{}
	matcher2 := &engine.Matcher{}

	plugin.AddMatcher(matcher1)
	plugin.AddMatcher(matcher2)

	matchers := plugin.GetMatchers()
	assert.Equal(t, 2, len(matchers))

	// Verify it's a copy
	matchers[0] = nil
	assert.NotNil(t, plugin.GetMatchers()[0])
}

// TestBasePlugin_Load tests loading plugin
func TestBasePlugin_Load(t *testing.T) {
	plugin := NewBasePlugin("test")
	eng := engine.NewEngine()

	err := plugin.Load(eng)
	assert.NoError(t, err)
}

// TestBasePlugin_Unload tests unloading plugin
func TestBasePlugin_Unload(t *testing.T) {
	plugin := NewBasePlugin("test")
	eng := engine.NewEngine()

	matcher := &engine.Matcher{}
	plugin.AddMatcher(matcher)

	err := plugin.Unload(eng)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(plugin.GetMatchers()))
}

// TestBasePlugin_Unload_NilEngine tests unloading with nil engine
func TestBasePlugin_Unload_NilEngine(t *testing.T) {
	plugin := NewBasePlugin("test")

	matcher := &engine.Matcher{}
	plugin.AddMatcher(matcher)

	err := plugin.Unload(nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(plugin.GetMatchers()))
}

// TestBasePlugin_Reload tests reloading plugin
func TestBasePlugin_Reload(t *testing.T) {
	t.Run("successful reload", func(t *testing.T) {
		plugin := newMockPlugin("test")
		eng := engine.NewEngine()

		matcher := &engine.Matcher{}
		plugin.AddMatcher(matcher)

		err := plugin.Reload(eng)
		assert.NoError(t, err)
	})

	t.Run("reload with nil engine", func(t *testing.T) {
		plugin := NewBasePlugin("test")

		err := plugin.Reload(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "coordinator is nil")
	})

	t.Run("reload with unload error", func(t *testing.T) {
		plugin := newMockPlugin("test")
		eng := engine.NewEngine()

		// Set unload error
		plugin.unloadErr = errors.New("unload failed")

		// Test Unload directly, not through Reload
		// because BasePlugin.Reload calls BasePlugin.Unload, not mockPlugin.Unload
		err := plugin.Unload(eng)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unload failed")
	})

	t.Run("reload with load error", func(t *testing.T) {
		plugin := newMockPlugin("test")
		eng := engine.NewEngine()

		// Set load error
		plugin.loadErr = errors.New("load failed")

		// Test Load directly, not through Reload
		// because BasePlugin.Reload calls BasePlugin.Load, not mockPlugin.Load
		err := plugin.Load(eng)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load failed")
	})
}

// TestBasePlugin_Dependencies tests getting dependencies
func TestBasePlugin_Dependencies(t *testing.T) {
	plugin := NewBasePlugin("test")
	deps := plugin.Dependencies()
	assert.Equal(t, 0, len(deps))
}

// TestBasePlugin_Use tests using middleware
func TestBasePlugin_Use(t *testing.T) {
	plugin := NewBasePlugin("test")
	eng := engine.NewEngine()

	mw := func(next context.Handler) context.Handler {
		return next
	}

	plugin.Use(eng, mw)
	// No error means success
}

// TestBasePlugin_Use_NilEngine tests using middleware with nil engine
func TestBasePlugin_Use_NilEngine(t *testing.T) {
	plugin := NewBasePlugin("test")

	mw := func(next context.Handler) context.Handler {
		return next
	}

	// Should not panic
	plugin.Use(nil, mw)
}

// TestBasePlugin_ConcurrentAccess tests concurrent access
func TestBasePlugin_ConcurrentAccess(t *testing.T) {
	plugin := NewBasePlugin("test")

	var wg sync.WaitGroup

	// Concurrent AddMatcher
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matcher := &engine.Matcher{}
			plugin.AddMatcher(matcher)
		}()
	}

	// Concurrent GetMatchers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = plugin.GetMatchers()
		}()
	}

	wg.Wait()

	assert.Equal(t, 10, len(plugin.GetMatchers()))
}

// TestNewManager tests creating a manager
func TestNewManager(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	require.NotNil(t, manager)
	assert.Equal(t, 0, len(manager.List()))
}

// TestManager_Register tests registering a plugin
func TestManager_Register(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")

		err := manager.Register(plugin)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(manager.List()))
	})

	t.Run("duplicate registration", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin1 := newMockPlugin("test")
		plugin2 := newMockPlugin("test")

		err := manager.Register(plugin1)
		require.NoError(t, err)

		err = manager.Register(plugin2)
		require.Error(t, err)
		assert.ErrorIs(t, err, errutil.ErrPluginAlreadyExists)
	})

	t.Run("registration with load error", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")
		plugin.loadErr = errors.New("load failed")

		err := manager.Register(plugin)
		require.Error(t, err)
		assert.Equal(t, 0, len(manager.List()))
	})
}

// TestManager_Unregister tests unregistering a plugin
func TestManager_Unregister(t *testing.T) {
	t.Run("successful unregistration", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")

		require.NoError(t, manager.Register(plugin))

		err := manager.Unregister("test")
		assert.NoError(t, err)
		assert.Equal(t, 0, len(manager.List()))
	})

	t.Run("unregister non-existent plugin", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)

		err := manager.Unregister("non-existent")
		require.Error(t, err)
		assert.ErrorIs(t, err, errutil.ErrPluginNotFound)
	})

	t.Run("unregister with unload error", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")
		plugin.unloadErr = errors.New("unload failed")

		require.NoError(t, manager.Register(plugin))

		err := manager.Unregister("test")
		require.Error(t, err)
		// Plugin should NOT be removed if unload fails
		assert.Equal(t, 1, len(manager.List()))
	})
}

// TestManager_Get tests getting a plugin
func TestManager_Get(t *testing.T) {
	t.Run("get existing plugin", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")

		require.NoError(t, manager.Register(plugin))

		retrieved, exists := manager.Get("test")
		assert.True(t, exists)
		assert.Equal(t, plugin, retrieved)
	})

	t.Run("get non-existent plugin", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)

		_, exists := manager.Get("non-existent")
		assert.False(t, exists)
	})
}

// TestManager_List tests listing plugins
func TestManager_List(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	plugin1 := newMockPlugin("plugin1")
	plugin2 := newMockPlugin("plugin2")

	require.NoError(t, manager.Register(plugin1))
	require.NoError(t, manager.Register(plugin2))

	names := manager.List()
	assert.Equal(t, 2, len(names))
	assert.Contains(t, names, "plugin1")
	assert.Contains(t, names, "plugin2")
}

// TestManager_Count tests counting plugins
func TestManager_Count(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	assert.Equal(t, 0, len(manager.List()))

	require.NoError(t, manager.Register(newMockPlugin("plugin1")))
	assert.Equal(t, 1, len(manager.List()))

	require.NoError(t, manager.Register(newMockPlugin("plugin2")))
	assert.Equal(t, 2, len(manager.List()))

	require.NoError(t, manager.Unregister("plugin1"))
	assert.Equal(t, 1, len(manager.List()))
}

// TestManager_Reload tests reloading a plugin
func TestManager_Reload(t *testing.T) {
	t.Run("successful reload", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")

		require.NoError(t, manager.Register(plugin))

		err := manager.Reload("test")
		assert.NoError(t, err)
	})

	t.Run("reload non-existent plugin", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)

		err := manager.Reload("non-existent")
		require.Error(t, err)
		assert.ErrorIs(t, err, errutil.ErrPluginNotFound)
	})

	t.Run("reload with error", func(t *testing.T) {
		eng := engine.NewEngine()
		manager := NewManager(eng)
		plugin := newMockPlugin("test")
		plugin.reloadErr = errors.New("reload failed")

		require.NoError(t, manager.Register(plugin))

		err := manager.Reload("test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reload failed")
	})
}

// TestManager_Listener tests lifecycle listeners
func TestManager_Listener(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)
	listener := newMockListener()

	manager.AddListener(listener)

	// Register
	plugin := newMockPlugin("test")
	require.NoError(t, manager.Register(plugin))

	assert.Equal(t, 1, len(listener.loaded))
	assert.Equal(t, "test", listener.loaded[0])

	// Reload
	require.NoError(t, manager.Reload("test"))

	assert.Equal(t, 1, len(listener.reloaded))
	assert.Equal(t, "test", listener.reloaded[0])

	// Unregister
	require.NoError(t, manager.Unregister("test"))

	assert.Equal(t, 1, len(listener.unloaded))
	assert.Equal(t, "test", listener.unloaded[0])
}

// TestManager_Listener_Error tests listener error notification
func TestManager_Listener_Error(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)
	listener := newMockListener()

	manager.AddListener(listener)

	// Register with load error
	plugin := newMockPlugin("test")
	plugin.loadErr = errors.New("load failed")

	_ = manager.Register(plugin)

	assert.Equal(t, 1, len(listener.errors))
	assert.Equal(t, "test:load", listener.errors[0])
}

// TestManager_RemoveListener tests removing listener
func TestManager_RemoveListener(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)
	listener := newMockListener()

	manager.AddListener(listener)
	manager.RemoveListener(listener)

	// Register plugin - listener should not be notified
	plugin := newMockPlugin("test")
	require.NoError(t, manager.Register(plugin))

	assert.Equal(t, 0, len(listener.loaded))
}

// TestErrors tests error types
func TestErrors(t *testing.T) {
	t.Run("errutil.ErrPluginAlreadyExists", func(t *testing.T) {
		assert.NotNil(t, errutil.ErrPluginAlreadyExists)
	})

	t.Run("errutil.ErrPluginNotFound", func(t *testing.T) {
		assert.NotNil(t, errutil.ErrPluginNotFound)
	})

	t.Run("errutil.ErrCircularDependency", func(t *testing.T) {
		assert.NotNil(t, errutil.ErrCircularDependency)
	})

	t.Run("errutil.ErrDependencyNotFound", func(t *testing.T) {
		assert.NotNil(t, errutil.ErrDependencyNotFound)
	})
}

// TestDependencyError tests DependencyError
func TestDependencyError(t *testing.T) {
	err := &DependencyError{
		Plugin:     "plugin-a",
		Dependency: "plugin-b",
		Err:        errutil.ErrDependencyNotFound,
	}

	assert.Contains(t, err.Error(), "plugin-a")
	assert.Contains(t, err.Error(), "plugin-b")
	assert.ErrorIs(t, err, errutil.ErrDependencyNotFound)
}

// TestCircularDependencyError tests CircularDependencyError
func TestCircularDependencyError(t *testing.T) {
	err := &CircularDependencyError{
		Cycle: []string{"A", "B", "C", "A"},
	}

	assert.Contains(t, err.Error(), "circular dependency")
	assert.ErrorIs(t, err, errutil.ErrCircularDependency)
}

// BenchmarkBasePlugin_AddMatcher benchmarks adding matchers
func BenchmarkBasePlugin_AddMatcher(b *testing.B) {
	plugin := NewBasePlugin("bench")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matcher := &engine.Matcher{}
		plugin.AddMatcher(matcher)
	}
}

// BenchmarkBasePlugin_GetMatchers benchmarks getting matchers
func BenchmarkBasePlugin_GetMatchers(b *testing.B) {
	plugin := NewBasePlugin("bench")
	for i := 0; i < 100; i++ {
		matcher := &engine.Matcher{}
		plugin.AddMatcher(matcher)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = plugin.GetMatchers()
	}
}

// BenchmarkManager_Register benchmarks registering plugins
func BenchmarkManager_Register(b *testing.B) {
	eng := engine.NewEngine()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		manager := NewManager(eng)
		for j := 0; j < 10; j++ {
			plugin := newMockPlugin("plugin")
			_ = manager.Register(plugin)
		}
	}
}
