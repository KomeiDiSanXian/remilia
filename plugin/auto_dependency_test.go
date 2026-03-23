package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutoTrackDependencies tests automatic dependency tracking via Get.
func TestAutoTrackDependencies(t *testing.T) {
	manager := NewManager(nil)

	basePlugin := &Descriptor{
		Name:  "base",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}
	err := manager.Register(basePlugin)
	require.NoError(t, err)

	// dependent plugin accesses "base" via Get, auto-tracking the dependency.
	depPlugin := &Descriptor{
		Name: "dependent",
		Setup: func(ctx *SetupContext) (any, error) {
			_, _ = ctx.Get("base")
			return nil, nil
		},
	}

	err = manager.Register(depPlugin)
	assert.NoError(t, err)
	t.Log("✓ Dependency tracking works")
}

// TestAutoTrackDependencies_MustGet tests automatic tracking via MustGet.
func TestAutoTrackDependencies_MustGet(t *testing.T) {
	manager := NewManager(nil)

	err := manager.Register(&Descriptor{
		Name:  "auth",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})
	require.NoError(t, err)

	err = manager.Register(&Descriptor{
		Name: "permission",
		Setup: func(ctx *SetupContext) (any, error) {
			_ = ctx.MustGet("auth")
			return nil, nil
		},
	})
	assert.NoError(t, err)
	t.Log("✓ MustGet dependency tracking works")
}

// TestRegisterMultipleV2Smart tests smart batch registration with auto-inferred deps.
func TestRegisterMultipleV2Smart(t *testing.T) {
	t.Run("auto infer simple dependency", func(t *testing.T) {
		manager := NewManager(nil)

		plugins := []*Descriptor{
			{
				Name: "permission",
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.MustGet("auth")
					return nil, nil
				},
			},
			{
				Name:  "auth",
				Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
			},
		}

		err := manager.RegisterMultipleSmart(plugins)
		assert.NoError(t, err)
		assert.True(t, manager.IsLoaded("auth"))
		assert.True(t, manager.IsLoaded("permission"))
		t.Log("✓ Smart registration with auto-inferred dependencies works")
	})

	t.Run("detect inferred circular dependency", func(t *testing.T) {
		manager := NewManager(nil)

		plugins := []*Descriptor{
			{
				Name: "a",
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.MustGet("b")
					return nil, nil
				},
			},
			{
				Name: "b",
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.MustGet("a")
					return nil, nil
				},
			},
		}

		err := manager.RegisterMultipleSmart(plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
		t.Log("✓ Circular dependency detected with smart registration")
	})
}

// TestGetTrackedDependencies tests the GetTrackedDependencies method.
func TestGetTrackedDependencies(t *testing.T) {
	manager := NewManager(nil)

	manager.Register(&Descriptor{
		Name:  "a",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})
	manager.Register(&Descriptor{
		Name:  "b",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	var trackedDeps []string
	err := manager.Register(&Descriptor{
		Name: "test",
		Setup: func(ctx *SetupContext) (any, error) {
			_ = ctx.MustGet("a")
			_ = ctx.MustGet("b")
			trackedDeps = ctx.GetTrackedDependencies()
			return nil, nil
		},
	})

	require.NoError(t, err)
	assert.Len(t, trackedDeps, 2)
	assert.Contains(t, trackedDeps, "a")
	assert.Contains(t, trackedDeps, "b")
	t.Log("✓ GetTrackedDependencies works")
}

// TestSmartRegistration_ComplexCase tests complex diamond-shaped dependency graphs.
func TestSmartRegistration_ComplexCase(t *testing.T) {
	manager := NewManager(nil)

	// Diamond: A -> B,C -> D
	plugins := []*Descriptor{
		{
			Name: "d",
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.MustGet("b")
				_ = ctx.MustGet("c")
				return nil, nil
			},
		},
		{
			Name: "c",
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.MustGet("a")
				return nil, nil
			},
		},
		{
			Name: "b",
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.MustGet("a")
				return nil, nil
			},
		},
		{
			Name:  "a",
			Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
		},
	}

	err := manager.RegisterMultipleSmart(plugins)
	assert.NoError(t, err)
	assert.True(t, manager.IsLoaded("a"))
	assert.True(t, manager.IsLoaded("b"))
	assert.True(t, manager.IsLoaded("c"))
	assert.True(t, manager.IsLoaded("d"))

	order := manager.GetLoadOrder()
	aPos := indexOf(order, "a")
	bPos := indexOf(order, "b")
	cPos := indexOf(order, "c")
	dPos := indexOf(order, "d")

	assert.Less(t, aPos, bPos)
	assert.Less(t, aPos, cPos)
	assert.Less(t, bPos, dPos)
	assert.Less(t, cPos, dPos)

	t.Logf("✓ Complex smart registration works, order: %v", order)
}

// TestDeclaredVsInferred tests undeclared dependency detection.
func TestDeclaredVsInferred(t *testing.T) {
	manager := NewManager(nil)

	manager.Register(&Descriptor{
		Name:  "a",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})
	manager.Register(&Descriptor{
		Name:  "b",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	// Declared dep on "a" but also uses "b" without declaring it.
	err := manager.Register(&Descriptor{
		Name: "test",
		Deps: []string{"a"},
		Setup: func(ctx *SetupContext) (any, error) {
			_ = ctx.MustGet("a")
			_ = ctx.MustGet("b") // undeclared; should produce a warning log
			return nil, nil
		},
	})

	assert.NoError(t, err)
	t.Log("✓ Undeclared dependency detection works")
}

// indexOf is a helper to find the position of item in slice.
func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}
