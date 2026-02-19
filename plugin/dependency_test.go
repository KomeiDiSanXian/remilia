package plugin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTopologicalSort_NoDependencies 测试无依赖的情况
func TestTopologicalSort_NoDependencies(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-b", Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-c", Setup: func(ctx *SetupContext) error { return nil }},
	}

	sorted, err := manager.topologicalSortV2(plugins)
	require.NoError(t, err)
	assert.Equal(t, 3, len(sorted))

	// 无依赖时，顺序可以是任意的，只要所有插件都在
	names := make(map[string]bool)
	for _, p := range sorted {
		names[p.Name] = true
	}
	assert.True(t, names["plugin-a"])
	assert.True(t, names["plugin-b"])
	assert.True(t, names["plugin-c"])
}

// TestTopologicalSort_SimpleDependency 测试简单依赖链
func TestTopologicalSort_SimpleDependency(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{Name: "plugin-c", Deps: []string{"plugin-b"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-a", Setup: func(ctx *SetupContext) error { return nil }}, // 无依赖
		{Name: "plugin-b", Deps: []string{"plugin-a"}, Setup: func(ctx *SetupContext) error { return nil }},
	}

	sorted, err := manager.topologicalSortV2(plugins)
	require.NoError(t, err)
	assert.Equal(t, 3, len(sorted))

	// 验证顺序：a -> b -> c
	assert.Equal(t, "plugin-a", sorted[0].Name)
	assert.Equal(t, "plugin-b", sorted[1].Name)
	assert.Equal(t, "plugin-c", sorted[2].Name)
}

// TestTopologicalSort_CircularDependency_Direct 测试直接循环依赖
func TestTopologicalSort_CircularDependency_Direct(t *testing.T) {
	manager := NewManager(nil)

	// A -> B -> C -> A (直接循环)
	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Deps: []string{"plugin-c"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-b", Deps: []string{"plugin-a"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-c", Deps: []string{"plugin-b"}, Setup: func(ctx *SetupContext) error { return nil }},
	}

	_, err := manager.topologicalSortV2(plugins)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency detected")
	t.Logf("Expected error: %v", err)
}

// TestTopologicalSort_CircularDependency_Indirect 测试间接循环依赖
func TestTopologicalSort_CircularDependency_Indirect(t *testing.T) {
	manager := NewManager(nil)

	// 复杂的循环依赖：
	// A -> B -> D
	// B -> C -> E
	// D -> E -> A (形成循环)
	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Deps: []string{"plugin-b"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-b", Deps: []string{"plugin-c", "plugin-d"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-c", Deps: []string{"plugin-e"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-d", Deps: []string{"plugin-e"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-e", Deps: []string{"plugin-a"}, Setup: func(ctx *SetupContext) error { return nil }}, // 循环！
	}

	_, err := manager.topologicalSortV2(plugins)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency detected")

	// 验证错误信息包含所有相关插件
	t.Logf("Circular dependency error: %v", err)
}

// TestTopologicalSort_MissingDependency 测试缺失依赖
func TestTopologicalSort_MissingDependency(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Deps: []string{"plugin-missing"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-b", Setup: func(ctx *SetupContext) error { return nil }},
	}

	_, err := manager.topologicalSortV2(plugins)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependency")
	assert.Contains(t, err.Error(), "plugin-missing")
}

// TestTopologicalSort_DuplicateNames 测试重复插件名
func TestTopologicalSort_DuplicateNames(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "plugin-a", Setup: func(ctx *SetupContext) error { return nil }}, // 重复
	}

	_, err := manager.topologicalSortV2(plugins)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate plugin name")
}

// TestTopologicalSort_ComplexDAG 测试复杂的有向无环图
func TestTopologicalSort_ComplexDAG(t *testing.T) {
	manager := NewManager(nil)

	// 复杂依赖图（无循环）：
	//     A
	//    / \
	//   B   C
	//   |\ /|
	//   | X |
	//   |/ \|
	//   D   E
	//    \ /
	//     F
	plugins := []*PluginDescriptor{
		{Name: "f", Deps: []string{"d", "e"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "d", Deps: []string{"b", "c"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "e", Deps: []string{"b", "c"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "b", Deps: []string{"a"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "c", Deps: []string{"a"}, Setup: func(ctx *SetupContext) error { return nil }},
		{Name: "a", Setup: func(ctx *SetupContext) error { return nil }},
	}

	sorted, err := manager.topologicalSortV2(plugins)
	require.NoError(t, err)
	assert.Equal(t, 6, len(sorted))

	// 验证拓扑顺序的正确性
	position := make(map[string]int)
	for i, p := range sorted {
		position[p.Name] = i
	}

	// A 必须在 B 和 C 之前
	assert.Less(t, position["a"], position["b"])
	assert.Less(t, position["a"], position["c"])

	// B 和 C 必须在 D 和 E 之前
	assert.Less(t, position["b"], position["d"])
	assert.Less(t, position["b"], position["e"])
	assert.Less(t, position["c"], position["d"])
	assert.Less(t, position["c"], position["e"])

	// D 和 E 必须在 F 之前
	assert.Less(t, position["d"], position["f"])
	assert.Less(t, position["e"], position["f"])

	t.Logf("Topological order: %v", getNames(sorted))
}

// TestValidateDependencies 测试依赖验证方法
func TestValidateDependencies(t *testing.T) {
	manager := NewManager(nil)

	t.Run("valid dependencies", func(t *testing.T) {
		plugins := []*PluginDescriptor{
			{Name: "a", Setup: func(ctx *SetupContext) error { return nil }},
			{Name: "b", Deps: []string{"a"}, Setup: func(ctx *SetupContext) error { return nil }},
		}
		err := manager.ValidateDependencies(plugins)
		assert.NoError(t, err)
	})

	t.Run("circular dependency", func(t *testing.T) {
		plugins := []*PluginDescriptor{
			{Name: "a", Deps: []string{"b"}, Setup: func(ctx *SetupContext) error { return nil }},
			{Name: "b", Deps: []string{"a"}, Setup: func(ctx *SetupContext) error { return nil }},
		}
		err := manager.ValidateDependencies(plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular")
	})
}

// TestRegisterMultipleV2 测试批量注册
func TestRegisterMultipleV2(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		manager := NewManager(nil)
		err := manager.RegisterMultipleV2([]*PluginDescriptor{})
		assert.NoError(t, err)
	})

	t.Run("nil descriptor", func(t *testing.T) {
		manager := NewManager(nil)
		err := manager.RegisterMultipleV2([]*PluginDescriptor{nil})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("empty name", func(t *testing.T) {
		manager := NewManager(nil)
		err := manager.RegisterMultipleV2([]*PluginDescriptor{
			{Name: "", Setup: func(ctx *SetupContext) error { return nil }},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty name")
	})

	t.Run("no setup function", func(t *testing.T) {
		manager := NewManager(nil)
		err := manager.RegisterMultipleV2([]*PluginDescriptor{
			{Name: "test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no setup function")
	})
}

// TestTopologicalSort_SelfDependency 测试自依赖
func TestTopologicalSort_SelfDependency(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{Name: "plugin-a", Deps: []string{"plugin-a"}, Setup: func(ctx *SetupContext) error { return nil }},
	}

	_, err := manager.topologicalSortV2(plugins)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

// Helper function
func getNames(descriptors []*PluginDescriptor) []string {
	names := make([]string, len(descriptors))
	for i, d := range descriptors {
		names[i] = d.Name
	}
	return names
}

// BenchmarkTopologicalSort 性能基准测试
func BenchmarkTopologicalSort(b *testing.B) {
	manager := NewManager(nil)

	// 创建一个大型的依赖图（100个插件）
	plugins := make([]*PluginDescriptor, 100)
	for i := range 100 {
		deps := make([]string, 0)
		// 每个插件依赖前面的 1-3 个插件
		if i > 0 {
			deps = append(deps, fmt.Sprintf("plugin-%d", i-1))
		}
		if i > 1 {
			deps = append(deps, fmt.Sprintf("plugin-%d", i-2))
		}
		if i > 2 {
			deps = append(deps, fmt.Sprintf("plugin-%d", i-3))
		}

		plugins[i] = &PluginDescriptor{
			Name:  fmt.Sprintf("plugin-%d", i),
			Deps:  deps,
			Setup: func(ctx *SetupContext) error { return nil },
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.topologicalSortV2(plugins)
		if err != nil {
			b.Fatal(err)
		}
	}
}
