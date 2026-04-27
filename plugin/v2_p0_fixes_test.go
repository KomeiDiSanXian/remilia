package plugin

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestP0Fix1_MatcherTracking tests that Matchers are properly tracked when using ctx.Reg.RegisterCommand
func TestP0Fix1_MatcherTracking(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	matcherCount := 0

	desc := &Descriptor{
		Name:    "matcher-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/cmd1")
			ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/cmd2")
			matcherCount = 2
			return nil, nil
		},
	}

	err := manager.Register(desc)
	require.NoError(t, err)

	instance, exists := manager.Get("matcher-test")
	require.True(t, exists)

	matchers := instance.GetMatchers()
	assert.Equal(t, matcherCount, len(matchers), "Expected %d matchers to be tracked", matcherCount)

	for _, m := range matchers {
		assert.Equal(t, "matcher-test", m.GetGroup())
		assert.Equal(t, "plugin:matcher-test", m.GetSource())
	}
}

// TestP0Fix2_StatefulPluginComplete tests the complete StatefulPlugin interface implementation
func TestP0Fix2_StatefulPluginComplete(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	desc := &Descriptor{
		Name:    "stateful-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			time.Sleep(10 * time.Millisecond)
			return nil, nil
		},
	}

	beforeLoad := time.Now()
	err := manager.Register(desc)
	require.NoError(t, err)

	inst, exists := manager.Get("stateful-test")
	require.True(t, exists)

	assert.Equal(t, Loaded, inst.GetState())

	loadTime := inst.GetLoadTime()
	assert.False(t, loadTime.IsZero(), "LoadTime should be set")
	assert.True(t, loadTime.After(beforeLoad) || loadTime.Equal(beforeLoad))

	assert.Nil(t, inst.GetLastError())

	uptime := inst.GetUptime()
	assert.Greater(t, uptime, time.Duration(0), "Uptime should be positive")

	newTime := time.Now().Add(-1 * time.Hour)
	inst.SetLoadTime(newTime)
	assert.Equal(t, newTime, inst.GetLoadTime())

	testErr := assert.AnError
	inst.SetLastError(testErr)
	assert.Equal(t, testErr, inst.GetLastError())

	inst.SetState(Error)
	assert.Equal(t, Error, inst.GetState())
}

// TestP0Fix3_ReloadRecreatesContext tests that Reload recreates SetupContext
func TestP0Fix3_ReloadRecreatesContext(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	setupCallCount := 0

	desc := &Descriptor{
		Name:    "reload-test",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			setupCallCount++
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace,
			Reload: func(ctx *SetupContext) error {
				setupCallCount++
				return nil
			},
		},
	}

	err := manager.Register(desc)
	require.NoError(t, err)
	assert.Equal(t, 1, setupCallCount)

	err = manager.Reload("reload-test")
	require.NoError(t, err)
	assert.Equal(t, 2, setupCallCount)
}

// TestP0Fix4_ConcurrentSafety tests concurrent registration
func TestP0Fix4_ConcurrentSafety(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	done := make(chan bool, 10)

	for i := range 10 {
		go func(id int) {
			desc := &Descriptor{
				Name:    "concurrent-test",
				Version: "1.0.0",
				Setup: func(ctx *SetupContext) (any, error) {
					time.Sleep(10 * time.Millisecond)
					return nil, nil
				},
			}

			err := manager.Register(desc)
			if err != nil {
				assert.ErrorIs(t, err, errutil.ErrPluginAlreadyExists)
			}
			done <- true
		}(i)
	}

	for range 10 {
		<-done
	}

	plugin, exists := manager.Get("concurrent-test")
	assert.True(t, exists)
	assert.NotNil(t, plugin)

	count := manager.Count()
	assert.Equal(t, 1, count, "Only one plugin should be registered")
}

// TestP0Fix_Integration tests all P0 fixes working together
func TestP0Fix_Integration(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	reloadCalled := false

	desc := &Descriptor{
		Name:    "integration-test",
		Version: "1.0.0",
		Meta: &Metadata{
			Description: "Test all P0 fixes",
		},
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/test1")
			ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/test2")
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace, // 必须显式声明，否则 Reload 不会被调用
			Reload: func(ctx *SetupContext) error {
				reloadCalled = true
				return nil
			},
		},
	}

	beforeLoad := time.Now()

	err := manager.Register(desc)
	require.NoError(t, err)

	instance, exists := manager.Get("integration-test")
	require.True(t, exists)

	// P0 Fix #1: Verify matchers are tracked
	matchers := instance.GetMatchers()
	assert.Equal(t, 2, len(matchers))

	// P0 Fix #2: Verify StatefulPlugin interface
	assert.Equal(t, Loaded, instance.GetState())
	assert.False(t, instance.GetLoadTime().IsZero())
	assert.True(t, instance.GetLoadTime().After(beforeLoad) || instance.GetLoadTime().Equal(beforeLoad))
	assert.Nil(t, instance.GetLastError())

	time.Sleep(5 * time.Millisecond)
	assert.Greater(t, instance.GetUptime(), time.Duration(0))

	// P0 Fix #3: Reload
	err = manager.Reload("integration-test")
	require.NoError(t, err)
	assert.True(t, reloadCalled, "Advanced.Reload should be called on Reload")

	// P0 Fix #4: Concurrent safety - try duplicate registration
	err = manager.Register(desc)
	assert.ErrorIs(t, err, errutil.ErrPluginAlreadyExists)
}
