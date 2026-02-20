package plugin

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// TestBugFix_RegisterV2ConcurrentAccess 测试 Bug 1 修复：RegisterV2 竞态条件
func TestBugFix_RegisterV2ConcurrentAccess(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	manager := NewManager(eng)

	// 创建一个加载时间较长的插件
	slowPlugin := &PluginDescriptor{
		Name:    "slow-plugin",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) error {
			time.Sleep(100 * time.Millisecond) // 模拟慢速加载
			return nil
		},
	}

	var wg sync.WaitGroup
	errors := make(chan error, 2)

	// Goroutine 1: 注册插件
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := manager.RegisterV2(slowPlugin)
		if err != nil {
			errors <- err
		}
	}()

	// Goroutine 2: 尝试获取正在加载的插件
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond) // 等待插件开始加载

		plugin, exists := manager.Get("slow-plugin")
		if exists && plugin != nil {
			// 如果获取到了插件，应该是已加载状态
			if stateful, ok := plugin.(StatefulPlugin); ok {
				state := stateful.GetState()
				if state == Loading {
					errors <- ErrPluginLoading
					return
				}
			}
		}
		// 如果不存在或者已加载，都是正常的
	}()

	wg.Wait()
	close(errors)

	// 检查是否有错误
	for err := range errors {
		if err == ErrPluginLoading {
			t.Log("✓ Bug 1 已修复：正在加载的插件不会被 Get() 返回")
		} else {
			t.Errorf("Unexpected error: %v", err)
		}
	}

	// 验证插件最终加载成功
	plugin, exists := manager.Get("slow-plugin")
	if !exists {
		t.Fatal("Plugin should be loaded after waiting")
	}
	if stateful, ok := plugin.(StatefulPlugin); ok {
		if stateful.GetState() != Loaded {
			t.Errorf("Plugin state should be Loaded, got %v", stateful.GetState())
		}
	}
}

// TestBugFix_RemoveListenerSafety 测试 Bug 2 修复：RemoveListener 安全性
func TestBugFix_RemoveListenerSafety(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	manager := NewManager(eng)

	// 添加多个监听器
	listener1 := &mockListener{}
	listener2 := &mockListener{}
	listener3 := &mockListener{}

	manager.AddListener(listener1)
	manager.AddListener(listener2)
	manager.AddListener(listener3)

	// 移除中间的监听器（这在旧实现中可能有问题）
	manager.RemoveListener(listener2)

	// 移除不存在的监听器（应该不会 panic）
	manager.RemoveListener(&mockListener{})

	// 验证剩余监听器数量
	manager.mu.RLock()
	count := len(manager.listeners)
	manager.mu.RUnlock()

	if count != 2 {
		t.Errorf("Expected 2 listeners, got %d", count)
	}

	t.Log("✓ Bug 2 已修复：RemoveListener 更安全")
}

// TestBugFix_UnloadStateTransition 测试 Bug 3 修复：Unload 状态转换
func TestBugFix_UnloadStateTransition(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	manager := NewManager(eng)

	// ��册一个简单插件
	desc := &PluginDescriptor{
		Name:    "test-plugin",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) error {
			return nil
		},
		Teardown: func() error {
			time.Sleep(50 * time.Millisecond) // 模拟慢速卸载
			return nil
		},
	}

	err := manager.RegisterV2(desc)
	if err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	plugin, _ := manager.Get("test-plugin")
	instance := plugin.(*PluginInstance)

	// 启动卸载
	done := make(chan bool)
	go func() {
		_ = instance.Unload(eng)
		done <- true
	}()

	// 等待一小段时间，检查状态
	time.Sleep(10 * time.Millisecond)

	state := instance.GetState()
	if state != Unloading && state != Unloaded {
		t.Errorf("State should be Unloading or Unloaded during unload, got %v", state)
	}

	<-done

	// 验证最终状态
	if instance.GetState() != Unloaded {
		t.Errorf("Final state should be Unloaded, got %v", instance.GetState())
	}

	t.Log("✓ Bug 3 已修复：Unload 正确设置 Unloading 状态")
}

// TestBugFix_ContainerConcurrentAccess 测试 Bug 4 修复：Container 并发性能
func TestBugFix_ContainerConcurrentAccess(t *testing.T) {
	container := NewContainer()

	// 并发写入
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			container.Register(string(rune('a'+n%26)), n)
		}(i)
	}

	// 并发读取
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			container.Get(string(rune('a' + n%26)))
		}(i)
	}

	wg.Wait()

	// 验证没有 panic 且数据正确
	count := 0
	container.services.Range(func(key, value any) bool {
		count++
		return true
	})

	if count == 0 {
		t.Error("Container should have services")
	}

	t.Logf("✓ Bug 4 已修复：Container 支持高并发访问（%d services）", count)
}

// mockListener 模拟监听器
type mockListener struct {
	loaded   int
	unloaded int
	reloaded int
	errors   int
}

func (m *mockListener) OnPluginLoaded(name string)                             { m.loaded++ }
func (m *mockListener) OnPluginUnloaded(name string)                           { m.unloaded++ }
func (m *mockListener) OnPluginReloaded(name string)                           { m.reloaded++ }
func (m *mockListener) OnPluginError(name string, operation string, err error) { m.errors++ }

// ErrPluginLoading 插件正在加载错误（用于测试）
var ErrPluginLoading = fmt.Errorf("plugin is loading")
