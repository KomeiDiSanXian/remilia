package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPluginCircularDependencyDetection 测试循环依赖检测
func TestPluginCircularDependencyDetection(t *testing.T) {
	t.Run("直接循环：A -> B -> A", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		// 创建循环依赖的插件
		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"B"},
		}

		pluginB := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("B"),
			deps:       []string{"A"},
		}

		// 尝试注册，应该检测到循环依赖
		err := pm.RegisterWithDependencies([]Plugin{pluginA, pluginB})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("间接循环：A -> B -> C -> A", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"B"},
		}

		pluginB := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("B"),
			deps:       []string{"C"},
		}

		pluginC := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("C"),
			deps:       []string{"A"},
		}

		// 尝试注册，应该检测到间接循环
		err := pm.RegisterWithDependencies([]Plugin{pluginA, pluginB, pluginC})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("复杂循环：A -> B, C -> D, D -> A", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"B"},
		}

		pluginB := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("B"),
			deps:       []string{}, // B 无依赖
		}

		pluginC := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("C"),
			deps:       []string{"D"},
		}

		pluginD := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("D"),
			deps:       []string{"A"}, // D 依赖 A，但 A 依赖 B，形成间接循环
		}

		// 注意：这实际上不是循环，因为 A -> B, C -> D -> A -> B
		// 没有节点回到自己
		err := pm.RegisterWithDependencies([]Plugin{pluginA, pluginB, pluginC, pluginD})
		assert.NoError(t, err) // 应该成功
	})

	t.Run("自依赖：A -> A", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"A"}, // 自己依赖自己
		}

		err := pm.RegisterWithDependencies([]Plugin{pluginA})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("长循环：A -> B -> C -> D -> E -> A", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		plugins := []Plugin{
			&CircularTestPlugin{BasePlugin: NewBasePlugin("A"), deps: []string{"B"}},
			&CircularTestPlugin{BasePlugin: NewBasePlugin("B"), deps: []string{"C"}},
			&CircularTestPlugin{BasePlugin: NewBasePlugin("C"), deps: []string{"D"}},
			&CircularTestPlugin{BasePlugin: NewBasePlugin("D"), deps: []string{"E"}},
			&CircularTestPlugin{BasePlugin: NewBasePlugin("E"), deps: []string{"A"}},
		}

		err := pm.RegisterWithDependencies(plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("菱形依赖（非循环）：A -> B, A -> C, B -> D, C -> D", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		pluginD := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("D"),
			deps:       []string{},
		}

		pluginB := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("B"),
			deps:       []string{"D"},
		}

		pluginC := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("C"),
			deps:       []string{"D"},
		}

		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"B", "C"},
		}

		err := pm.RegisterWithDependencies([]Plugin{pluginA, pluginB, pluginC, pluginD})
		assert.NoError(t, err) // 菱形依赖是合法的
	})
}

// TestPluginDependencyOrder 测试依赖顺序
func TestPluginDependencyOrder(t *testing.T) {
	t.Run("线性依赖：A -> B -> C", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		var loadOrder []string

		pluginC := &OrderTestPlugin{
			BasePlugin: NewBasePlugin("C"),
			deps:       []string{},
			loadOrder:  &loadOrder,
		}

		pluginB := &OrderTestPlugin{
			BasePlugin: NewBasePlugin("B"),
			deps:       []string{"C"},
			loadOrder:  &loadOrder,
		}

		pluginA := &OrderTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"B"},
			loadOrder:  &loadOrder,
		}

		err := pm.RegisterWithDependencies([]Plugin{pluginA, pluginB, pluginC})
		assert.NoError(t, err)

		// 验证加载顺序：C -> B -> A
		assert.Equal(t, []string{"C", "B", "A"}, loadOrder)
	})

	t.Run("复杂依赖图", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		var loadOrder []string

		// 依赖图：
		// A -> B, C
		// B -> D
		// C -> D
		// D -> E
		// E -> (无)
		// 正确顺序应该是：E -> D -> (B, C 任意顺序) -> A

		plugins := []Plugin{
			&OrderTestPlugin{BasePlugin: NewBasePlugin("E"), deps: []string{}, loadOrder: &loadOrder},
			&OrderTestPlugin{BasePlugin: NewBasePlugin("D"), deps: []string{"E"}, loadOrder: &loadOrder},
			&OrderTestPlugin{BasePlugin: NewBasePlugin("B"), deps: []string{"D"}, loadOrder: &loadOrder},
			&OrderTestPlugin{BasePlugin: NewBasePlugin("C"), deps: []string{"D"}, loadOrder: &loadOrder},
			&OrderTestPlugin{BasePlugin: NewBasePlugin("A"), deps: []string{"B", "C"}, loadOrder: &loadOrder},
		}

		err := pm.RegisterWithDependencies(plugins)
		assert.NoError(t, err)

		// 验证 E 在 D 之前
		eIndex := indexOf(loadOrder, "E")
		dIndex := indexOf(loadOrder, "D")
		assert.True(t, eIndex < dIndex, "E should be loaded before D")

		// 验证 D 在 B 和 C 之前
		bIndex := indexOf(loadOrder, "B")
		cIndex := indexOf(loadOrder, "C")
		assert.True(t, dIndex < bIndex, "D should be loaded before B")
		assert.True(t, dIndex < cIndex, "D should be loaded before C")

		// 验证 B 和 C 在 A 之前
		aIndex := indexOf(loadOrder, "A")
		assert.True(t, bIndex < aIndex, "B should be loaded before A")
		assert.True(t, cIndex < aIndex, "C should be loaded before A")
	})
}

// TestPluginMissingDependency 测试缺失依赖
func TestPluginMissingDependency(t *testing.T) {
	t.Run("依赖不存在", func(t *testing.T) {
		engine := NewEngine()
		pm := NewPluginManager(engine)

		pluginA := &CircularTestPlugin{
			BasePlugin: NewBasePlugin("A"),
			deps:       []string{"NonExistent"},
		}

		err := pm.RegisterWithDependencies([]Plugin{pluginA})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// CircularTestPlugin 用于测试循环依赖的插件
type CircularTestPlugin struct {
	*BasePlugin
	deps []string
}

func (p *CircularTestPlugin) Dependencies() []string {
	return p.deps
}

func (p *CircularTestPlugin) Load(engine *Engine) error {
	// 简单实现，不做实际加载
	return nil
}

// OrderTestPlugin 用于测试加载顺序的插件
type OrderTestPlugin struct {
	*BasePlugin
	deps      []string
	loadOrder *[]string
}

func (p *OrderTestPlugin) Dependencies() []string {
	return p.deps
}

func (p *OrderTestPlugin) Load(engine *Engine) error {
	*p.loadOrder = append(*p.loadOrder, p.Name())
	return nil
}

// indexOf 辅助函数，查找元素在切片中的位置
func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
