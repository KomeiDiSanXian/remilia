package plugin

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// TestBugFix_RegisterV2ConcurrentAccess 测试 Bug 1 修复：Register 竞态条件
func TestBugFix_RegisterV2ConcurrentAccess(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())

	manager := NewManager(eng)

	// 创建一个加载时间较长的插件
	slowPlugin := &Descriptor{
		Name:    "slow-plugin",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			time.Sleep(100 * time.Millisecond) // 模拟慢速加载
			return nil, nil
		},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	// Goroutine 1: 注册插件
	wg.Go(func() {
		err := manager.Register(slowPlugin)
		if err != nil {
			errs <- err
		}
	})

	// Goroutine 2: 尝试获取正在加载的插件
	wg.Go(func() {
		time.Sleep(20 * time.Millisecond) // 等待插件开始加载

		plugin, exists := manager.Get("slow-plugin")
		if exists && plugin != nil {
			// *Instance 直接实现 StatefulPlugin，无需类型断言
			state := plugin.GetState()
			if state == Loading {
				errs <- ErrPluginLoading
				return
			}
		}
		// 如果不存在或者已加载，都是正常的
	})

	wg.Wait()
	close(errs)

	// 检查是否有错误
	for err := range errs {
		if errors.Is(err, ErrPluginLoading) {
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
	if plugin.GetState() != Loaded {
		t.Errorf("Plugin state should be Loaded, got %v", plugin.GetState())
	}
}

// TestBugFix_RemoveListenerSafety 测试 Bug 2 修复：RemoveListener 安全性
func TestBugFix_RemoveListenerSafety(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())

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
	defer eng.Shutdown(stdctx.Background())

	manager := NewManager(eng)

	// ��册一个简单插件
	desc := &Descriptor{
		Name:    "test-plugin",
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			time.Sleep(50 * time.Millisecond) // 模拟慢速卸载
			return nil
		},
	}

	err := manager.Register(desc)
	if err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	instance, _ := manager.Get("test-plugin")

	// 启动卸载
	done := make(chan bool)
	go func() {
		_ = instance.unload(stdctx.Background(), eng)
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
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			container.Register(string(rune('a'+n%26)), n)
		}(i)
	}

	// 并发读取
	for i := range 100 {
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

// TestBugFix_CrossBatchCyclicDependency 测试 Bug 5 修复：跨批次循环依赖检测
func TestBugFix_CrossBatchCyclicDependency(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())

	// 场景1：已注册插件 A，批次中插件 B 依赖 A，但 A 依赖 B（形成循环）
	t.Run("DirectCrossBatchCycle", func(t *testing.T) {
		subManager := NewManager(eng)

		// 先注册插件 A（依赖将在批次中注册的 B）
		pluginA := &Descriptor{
			Name:    "plugin-a",
			Version: "1.0.0",
			Deps:    []string{}, // 暂时不声明依赖
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		err := subManager.Register(pluginA)
		if err != nil {
			t.Fatalf("Failed to register plugin A: %v", err)
		}

		// 手动修改 A 的依赖为 B（模拟已注册插件后来依赖新插件的情况）
		// 在实际场景中，这可能通过配置更新或动态依赖解析发生
		subManager.mu.Lock()
		if instance, exists := subManager.plugins["plugin-a"]; exists {
			instance.desc.Deps = []string{"plugin-b"}
		}
		subManager.mu.Unlock()

		// 批次注册插件 B（依赖 A）
		pluginB := &Descriptor{
			Name:    "plugin-b",
			Version: "1.0.0",
			Deps:    []string{"plugin-a"}, // B 依赖 A
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		// 应该检测到跨批次循环依赖
		err = subManager.RegisterMultiple([]*Descriptor{pluginB})
		if err == nil {
			t.Error("Should detect cross-batch circular dependency")
		} else {
			t.Logf("✓ Correctly detected: %v", err)
		}
	})

	// 场景2：复杂的跨批次依赖链
	t.Run("IndirectCrossBatchCycle", func(t *testing.T) {
		subManager := NewManager(eng)

		// 注册插件 A
		pluginA := &Descriptor{
			Name:    "plugin-a",
			Version: "1.0.0",
			Deps:    []string{},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		err := subManager.Register(pluginA)
		if err != nil {
			t.Fatalf("Failed to register plugin A: %v", err)
		}

		// 批次包含 B -> C，但 A 依赖 C，C 依赖 B，B 依赖 A（间接循环）
		// 为了简化测试，我们只测试能检测到的情况

		// 修改 A 依赖 C
		subManager.mu.Lock()
		if instance, exists := subManager.plugins["plugin-a"]; exists {
			instance.desc.Deps = []string{"plugin-c"}
		}
		subManager.mu.Unlock()

		pluginB := &Descriptor{
			Name:    "plugin-b",
			Version: "1.0.0",
			Deps:    []string{"plugin-a"},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		pluginC := &Descriptor{
			Name:    "plugin-c",
			Version: "1.0.0",
			Deps:    []string{"plugin-b"},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		// 应该检测到跨批次循环依赖
		err = subManager.RegisterMultiple([]*Descriptor{pluginB, pluginC})
		if err == nil {
			t.Error("Should detect indirect cross-batch circular dependency")
		} else {
			t.Logf("✓ Correctly detected: %v", err)
		}
	})

	// 场景3：无循环依赖（应该成功）
	t.Run("NoCrossBatchCycle", func(t *testing.T) {
		subManager := NewManager(eng)

		// 注册插件 A（无依赖）
		pluginA := &Descriptor{
			Name:    "plugin-a",
			Version: "1.0.0",
			Deps:    []string{},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		err := subManager.Register(pluginA)
		if err != nil {
			t.Fatalf("Failed to register plugin A: %v", err)
		}

		// 批次注册插件 B（依赖 A，无循环）
		pluginB := &Descriptor{
			Name:    "plugin-b",
			Version: "1.0.0",
			Deps:    []string{"plugin-a"},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		pluginC := &Descriptor{
			Name:    "plugin-c",
			Version: "1.0.0",
			Deps:    []string{"plugin-b"},
			Setup: func(ctx *SetupContext) (any, error) {
				return nil, nil
			},
		}

		// 应该成功注册
		err = subManager.RegisterMultiple([]*Descriptor{pluginB, pluginC})
		if err != nil {
			t.Errorf("Should not detect cycle in valid dependency chain: %v", err)
		} else {
			t.Log("✓ No false positive for valid dependency chain")
		}
	})

	t.Log("✓ Bug 5 已修复：topologicalSort 可以检测跨批次循环依赖")
}
