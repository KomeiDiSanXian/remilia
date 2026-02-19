package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterV2_NoRepeatedServiceRegistration 测试不会重复注册特殊服务
func TestRegisterV2_NoRepeatedServiceRegistration(t *testing.T) {
	manager := NewManager(nil)

	// 注册第一个插件
	plugin1 := &PluginDescriptor{
		Name: "plugin1",
		Setup: func(ctx *SetupContext) error {
			return nil
		},
	}
	err := manager.RegisterV2(plugin1)
	require.NoError(t, err)

	// 验证容器中的特殊服务
	container := manager.GetContainer()
	assert.True(t, container.Has("manager"))
	assert.True(t, container.Has("engine"))
	assert.True(t, container.Has("coordinator"))

	// 获取特殊服务的引用
	managerService1, _ := container.Get("manager")
	engineService1, _ := container.Get("engine")
	coordService1, _ := container.Get("coordinator")

	// 注册第二个插件
	plugin2 := &PluginDescriptor{
		Name: "plugin2",
		Setup: func(ctx *SetupContext) error {
			return nil
		},
	}
	err = manager.RegisterV2(plugin2)
	require.NoError(t, err)

	// 验证特殊服务仍然存在
	assert.True(t, container.Has("manager"))
	assert.True(t, container.Has("engine"))
	assert.True(t, container.Has("coordinator"))

	// 验证特殊服务的引用没有改变（没有重复注册）
	managerService2, _ := container.Get("manager")
	engineService2, _ := container.Get("engine")
	coordService2, _ := container.Get("coordinator")

	// 指针应该相同，说明没有重复注册
	assert.Same(t, managerService1, managerService2, "Manager service should not be re-registered")
	assert.Same(t, engineService1, engineService2, "Engine service should not be re-registered")
	assert.Same(t, coordService1, coordService2, "Coordinator service should not be re-registered")

	t.Log("✓ Special services are not re-registered on subsequent RegisterV2 calls")
}

// TestRegisterV2_ContainerInitialization 测试容器的正确初始化
func TestRegisterV2_ContainerInitialization(t *testing.T) {
	manager := NewManager(nil)

	// 注册第一个插件
	plugin1 := &PluginDescriptor{
		Name: "plugin1",
		Setup: func(ctx *SetupContext) error {
			// 验证可以访问特殊服务
			mgr, ok := ctx.Get("manager")
			assert.True(t, ok, "Should be able to get manager")
			assert.NotNil(t, mgr, "Manager should not be nil")

			// engine 可能是 nil（如果 coordinator 是 nil）
			_, _ = ctx.Get("engine")
			_, _ = ctx.Get("coordinator")

			return nil
		},
	}
	err := manager.RegisterV2(plugin1)
	require.NoError(t, err)

	// 验证容器已初始化
	assert.NotNil(t, manager.container)

	// 验证容器中有所有必需的服务
	container := manager.GetContainer()
	assert.True(t, container.Has("manager"))
	assert.True(t, container.Has("plugin1"))

	t.Log("✓ Container is properly initialized")
}

// TestRegisterV2_PluginCanAccessSpecialServices 测试插件可以访问特殊服务
func TestRegisterV2_PluginCanAccessSpecialServices(t *testing.T) {
	manager := NewManager(nil)

	var accessedManager *Manager
	var accessedEngine any

	plugin := &PluginDescriptor{
		Name: "test",
		Setup: func(ctx *SetupContext) error {
			// 访问 manager
			mgr, ok := ctx.Get("manager")
			assert.True(t, ok)
			accessedManager = mgr.(*Manager)

			// 访问 engine
			eng, ok := ctx.Get("engine")
			assert.True(t, ok)
			accessedEngine = eng

			// 访问 coordinator（应该和 engine 是同一个）
			coord, ok := ctx.Get("coordinator")
			assert.True(t, ok)
			assert.Same(t, eng, coord)

			return nil
		},
	}

	err := manager.RegisterV2(plugin)
	require.NoError(t, err)

	// 验证获取到的是正确的实例
	assert.Same(t, manager, accessedManager)
	assert.Same(t, manager.coordinator, accessedEngine)

	t.Log("✓ Plugin can access special services correctly")
}

// TestEnsureContainerInitialized_Idempotent 测试容器初始化的幂等性
func TestEnsureContainerInitialized_Idempotent(t *testing.T) {
	manager := NewManager(nil)

	// 手动调用多次（模拟多次 RegisterV2）
	manager.mu.Lock()

	manager.ensureContainerInitialized()
	container1 := manager.container

	manager.ensureContainerInitialized()
	container2 := manager.container

	manager.ensureContainerInitialized()
	container3 := manager.container

	manager.mu.Unlock()

	// 验证容器实例没有改变
	assert.Same(t, container1, container2)
	assert.Same(t, container2, container3)

	// 验证特殊服务只注册了一次
	count := 0
	container1.mu.RLock()
	for name := range container1.services {
		if name == "manager" || name == "engine" || name == "coordinator" {
			count++
		}
	}
	container1.mu.RUnlock()

	assert.Equal(t, 3, count, "Should have exactly 3 special services")

	t.Log("✓ ensureContainerInitialized is idempotent")
}

// TestRegisterMultipleV2_SharedContainer 测试批量注册共享同一个容器
func TestRegisterMultipleV2_SharedContainer(t *testing.T) {
	manager := NewManager(nil)

	plugins := []*PluginDescriptor{
		{
			Name: "a",
			Setup: func(ctx *SetupContext) error {
				return nil
			},
		},
		{
			Name: "b",
			Deps: []string{"a"},
			Setup: func(ctx *SetupContext) error {
				// 应该能访问 a
				_, ok := ctx.Get("a")
				assert.True(t, ok)
				return nil
			},
		},
		{
			Name: "c",
			Deps: []string{"a", "b"},
			Setup: func(ctx *SetupContext) error {
				// 应该能访问 a 和 b
				_, ok := ctx.Get("a")
				assert.True(t, ok)
				_, ok = ctx.Get("b")
				assert.True(t, ok)
				return nil
			},
		},
	}

	err := manager.RegisterMultipleV2(plugins)
	require.NoError(t, err)

	// 验证所有插件共享同一个容器
	container := manager.GetContainer()
	assert.True(t, container.Has("a"))
	assert.True(t, container.Has("b"))
	assert.True(t, container.Has("c"))
	assert.True(t, container.Has("manager"))
	assert.True(t, container.Has("engine"))

	t.Log("✓ All plugins share the same container")
}
