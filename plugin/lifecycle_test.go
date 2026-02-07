package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/stretchr/testify/assert"
)

// TestPluginLifecycleIntegration 测试插件与 lifecycle 包的集成
func TestPluginLifecycleIntegration(t *testing.T) {
	t.Run("Component Basic Lifecycle", func(t *testing.T) {
		eng := engine.NewEngine()
		pluginManager := NewManager(eng)
		lifecycleManager := lifecycle.NewManager()

		// 创建测试插件
		plugin := NewBasePlugin("test-lifecycle")

		// 添加监听器验证加载和卸载
		loaded := false
		unloaded := false

		listener := &testLifecycleListener{
			onLoaded: func(name string) {
				if name == "test-lifecycle" {
					loaded = true
				}
			},
			onUnloaded: func(name string) {
				if name == "test-lifecycle" {
					unloaded = true
				}
			},
		}
		pluginManager.AddListener(listener)

		// 转换为 lifecycle.Component
		component := pluginManager.AsLifecycleComponent(plugin)

		// 注册到 lifecycle manager
		lifecycleManager.Register(component)

		// 启动
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := lifecycleManager.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, loaded, "Plugin should trigger load notification")

		// 验证状态
		assert.Equal(t, Loaded, plugin.GetState())

		// 等待一下
		time.Sleep(100 * time.Millisecond)

		// 停止
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()

		err = lifecycleManager.Stop(stopCtx)
		assert.NoError(t, err)
		assert.True(t, unloaded, "Plugin should trigger unload notification")

		// 验证状态
		assert.Equal(t, Unloaded, plugin.GetState())
	})

	t.Run("Multiple Plugins With Lifecycle", func(t *testing.T) {
		eng := engine.NewEngine()
		pluginManager := NewManager(eng)
		lifecycleManager := lifecycle.NewManager()

		// 创建多个插件
		plugin1 := NewBasePlugin("plugin1")
		plugin2 := NewBasePlugin("plugin2")

		// 直接转换为 lifecycle component（不通过 pluginManager.Register）
		component1 := NewPluginComponent(plugin1, eng, pluginManager)
		component2 := NewPluginComponent(plugin2, eng, pluginManager)

		lifecycleManager.Register(component1)
		lifecycleManager.Register(component2)

		// 启动
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := lifecycleManager.Start(ctx)
		assert.NoError(t, err)

		// 验证状态
		assert.Equal(t, Loaded, plugin1.GetState())
		assert.Equal(t, Loaded, plugin2.GetState())

		// 停止
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()

		err = lifecycleManager.Stop(stopCtx)
		assert.NoError(t, err)

		// 验证状态
		assert.Equal(t, Unloaded, plugin1.GetState())
		assert.Equal(t, Unloaded, plugin2.GetState())
	})

	t.Run("Lifecycle Listener Notification", func(t *testing.T) {
		eng := engine.NewEngine()
		pluginManager := NewManager(eng)
		lifecycleManager := lifecycle.NewManager()

		// 添加生命周期监听器
		loadedNames := make([]string, 0)
		unloadedNames := make([]string, 0)

		listener := &testLifecycleListener{
			onLoaded: func(name string) {
				loadedNames = append(loadedNames, name)
			},
			onUnloaded: func(name string) {
				unloadedNames = append(unloadedNames, name)
			},
		}

		pluginManager.AddListener(listener)

		// 创建插件
		plugin := NewBasePlugin("listener-test")

		// 转换并注册
		component := pluginManager.AsLifecycleComponent(plugin).(lifecycle.Component)
		lifecycleManager.Register(component)

		// 启动
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := lifecycleManager.Start(ctx)
		assert.NoError(t, err)

		// 等待通知
		time.Sleep(100 * time.Millisecond)
		assert.Contains(t, loadedNames, "listener-test")

		// 停止
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()

		err = lifecycleManager.Stop(stopCtx)
		assert.NoError(t, err)

		// 等待通知
		time.Sleep(100 * time.Millisecond)
		assert.Contains(t, unloadedNames, "listener-test")
	})
}

// testLifecycleListener 测试用的生命周期监听器
type testLifecycleListener struct {
	onLoaded   func(string)
	onUnloaded func(string)
	onReloaded func(string)
	onError    func(string, string, error)
}

func (l *testLifecycleListener) OnPluginLoaded(name string) {
	if l.onLoaded != nil {
		l.onLoaded(name)
	}
}

func (l *testLifecycleListener) OnPluginUnloaded(name string) {
	if l.onUnloaded != nil {
		l.onUnloaded(name)
	}
}

func (l *testLifecycleListener) OnPluginReloaded(name string) {
	if l.onReloaded != nil {
		l.onReloaded(name)
	}
}

func (l *testLifecycleListener) OnPluginError(name string, operation string, err error) {
	if l.onError != nil {
		l.onError(name, operation, err)
	}
}

// TestPluginComponentName 测试 Component 名称格式
func TestPluginComponentName(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	plugin := NewBasePlugin("my-plugin")
	component := manager.AsLifecycleComponent(plugin)

	// 类型断言获取 Name 方法
	if named, ok := component.(interface{ Name() string }); ok {
		name := named.Name()
		assert.Equal(t, "plugin:my-plugin", name)
	} else {
		t.Fatal("Component should have Name() method")
	}
}
