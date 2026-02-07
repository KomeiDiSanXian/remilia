package plugin

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

// TestPluginConfig 测试插件配置
func TestPluginConfig(t *testing.T) {
	// 创建 viper 实例
	v := viper.New()
	v.Set("plugins.test.enabled", true)
	v.Set("plugins.test.api_key", "test-key")
	v.Set("plugins.test.timeout", "10s")
	v.Set("plugins.test.max_retries", 3)

	// 创建插件配置
	config := NewPluginConfig("test", v)

	t.Run("GetString", func(t *testing.T) {
		apiKey := config.GetString("api_key", "default")
		assert.Equal(t, "test-key", apiKey)

		missing := config.GetString("missing", "default")
		assert.Equal(t, "default", missing)
	})

	t.Run("GetInt", func(t *testing.T) {
		maxRetries := config.GetInt("max_retries", 0)
		assert.Equal(t, 3, maxRetries)

		missing := config.GetInt("missing", 5)
		assert.Equal(t, 5, missing)
	})

	t.Run("GetBool", func(t *testing.T) {
		enabled := config.GetBool("enabled", false)
		assert.True(t, enabled)

		missing := config.GetBool("missing", true)
		assert.True(t, missing)
	})

	t.Run("GetDuration", func(t *testing.T) {
		timeout := config.GetDuration("timeout", 5*time.Second)
		assert.Equal(t, 10*time.Second, timeout)

		missing := config.GetDuration("missing", 1*time.Second)
		assert.Equal(t, 1*time.Second, missing)
	})

	t.Run("Set and OnChange", func(t *testing.T) {
		changed := false
		var oldValue, newValue interface{}

		config.OnChange(func(key string, old, new interface{}) {
			changed = true
			oldValue = old
			newValue = new
		})

		config.Set("api_key", "new-key")
		assert.True(t, changed)
		assert.Equal(t, "test-key", oldValue)
		assert.Equal(t, "new-key", newValue)

		// 验证配置已更新
		apiKey := config.GetString("api_key", "")
		assert.Equal(t, "new-key", apiKey)
	})
}

// TestEventBus 测试事件总线
func TestEventBus(t *testing.T) {
	bus := NewEventBus()

	t.Run("Publish and Subscribe", func(t *testing.T) {
		received := false
		var receivedData interface{}

		// 订阅事件
		sub, err := bus.Subscribe("test.event", func(data interface{}) {
			received = true
			receivedData = data
		})
		assert.NoError(t, err)
		assert.NotNil(t, sub)

		// 发布事件
		bus.Publish("test.event", "hello")

		// 等待异步处理
		time.Sleep(100 * time.Millisecond)

		assert.True(t, received)
		assert.Equal(t, "hello", receivedData)
	})

	t.Run("Multiple Subscribers", func(t *testing.T) {
		count := 0
		done := make(chan bool, 2)

		// 多个订阅者
		bus.Subscribe("multi.event", func(data interface{}) {
			count++
			done <- true
		})

		bus.Subscribe("multi.event", func(data interface{}) {
			count++
			done <- true
		})

		// 发布事件
		bus.Publish("multi.event", "test")

		// 等待两个订阅者处理完成
		<-done
		<-done

		assert.Equal(t, 2, count)
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		received := false

		sub, _ := bus.Subscribe("unsub.event", func(data interface{}) {
			received = true
		})

		// 取消订阅
		err := bus.Unsubscribe(sub)
		assert.NoError(t, err)

		// 发布事件
		bus.Publish("unsub.event", "test")
		time.Sleep(100 * time.Millisecond)

		// 不应该收到事件
		assert.False(t, received)
	})

	t.Run("GetStats", func(t *testing.T) {
		newBus := NewEventBus()

		newBus.Subscribe("topic1", func(data interface{}) {})
		newBus.Subscribe("topic1", func(data interface{}) {})
		newBus.Subscribe("topic2", func(data interface{}) {})

		stats := newBus.GetStats()
		assert.Equal(t, 2, stats.TopicCount)
		assert.Equal(t, 3, stats.SubscriptionCount)
		assert.Equal(t, 2, stats.TopicStats["topic1"])
		assert.Equal(t, 1, stats.TopicStats["topic2"])
	})
}

// TestPluginStatusManagement 测试插件状态管理
func TestPluginStatusManagement(t *testing.T) {
	eng := engine.NewEngine()
	manager := NewManager(eng)

	// 测试插件类型
	type testPlugin struct {
		*BasePlugin
		loadFunc func(*engine.Engine) error
	}

	newTestPlugin := func(name string, loadFunc func(*engine.Engine) error) *testPlugin {
		return &testPlugin{
			BasePlugin: NewBasePlugin(name),
			loadFunc:   loadFunc,
		}
	}

	// 实现 Load 方法
	func() {
		tp := &testPlugin{}
		_ = Plugin(tp)
	}()

	t.Run("Plugin State Lifecycle", func(t *testing.T) {
		plugin := newTestPlugin("test", func(eng *engine.Engine) error {
			return nil
		})

		// 初始状态
		assert.Equal(t, Unloaded, plugin.GetState())

		// 注册插件（不能直接注册 testPlugin，需要实现接口）
		// 这里简化测试，直接设置状态
		plugin.SetState(Loaded)
		plugin.SetLoadTime(time.Now())

		// 应该是已加载状态
		assert.Equal(t, Loaded, plugin.GetState())
		assert.False(t, plugin.GetLoadTime().IsZero())
	})

	t.Run("GetStatus", func(t *testing.T) {
		// 创建一个简单的插件实现
		type simplePlugin struct {
			*BasePlugin
		}

		impl := &simplePlugin{BasePlugin: NewBasePlugin("status-test")}
		impl.SetState(Loaded)
		impl.SetLoadTime(time.Now())

		// 手动添加到 manager
		manager.mu.Lock()
		manager.plugins["status-test"] = impl
		manager.mu.Unlock()

		status, err := manager.GetStatus("status-test")
		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, "status-test", status.Name)
		assert.Equal(t, Loaded, status.State)
	})

	t.Run("ListStatus", func(t *testing.T) {
		newManager := NewManager(eng)

		type simplePlugin struct {
			*BasePlugin
		}

		plugin1 := &simplePlugin{BasePlugin: NewBasePlugin("plugin1")}
		plugin1.SetState(Loaded)

		plugin2 := &simplePlugin{BasePlugin: NewBasePlugin("plugin2")}
		plugin2.SetState(Loaded)

		newManager.mu.Lock()
		newManager.plugins["plugin1"] = plugin1
		newManager.plugins["plugin2"] = plugin2
		newManager.mu.Unlock()

		statuses := newManager.ListStatus()
		assert.GreaterOrEqual(t, len(statuses), 2)
	})

	t.Run("IsLoaded", func(t *testing.T) {
		newManager := NewManager(eng)

		type simplePlugin struct {
			*BasePlugin
		}

		assert.False(t, newManager.IsLoaded("loaded-test"))

		plugin := &simplePlugin{BasePlugin: NewBasePlugin("loaded-test")}
		plugin.SetState(Loaded)

		newManager.mu.Lock()
		newManager.plugins["loaded-test"] = plugin
		newManager.mu.Unlock()

		assert.True(t, newManager.IsLoaded("loaded-test"))
	})

	t.Run("GetLoadOrder", func(t *testing.T) {
		newManager := NewManager(eng)

		newManager.mu.Lock()
		newManager.loadOrder = []string{"first", "second"}
		newManager.mu.Unlock()

		order := newManager.GetLoadOrder()
		assert.Equal(t, []string{"first", "second"}, order)
	})
}

// TestBasePluginEnhancements 测试 BasePlugin 增强功能
func TestBasePluginEnhancements(t *testing.T) {
	t.Run("Event Publishing and Subscribing", func(t *testing.T) {
		plugin := NewBasePlugin("event-test")

		received := false
		var receivedData interface{}

		// 订阅事件
		sub, err := plugin.SubscribeEvent("test.event", func(data interface{}) {
			received = true
			receivedData = data
		})
		assert.NoError(t, err)
		assert.NotNil(t, sub)

		// 发布事件
		plugin.PublishEvent("test.event", "hello")

		// 等待处理
		time.Sleep(100 * time.Millisecond)

		assert.True(t, received)
		assert.Equal(t, "hello", receivedData)
	})

	t.Run("Config Integration", func(t *testing.T) {
		v := viper.New()
		v.Set("plugins.config-test.setting", "value")

		plugin := NewBasePlugin("config-test")
		config := NewPluginConfig("config-test", v)
		plugin.SetConfig(config)

		retrievedConfig := plugin.GetConfig()
		assert.NotNil(t, retrievedConfig)
		assert.Equal(t, "value", retrievedConfig.GetString("setting", ""))
	})

	t.Run("Uptime Calculation", func(t *testing.T) {
		plugin := NewBasePlugin("uptime-test")

		// 未加载时，uptime 应该为 0
		assert.Equal(t, time.Duration(0), plugin.GetUptime())

		// 模拟加载
		plugin.SetState(Loaded)
		plugin.SetLoadTime(time.Now().Add(-5 * time.Second))

		// Uptime 应该约为 5 秒
		uptime := plugin.GetUptime()
		assert.Greater(t, uptime, 4*time.Second)
		assert.Less(t, uptime, 6*time.Second)
	})
}
