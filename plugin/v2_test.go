package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContainer_BasicOperations tests basic container operations
func TestContainer_BasicOperations(t *testing.T) {
	container := NewContainer()

	// Test Register and Get
	container.Register("key1", "value1")
	val, ok := container.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	// Test Has
	assert.True(t, container.Has("key1"))
	assert.False(t, container.Has("key2"))

	// Test Remove
	container.Remove("key1")
	assert.False(t, container.Has("key1"))
}

// TestContainer_Concurrent tests concurrent safety
func TestContainer_Concurrent(t *testing.T) {
	container := NewContainer()
	done := make(chan bool)
	count := 100

	// Concurrent writes
	for i := range count {
		go func(n int) {
			container.Register(string(rune(n)), n)
			done <- true
		}(i)
	}

	// Wait for all writes
	for range count {
		<-done
	}

	// Verify all values
	for i := range count {
		_, ok := container.Get(string(rune(i)))
		assert.True(t, ok)
	}
}

// TestSetupContext_Get tests dependency retrieval
func TestSetupContext_Get(t *testing.T) {
	container := NewContainer()
	container.Register("dep1", "value1")

	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{container: container},
	}

	// Test Get
	val, ok := ctx.Get("dep1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	// Test Get non-existent
	val, ok = ctx.Get("dep2")
	assert.False(t, ok)
	assert.Nil(t, val)
}

// TestSetupContext_MustGet tests MustGet functionality
func TestSetupContext_MustGet(t *testing.T) {
	container := NewContainer()
	container.Register("dep1", "value1")

	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{container: container},
	}

	// Test MustGet success
	val := ctx.MustGet("dep1")
	assert.Equal(t, "value1", val)

	// Test MustGet panic
	assert.Panics(t, func() {
		ctx.MustGet("dep2")
	})
}

// TestPluginInstance_Lifecycle tests plugin lifecycle
func TestPluginInstance_Lifecycle(t *testing.T) {
	setupCalled := false
	teardownCalled := false

	desc := &Descriptor{
		Name: "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) {
			setupCalled = true
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			teardownCalled = true
			return nil
		},
	}

	eng := engine.NewEngine()
	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{container: NewContainer()},
	}

	instance := &Instance{
		desc:         desc,
		state:        Unloaded,
		setupContext: ctx,
		matchers:     make([]*engine.Matcher, 0),
	}

	// Test Load
	err := instance.load(context.Background())
	assert.NoError(t, err)
	assert.True(t, setupCalled)
	assert.Equal(t, Loaded, instance.GetState())

	// Test Unload
	err = instance.unload(context.Background(), eng)
	assert.NoError(t, err)
	assert.True(t, teardownCalled)
	assert.Equal(t, Unloaded, instance.GetState())
}

// TestPluginInstance_StatefulInterface tests StatefulPlugin interface
func TestPluginInstance_StatefulInterface(t *testing.T) {
	desc := &Descriptor{
		Name:  "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	instance := &Instance{
		desc:     desc,
		state:    Unloaded,
		matchers: make([]*engine.Matcher, 0),
	}

	// Test GetState / SetState
	assert.Equal(t, Unloaded, instance.GetState())
	instance.SetState(Loaded)
	assert.Equal(t, Loaded, instance.GetState())

	// Test LoadTime
	now := time.Now()
	instance.SetLoadTime(now)
	assert.Equal(t, now, instance.GetLoadTime())

	// Test LastError
	testErr := assert.AnError
	instance.SetLastError(testErr)
	assert.Equal(t, testErr, instance.GetLastError())

	// Test GetUptime (should be 0 for unloaded)
	instance.SetState(Unloaded)
	assert.Equal(t, time.Duration(0), instance.GetUptime())

	// Test GetUptime (should be > 0 for loaded)
	instance.SetState(Loaded)
	instance.SetLoadTime(time.Now().Add(-time.Second))
	uptime := instance.GetUptime()
	assert.True(t, uptime >= time.Second)
}

// TestPluginInstance_Metadata tests MetadataProvider interface
func TestPluginInstance_Metadata(t *testing.T) {
	desc := &Descriptor{
		Name:    "test-plugin",
		Version: "1.0.0",
		Deps:    []string{"dep1"},
		Meta: &Metadata{
			Author:      "Test Author",
			Description: "Test Description",
			Category:    "Test",
			Tags:        []string{"tag1", "tag2"},
			Hidden:      true,
		},
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	instance := &Instance{
		desc: desc,
	}

	metadata := instance.Metadata()
	assert.Equal(t, "test-plugin", metadata.Name)
	assert.Equal(t, "1.0.0", metadata.Version)
	assert.Equal(t, "Test Author", metadata.Author)
	assert.Equal(t, []string{"dep1"}, metadata.Dependencies)
	assert.True(t, metadata.Hidden)
}

// TestManager_RegisterV2_Basic tests basic v2 plugin registration
func TestManager_RegisterV2_Basic(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &Descriptor{
		Name:  "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	err := manager.Register(desc)
	assert.NoError(t, err)

	// Verify plugin is registered
	instance, exists := manager.Get("test-plugin")
	assert.True(t, exists)
	assert.Equal(t, "test-plugin", instance.Name())

	// Verify plugin state
	assert.Equal(t, Loaded, instance.GetState())
}

// TestManager_RegisterV2_WithDependencies tests plugin with dependencies
func TestManager_RegisterV2_WithDependencies(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	// Register dependency plugin
	dep := &Descriptor{
		Name:  "dep-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}
	err := manager.Register(dep)
	require.NoError(t, err)

	// Register main plugin that depends on dep-plugin
	main := &Descriptor{
		Name: "main-plugin",
		Deps: []string{"dep-plugin"},
		Setup: func(ctx *SetupContext) (any, error) {
			// Verify dependency is accessible
			depPlugin, ok := ctx.Get("dep-plugin")
			assert.True(t, ok)
			assert.NotNil(t, depPlugin)
			return nil, nil
		},
	}
	err = manager.Register(main)
	assert.NoError(t, err)
}

// TestManager_RegisterV2_MissingDependency tests missing dependency error
func TestManager_RegisterV2_MissingDependency(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &Descriptor{
		Name:  "test-plugin",
		Deps:  []string{"missing-dep"},
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	err := manager.Register(desc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

// TestManager_RegisterV2_DuplicatePlugin tests duplicate registration
func TestManager_RegisterV2_DuplicatePlugin(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &Descriptor{
		Name:  "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	// First registration should succeed
	err := manager.Register(desc)
	assert.NoError(t, err)

	// Second registration should fail
	err = manager.Register(desc)
	assert.Error(t, err)
}

// TestManager_RegisterV2_SetupError tests Setup error handling
func TestManager_RegisterV2_SetupError(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &Descriptor{
		Name: "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, assert.AnError
		},
	}

	err := manager.Register(desc)
	assert.Error(t, err)

	// Verify plugin was not registered
	_, exists := manager.Get("test-plugin")
	assert.False(t, exists)
}

// TestPluginInstance_Reload_Default tests default Reload behavior
func TestPluginInstance_Reload_Default(t *testing.T) {
	setupCount := 0
	teardownCount := 0

	desc := &Descriptor{
		Name: "test-plugin",
		Setup: func(ctx *SetupContext) (any, error) {
			setupCount++
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			teardownCount++
			return nil
		},
	}

	eng := engine.NewEngine()
	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{container: NewContainer()},
	}

	instance := &Instance{
		desc:         desc,
		state:        Unloaded,
		setupContext: ctx,
		matchers:     make([]*engine.Matcher, 0),
	}

	// Load once
	err := instance.load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, setupCount)

	// Reload should call Unload + Load
	err = instance.reload(context.Background(), eng)
	assert.NoError(t, err)
	assert.Equal(t, 2, setupCount)
	assert.Equal(t, 1, teardownCount)
	assert.Equal(t, Loaded, instance.GetState())
}

// TestGetPlugin_TypeSafe tests type-safe generic function
func TestGetPlugin_TypeSafe(t *testing.T) {
	type TestPlugin struct {
		Value string
	}

	container := NewContainer()
	testPlugin := &TestPlugin{Value: "test"}
	container.Register("test", testPlugin)

	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{container: container},
	}

	// Test successful type-safe get
	plugin, err := GetPlugin[TestPlugin](ctx, "test")
	assert.NoError(t, err)
	assert.Equal(t, "test", plugin.Value)

	// Test non-existent plugin
	_, err = GetPlugin[TestPlugin](ctx, "missing")
	assert.Error(t, err)
}

// BenchmarkContainer_Register benchmarks container registration
func BenchmarkContainer_Register(b *testing.B) {
	container := NewContainer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		container.Register("key", "value")
	}
}

// BenchmarkContainer_Get benchmarks container retrieval
func BenchmarkContainer_Get(b *testing.B) {
	container := NewContainer()
	container.Register("key", "value")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		container.Get("key")
	}
}
