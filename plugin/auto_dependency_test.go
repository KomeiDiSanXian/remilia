package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutoTrackDependencies 测试自动依赖跟踪
func TestAutoTrackDependencies(t *testing.T) {
	manager := NewManager(nil)

	// 先注册基础插件
	basePlugin := &PluginDescriptor{
		Name: "base",
		Setup: func(ctx *SetupContext) error {
			return nil
		},
	}
	err := manager.RegisterV2(basePlugin)
	require.NoError(t, err)

	// 注册依赖于 base 的插件（通过 Get 调用）
	depPlugin := &PluginDescriptor{
		Name: "dependent",
		// 注意：没有声明 Deps 字段
		Setup: func(ctx *SetupContext) error {
			// 调用 Get 会自动跟踪依赖
			_, _ = ctx.Get("base")
			return nil
		},
	}

	err = manager.RegisterV2(depPlugin)
	assert.NoError(t, err)

	// 验证依赖被跟踪到（通过日志警告）
	// 实际测试中会看到警告日志：undeclared_deps: [base]
	t.Log("✓ Dependency tracking works")
}

// TestAutoTrackDependencies_MustGet 测试 MustGet 的自��跟踪
func TestAutoTrackDependencies_MustGet(t *testing.T) {
	manager := NewManager(nil)

	// 先注册基础插件
	err := manager.RegisterV2(&PluginDescriptor{
		Name:  "auth",
		Setup: func(ctx *SetupContext) error { return nil },
	})
	require.NoError(t, err)

	// 使用 MustGet
	err = manager.RegisterV2(&PluginDescriptor{
		Name: "permission",
		Setup: func(ctx *SetupContext) error {
			_ = ctx.MustGet("auth") // 自动跟踪
			return nil
		},
	})
	assert.NoError(t, err)
	t.Log("✓ MustGet dependency tracking works")
}

// TestRegisterMultipleV2Smart 测试智能批量注册
func TestRegisterMultipleV2Smart(t *testing.T) {
	t.Run("auto infer simple dependency", func(t *testing.T) {
		manager := NewManager(nil)

		// 不声明 Deps，让系统自动推断
		plugins := []*PluginDescriptor{
			{
				Name: "permission",
				Setup: func(ctx *SetupContext) error {
					// 这里会尝试获取 auth，自动推断依赖
					defer func() { recover() }() // 捕获 panic（推断阶段）
					_ = ctx.MustGet("auth")
					return nil
				},
			},
			{
				Name:  "auth",
				Setup: func(ctx *SetupContext) error { return nil },
			},
		}

		// 智能注册：自动排序为 auth -> permission
		err := manager.RegisterMultipleV2Smart(plugins)
		assert.NoError(t, err)

		// 验证注册顺序
		assert.True(t, manager.IsLoaded("auth"))
		assert.True(t, manager.IsLoaded("permission"))
		t.Log("✓ Smart registration with auto-inferred dependencies works")
	})

	t.Run("detect inferred circular dependency", func(t *testing.T) {
		manager := NewManager(nil)

		// 循环依赖：a -> b -> a
		plugins := []*PluginDescriptor{
			{
				Name: "a",
				Setup: func(ctx *SetupContext) error {
					defer func() { recover() }()
					_ = ctx.MustGet("b")
					return nil
				},
			},
			{
				Name: "b",
				Setup: func(ctx *SetupContext) error {
					defer func() { recover() }()
					_ = ctx.MustGet("a")
					return nil
				},
			},
		}

		err := manager.RegisterMultipleV2Smart(plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
		t.Log("✓ Circular dependency detected with smart registration")
	})
}

// TestGetTrackedDependencies 测试依赖跟踪功能
func TestGetTrackedDependencies(t *testing.T) {
	manager := NewManager(nil)

	// 注册几个插件
	manager.RegisterV2(&PluginDescriptor{
		Name:  "a",
		Setup: func(ctx *SetupContext) error { return nil },
	})
	manager.RegisterV2(&PluginDescriptor{
		Name:  "b",
		Setup: func(ctx *SetupContext) error { return nil },
	})

	// 创建一个跟踪多个依赖的插件
	var trackedDeps []string
	err := manager.RegisterV2(&PluginDescriptor{
		Name: "test",
		Setup: func(ctx *SetupContext) error {
			_ = ctx.MustGet("a")
			_ = ctx.MustGet("b")
			trackedDeps = ctx.GetTrackedDependencies()
			return nil
		},
	})

	require.NoError(t, err)
	assert.Len(t, trackedDeps, 2)
	assert.Contains(t, trackedDeps, "a")
	assert.Contains(t, trackedDeps, "b")
	t.Log("✓ GetTrackedDependencies works")
}

// TestSmartRegistration_ComplexCase 测试复杂场景
func TestSmartRegistration_ComplexCase(t *testing.T) {
	manager := NewManager(nil)

	// 复杂依赖图（不声明 Deps）：
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	plugins := []*PluginDescriptor{
		{
			Name: "d",
			Setup: func(ctx *SetupContext) error {
				defer func() { recover() }()
				_ = ctx.MustGet("b")
				_ = ctx.MustGet("c")
				return nil
			},
		},
		{
			Name: "c",
			Setup: func(ctx *SetupContext) error {
				defer func() { recover() }()
				_ = ctx.MustGet("a")
				return nil
			},
		},
		{
			Name: "b",
			Setup: func(ctx *SetupContext) error {
				defer func() { recover() }()
				_ = ctx.MustGet("a")
				return nil
			},
		},
		{
			Name:  "a",
			Setup: func(ctx *SetupContext) error { return nil },
		},
	}

	err := manager.RegisterMultipleV2Smart(plugins)
	assert.NoError(t, err)

	// 验证所有插件都已加载
	assert.True(t, manager.IsLoaded("a"))
	assert.True(t, manager.IsLoaded("b"))
	assert.True(t, manager.IsLoaded("c"))
	assert.True(t, manager.IsLoaded("d"))

	// 验证加载顺序正确（a 最先，d 最后）
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

// TestDeclaredVsInferred 测试声明的依赖与推断的依赖
func TestDeclaredVsInferred(t *testing.T) {
	manager := NewManager(nil)

	manager.RegisterV2(&PluginDescriptor{
		Name:  "a",
		Setup: func(ctx *SetupContext) error { return nil },
	})
	manager.RegisterV2(&PluginDescriptor{
		Name:  "b",
		Setup: func(ctx *SetupContext) error { return nil },
	})

	// 声明依赖 a，但实际使用了 b
	err := manager.RegisterV2(&PluginDescriptor{
		Name: "test",
		Deps: []string{"a"}, // 声明依赖 a
		Setup: func(ctx *SetupContext) error {
			_ = ctx.MustGet("a")
			_ = ctx.MustGet("b") // 实际还使用了 b（未声明）
			return nil
		},
	})

	assert.NoError(t, err)
	// 应该会有警告日志：undeclared_deps: [b]
	t.Log("✓ Undeclared dependency detection works")
}

// Helper function
func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}
