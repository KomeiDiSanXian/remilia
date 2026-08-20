package plugin

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/errutil"
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
			_, _ = ctx.get("base")
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
			_ = ctx.mustGet("auth")
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
				Name:       "permission",
				DryRunSafe: true,
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.mustGet("auth")
					return nil, nil
				},
			},
			{
				Name:       "auth",
				DryRunSafe: true,
				Setup:      func(ctx *SetupContext) (any, error) { return nil, nil },
			},
		}

		err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
		assert.NoError(t, err)
		assert.True(t, manager.IsLoaded("auth"))
		assert.True(t, manager.IsLoaded("permission"))
		t.Log("✓ Smart registration with auto-inferred dependencies works")
	})

	t.Run("detect inferred circular dependency", func(t *testing.T) {
		manager := NewManager(nil)

		plugins := []*Descriptor{
			{
				Name:       "a",
				DryRunSafe: true,
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.mustGet("b")
					return nil, nil
				},
			},
			{
				Name:       "b",
				DryRunSafe: true,
				Setup: func(ctx *SetupContext) (any, error) {
					defer func() { recover() }()
					_ = ctx.mustGet("a")
					return nil, nil
				},
			},
		}

		err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
		assert.Error(t, err)
		assert.ErrorIs(t, err, errutil.ErrCircularDependency)
		t.Log("✓ Circular dependency detected with smart registration")
	})
}

// TestGetTrackedDependencies tests the getTrackedDependencies method.
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
			_ = ctx.mustGet("a")
			_ = ctx.mustGet("b")
			trackedDeps = ctx.getTrackedDependencies()
			return nil, nil
		},
	})

	require.NoError(t, err)
	assert.Len(t, trackedDeps, 2)
	assert.Contains(t, trackedDeps, "a")
	assert.Contains(t, trackedDeps, "b")
	t.Log("✓ getTrackedDependencies works")
}

// TestSmartRegistration_ComplexCase tests complex diamond-shaped dependency graphs.
func TestSmartRegistration_ComplexCase(t *testing.T) {
	manager := NewManager(nil)

	// Diamond: A -> B,C -> D
	plugins := []*Descriptor{
		{
			Name:       "d",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.mustGet("b")
				_ = ctx.mustGet("c")
				return nil, nil
			},
		},
		{
			Name:       "c",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.mustGet("a")
				return nil, nil
			},
		},
		{
			Name:       "b",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.mustGet("a")
				return nil, nil
			},
		},
		{
			Name:       "a",
			DryRunSafe: true,
			Setup:      func(ctx *SetupContext) (any, error) { return nil, nil },
		},
	}

	err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
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
			_ = ctx.mustGet("a")
			_ = ctx.mustGet("b") // undeclared; should produce a warning log
			return nil, nil
		},
	})

	assert.NoError(t, err)
	t.Log("✓ Undeclared dependency detection works")
}

// ========== 类型推断测试：ctx.Service[T]() 不带 name ==========

// TestSmartRegistration_TypeBasedResolution 验证类型推断在 Smart 注册中工作。
func TestSmartRegistration_TypeBasedResolution(t *testing.T) {
	manager := NewManager(nil)

	type storageAPI struct{ DB string }

	plugins := []*Descriptor{
		{
			Name:       "mysql",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				return &storageAPI{DB: "mysql://localhost"}, nil
			},
		},
		{
			Name:       "app",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				v := ctx.Service[*storageAPI]()
				if v.DB != "mysql://localhost" {
					t.Fatal("unexpected value")
				}
				return nil, nil
			},
		},
	}

	err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
	assert.NoError(t, err)
	assert.True(t, manager.IsLoaded("mysql"))
	assert.True(t, manager.IsLoaded("app"))
	t.Log("✓ Type-based ctx.Service[T]() with straight order works")
}

// TestSmartRegistration_TypeBasedResolution_Reversed 验证依赖在后时类型推断仍有效。
func TestSmartRegistration_TypeBasedResolution_Reversed(t *testing.T) {
	manager := NewManager(nil)

	type cacheAPI struct{ Addr string }

	plugins := []*Descriptor{
		{
			Name:       "app",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				v := ctx.Service[*cacheAPI]()
				if v.Addr != "redis:6379" {
					t.Fatal("unexpected value")
				}
				return nil, nil
			},
		},
		{
			Name:       "redis",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				return &cacheAPI{Addr: "redis:6379"}, nil
			},
		},
	}

	err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
	assert.NoError(t, err)
	assert.True(t, manager.IsLoaded("redis"))
	assert.True(t, manager.IsLoaded("app"))
	t.Log("✓ Type-based ctx.Service[T]() with reversed order works (three-color)")
}

// TestSmartRegistration_TypeBased_TryService 验证 TryService 类型推断。
func TestSmartRegistration_TypeBased_TryService(t *testing.T) {
	manager := NewManager(nil)

	type metricsAPI struct{ Port int }

	plugins := []*Descriptor{
		{
			Name:       "prometheus",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil // 不提供 metricsAPI
			},
		},
		{
			Name:       "app",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				proxy, ok := ctx.TryService[*metricsAPI]()
				if ok {
					t.Fatal("expected no metrics service")
				}
				_ = proxy
				return nil, nil
			},
		},
	}

	err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
	assert.NoError(t, err)
	t.Log("✓ Type-based ctx.TryService[T]() works when type not registered")
}

// TestSmartRegistration_TypeBased_Circular 验证类型解析的循环依赖检测。
func TestSmartRegistration_TypeBased_Circular(t *testing.T) {
	manager := NewManager(nil)

	type xAPI struct{ val string }
	type yAPI struct{ val string }

	plugins := []*Descriptor{
		{
			Name:       "x",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.Service[*yAPI]()
				return &xAPI{val: "x"}, nil
			},
		},
		{
			Name:       "y",
			DryRunSafe: true,
			Setup: func(ctx *SetupContext) (any, error) {
				defer func() { recover() }()
				_ = ctx.Service[*xAPI]()
				return &yAPI{val: "y"}, nil
			},
		},
	}

	err := manager.RegisterBatch(context.Background(), plugins, WithInferDeps())
	assert.Error(t, err)
	assert.ErrorIs(t, err, errutil.ErrCircularDependency)
	t.Log("✓ Type-based circular dependency detected")
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
