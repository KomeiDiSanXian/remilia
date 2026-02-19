package plugin

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestP0Fix1_MatcherTracking tests that Matchers are properly tracked when using RegisterCommand
func TestP0Fix1_MatcherTracking(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	matcherCount := 0

	desc := &PluginDescriptor{
		Name:    "matcher-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) error {
			// Register some matchers using the new RegisterCommand method
			ctx.RegisterCommand(dto.C2CMessageCreate, "/cmd1")
			ctx.RegisterCommand(dto.GroupAtMessageCreate, "/cmd2")

			matcherCount = 2
			return nil
		},
	}

	err := manager.RegisterV2(desc)
	require.NoError(t, err)

	// Get the plugin instance
	plugin, exists := manager.Get("matcher-test")
	require.True(t, exists)

	// Cast to PluginInstance
	instance, ok := plugin.(*PluginInstance)
	require.True(t, ok)

	// Verify matchers are tracked
	matchers := instance.GetMatchers()
	assert.Equal(t, matcherCount, len(matchers), "Expected %d matchers to be tracked", matcherCount)

	// Verify matcher group and source are set correctly
	for _, m := range matchers {
		assert.Equal(t, "matcher-test", m.GetGroup())
		assert.Equal(t, "plugin:matcher-test", m.GetSource())
	}
}

// TestP0Fix2_StatefulPluginComplete tests the complete StatefulPlugin interface implementation
func TestP0Fix2_StatefulPluginComplete(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &PluginDescriptor{
		Name:    "stateful-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) error {
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
			return nil
		},
	}

	beforeLoad := time.Now()
	err := manager.RegisterV2(desc)
	require.NoError(t, err)

	// Get the plugin instance
	plugin, exists := manager.Get("stateful-test")
	require.True(t, exists)

	// Cast to StatefulPlugin
	stateful, ok := plugin.(StatefulPlugin)
	require.True(t, ok, "Plugin should implement StatefulPlugin")

	// Test GetState
	assert.Equal(t, Loaded, stateful.GetState())

	// Test GetLoadTime
	loadTime := stateful.GetLoadTime()
	assert.False(t, loadTime.IsZero(), "LoadTime should be set")
	assert.True(t, loadTime.After(beforeLoad) || loadTime.Equal(beforeLoad), "LoadTime should be after or equal to beforeLoad")

	// Test GetLastError (should be nil after successful load)
	assert.Nil(t, stateful.GetLastError())

	// Test GetUptime
	uptime := stateful.GetUptime()
	assert.Greater(t, uptime, time.Duration(0), "Uptime should be positive")

	// Test SetLoadTime
	newTime := time.Now().Add(-1 * time.Hour)
	stateful.SetLoadTime(newTime)
	assert.Equal(t, newTime, stateful.GetLoadTime())

	// Test SetLastError
	testErr := assert.AnError
	stateful.SetLastError(testErr)
	assert.Equal(t, testErr, stateful.GetLastError())

	// Test SetState
	stateful.SetState(Error)
	assert.Equal(t, Error, stateful.GetState())
}

// TestP0Fix3_ReloadRecreatesContext tests that Reload recreates SetupContext
func TestP0Fix3_ReloadRecreatesContext(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	setupCallCount := 0
	var firstContext, secondContext *SetupContext

	desc := &PluginDescriptor{
		Name:    "reload-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) error {
			setupCallCount++
			if setupCallCount == 1 {
				firstContext = ctx
			} else if setupCallCount == 2 {
				secondContext = ctx
			}
			return nil
		},
		Reload: func(ctx *SetupContext) error {
			setupCallCount++
			secondContext = ctx
			return nil
		},
	}

	// Initial load
	err := manager.RegisterV2(desc)
	require.NoError(t, err)
	assert.Equal(t, 1, setupCallCount)
	require.NotNil(t, firstContext)

	// Reload
	err = manager.Reload("reload-test")
	require.NoError(t, err)
	assert.Equal(t, 2, setupCallCount)
	require.NotNil(t, secondContext)

	// Verify that a new context was created
	assert.NotNil(t, firstContext)
	assert.NotNil(t, secondContext)
}

// TestP0Fix4_ConcurrentSafety tests concurrent registration
func TestP0Fix4_ConcurrentSafety(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	// Try to register multiple plugins concurrently
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			desc := &PluginDescriptor{
				Name:    "concurrent-test",
				Version: "1.0.0",
				Setup: func(ctx *SetupContext) error {
					time.Sleep(10 * time.Millisecond) // Simulate work
					return nil
				},
			}

			err := manager.RegisterV2(desc)
			// Only one should succeed, others should get "already registered" error
			if err != nil {
				assert.ErrorIs(t, err, errutil.ErrPluginAlreadyExists)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify only one plugin was registered
	plugin, exists := manager.Get("concurrent-test")
	assert.True(t, exists)
	assert.NotNil(t, plugin)

	// Count total plugins
	count := manager.Count()
	assert.Equal(t, 1, count, "Only one plugin should be registered")
}

// TestP0Fix_Integration tests all P0 fixes working together
func TestP0Fix_Integration(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	var capturedContext *SetupContext

	desc := &PluginDescriptor{
		Name:        "integration-test",
		Version:     "1.0.0",
		Description: "Test all P0 fixes",
		Setup: func(ctx *SetupContext) error {
			capturedContext = ctx

			// P0 Fix #1: Matcher tracking
			ctx.RegisterCommand(dto.C2CMessageCreate, "/test1")
			ctx.RegisterCommand(dto.GroupAtMessageCreate, "/test2")

			return nil
		},
		Reload: func(ctx *SetupContext) error {
			// P0 Fix #3: Context recreation
			capturedContext = ctx
			return nil
		},
	}

	beforeLoad := time.Now()

	// Register plugin
	err := manager.RegisterV2(desc)
	require.NoError(t, err)

	// Get plugin
	plugin, exists := manager.Get("integration-test")
	require.True(t, exists)

	instance, ok := plugin.(*PluginInstance)
	require.True(t, ok)

	// P0 Fix #1: Verify matchers are tracked
	matchers := instance.GetMatchers()
	assert.Equal(t, 2, len(matchers))

	// P0 Fix #2: Verify StatefulPlugin interface
	assert.Equal(t, Loaded, instance.GetState())
	assert.False(t, instance.GetLoadTime().IsZero())
	assert.True(t, instance.GetLoadTime().After(beforeLoad) || instance.GetLoadTime().Equal(beforeLoad))
	assert.Nil(t, instance.GetLastError())

	// Wait a bit to ensure uptime is measurable
	time.Sleep(5 * time.Millisecond)
	assert.Greater(t, instance.GetUptime(), time.Duration(0))

	// P0 Fix #3: Reload and verify context recreation
	oldContext := capturedContext
	err = manager.Reload("integration-test")
	require.NoError(t, err)
	newContext := capturedContext

	// Contexts should have the same manager and engine but be recreated
	assert.Equal(t, oldContext.Manager, newContext.Manager)
	assert.Equal(t, oldContext.Engine, newContext.Engine)

	// P0 Fix #4: Concurrent safety - try duplicate registration
	err = manager.RegisterV2(desc)
	assert.ErrorIs(t, err, errutil.ErrPluginAlreadyExists)
}
