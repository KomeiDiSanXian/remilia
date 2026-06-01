package plugin

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// mockConfigProvider 可自定义行为的测试 ConfigProvider
type mockConfigProvider struct {
	subCalled atomic.Int64
	subFn     func(name string) map[string]any

	onChangeCalled atomic.Int64
	onChangeFn     func(callback func())

	stopped atomic.Bool
}

func (m *mockConfigProvider) Sub(name string) map[string]any {
	m.subCalled.Add(1)
	if m.subFn != nil {
		return m.subFn(name)
	}
	return nil
}

func (m *mockConfigProvider) OnConfigChange(callback func()) {
	m.onChangeCalled.Add(1)
	if m.onChangeFn != nil {
		m.onChangeFn(callback)
	}
}

// stopProvider implements Stop()
type stopProvider struct {
	mockConfigProvider
	stopFn func()
}

func (s *stopProvider) Stop() {
	s.stopped.Store(true)
	if s.stopFn != nil {
		s.stopFn()
	}
}

// syncConfigProvider 注册回调时立即同步触发（用于死锁测试）
type syncConfigProvider struct {
	mockConfigProvider
}

func (s *syncConfigProvider) OnConfigChange(callback func()) {
	s.onChangeCalled.Add(1)
	callback() // 同步触发！潜在的 deadlock 场景
}

// ---- Tests ----------------------------------------------------------------

// TestSetConfigProvider_Basic 替换 provider 后已有插件应读取更新后的配置
func TestSetConfigProvider_Basic(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	oldData := map[string]any{"key": "old"}
	newData := map[string]any{"key": "new"}

	// 先注册一个插件（使用旧 provider，实现 Stop 接口）
	oldCp := &stopProvider{
		mockConfigProvider: mockConfigProvider{
			subFn: func(name string) map[string]any {
				return oldData
			},
		},
	}
	pm.config.configProvider = oldCp
	pm.Register(&Descriptor{
		Name:  "test",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	// 验证一开始是 old 值
	cfg := pm.plugins["test"].GetConfig()
	if v := cfg.GetString("key", ""); v != "old" {
		t.Fatalf("expected 'old', got %q", v)
	}

	// 替换为新 provider
	newCp := &mockConfigProvider{
		subFn: func(name string) map[string]any {
			return newData
		},
	}
	pm.SetConfigProvider(newCp)

	// 验证配置已被替换
	cfg = pm.plugins["test"].GetConfig()
	if v := cfg.GetString("key", ""); v != "new" {
		t.Fatalf("expected 'new', got %q", v)
	}

	// 验证旧 provider 的 Stop 被调用了
	if !oldCp.stopped.Load() {
		t.Fatal("old provider's Stop() should be called")
	}
}

// TestSetConfigProvider_NoStop 旧 provider 未实现 Stop() 不应 panic
func TestSetConfigProvider_NoStop(t *testing.T) {
	pm := NewManager(engine.NewEngine(engine.WithNoBackgroundWorkers()))
	pm.config.configProvider = &mockConfigProvider{}
	pm.Register(&Descriptor{
		Name:  "test",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	newCp := &mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"key": "val"}
		},
	}
	pm.SetConfigProvider(newCp) // 不应 panic

	if cfg := pm.plugins["test"].GetConfig(); cfg != nil {
		if v := cfg.GetString("key", ""); v != "val" {
			t.Fatalf("expected 'val', got %q", v)
		}
	}
}

// TestSetConfigProvider_CallbackRegistered 新 provider 的 OnConfigChange 被调用
func TestSetConfigProvider_CallbackRegistered(t *testing.T) {
	pm := NewManager(engine.NewEngine(engine.WithNoBackgroundWorkers()))

	registered := make(chan struct{}, 1)
	cp := &mockConfigProvider{
		onChangeFn: func(_ func()) {
			registered <- struct{}{}
		},
	}
	pm.SetConfigProvider(cp)

	select {
	case <-registered:
		// OK
	default:
		t.Fatal("OnConfigChange was not called")
	}
}

// TestSetConfigProvider_Nil 设为 nil 时不应 panic，旧 provider 仍应停止
func TestSetConfigProvider_Nil(t *testing.T) {
	pm := NewManager(engine.NewEngine(engine.WithNoBackgroundWorkers()))

	oldCp := &stopProvider{}
	pm.config.configProvider = oldCp
	pm.SetConfigProvider(nil)

	if !oldCp.stopped.Load() {
		t.Fatal("old provider's Stop() should be called even when setting nil")
	}
	if pm.config.configProvider != nil {
		t.Fatal("configProvider should be nil")
	}
}

// TestSetConfigProvider_Concurrent 并发替换 provider：最终一致
func TestSetConfigProvider_Concurrent(t *testing.T) {
	pm := NewManager(engine.NewEngine(engine.WithNoBackgroundWorkers()))

	// 注册一个插件
	pm.config.configProvider = &mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"src": "initial"}
		},
	}
	pm.Register(&Descriptor{
		Name:  "test",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		val := i
		go func() {
			defer wg.Done()
			cp := &stopProvider{
				mockConfigProvider: mockConfigProvider{
					subFn: func(name string) map[string]any {
						return map[string]any{"val": val}
					},
				},
			}
			pm.SetConfigProvider(cp)
		}()
	}
	wg.Wait()

	// 无论最后一次是哪个 provider，config 应该包含 val key
	cfg := pm.plugins["test"].GetConfig()
	if cfg == nil {
		t.Fatal("config should not be nil")
	}
	if v := cfg.Get("val"); v == nil {
		t.Fatal("expected 'val' key in config after concurrent replacement")
	}
}

// TestSetConfigProvider_SyncOnConfigChange 同步触发回调不应死锁
func TestSetConfigProvider_SyncOnConfigChange(t *testing.T) {
	pm := NewManager(engine.NewEngine(engine.WithNoBackgroundWorkers()))

	// 注册一个插件
	pm.config.configProvider = &mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"original": "value"}
		},
	}
	pm.Register(&Descriptor{
		Name:  "test",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	// 使用同步触发回调的 provider——不应死锁
	syncCp := &syncConfigProvider{
		mockConfigProvider: mockConfigProvider{
			subFn: func(name string) map[string]any {
				return map[string]any{"synced": "ok"}
			},
		},
	}
	pm.SetConfigProvider(syncCp)

	// 验证存活（走到这里就没死锁）
	cfg := pm.plugins["test"].GetConfig()
	if v := cfg.GetString("synced", ""); v != "ok" {
		t.Fatalf("expected 'ok', got %q", v)
	}
}

// TestSetConfigProvider_ThenRegister 替换 provider 后新注册的插件使用新 provider
func TestSetConfigProvider_ThenRegister(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	pm.SetConfigProvider(&mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"from": "new-provider"}
		},
	})

	// 替换后注册新插件
	pm.Register(&Descriptor{
		Name:  "late",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	cfg := pm.plugins["late"].GetConfig()
	if v := cfg.GetString("from", ""); v != "new-provider" {
		t.Fatalf("expected 'new-provider', got %q", v)
	}
}

// TestSetConfigProvider_MultiplePlugins 多个插件的配置都应被替换
func TestSetConfigProvider_MultiplePlugins(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	pm.config.configProvider = &mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"val": "old"}
		},
	}
	for _, name := range []string{"a", "b", "c"} {
		n := name
		pm.Register(&Descriptor{
			Name:  n,
			Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
		})
	}

	pm.SetConfigProvider(&mockConfigProvider{
		subFn: func(name string) map[string]any {
			return map[string]any{"val": "new", "name": name}
		},
	})

	for _, name := range []string{"a", "b", "c"} {
		cfg := pm.plugins[name].GetConfig()
		if v := cfg.GetString("val", ""); v != "new" {
			t.Fatalf("plugin %s: expected 'new', got %q", name, v)
		}
		if v := cfg.GetString("name", ""); v != name {
			t.Fatalf("plugin %s: expected name %q, got %q", name, name, v)
		}
	}
}
