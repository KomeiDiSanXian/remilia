package plugin

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScope_SubscribeAutoCleanup 验证 Scope 订阅在 Dispose 后自动取消。
func TestScope_SubscribeAutoCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
		pm := NewManager(eng)
		eb := pm.GetEventBus()

		var mu sync.Mutex
		var received []string
		desc := &Descriptor{
			Name: "test-scope",
			Setup: func(ctx *SetupContext) (any, error) {
				root := ctx.Scope()
				root.Subscribe("test.topic", func(_ context.Context, data any) error {
					mu.Lock()
					received = append(received, data.(string))
					mu.Unlock()
					return nil
				})
				return nil, nil
			},
		}
		require.NoError(t, pm.Register(desc))

		// 发布事件——应收到（EventBus 异步分发，等待 delivery）
		eb.Publish("test.topic", "hello")
		eb.Publish("test.topic", "world")
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		assert.Len(t, received, 2)
		assert.Contains(t, received, "hello")
		assert.Contains(t, received, "world")
		mu.Unlock()

		// 卸载插件——Scope 自动 Dispose
		pm.Unregister(t.Context(), "test-scope")
		mu.Lock()
		received = nil
		mu.Unlock()

		// 再次发布——不应收到（订阅已被取消）
		eb.Publish("test.topic", "should-not-receive")
		mu.Lock()
		assert.Empty(t, received)
		mu.Unlock()
	})
}

// TestScope_ChildScopeCascadeDispose 验证子 Scope 级联清理。
func TestScope_ChildScopeCascadeDispose(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)
	eb := pm.GetEventBus()

	var disposeOrder []string
	desc := &Descriptor{
		Name: "test-cascade",
		Setup: func(ctx *SetupContext) (any, error) {
			root := ctx.Scope()
			root.OnDispose(func() error {
				disposeOrder = append(disposeOrder, "root")
				return nil
			})

			child := root.Scope("child")
			child.OnDispose(func() error {
				disposeOrder = append(disposeOrder, "child")
				return nil
			})

			grandchild := child.Scope("grandchild")
			grandchild.OnDispose(func() error {
				disposeOrder = append(disposeOrder, "grandchild")
				return nil
			})

			// 验证订阅自动清理
			grandchild.Subscribe("x", func(_ context.Context, data any) error { return nil })
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(desc))

	// 验证订阅自动清理——先验证有订阅，卸载后没了
	stats := eb.GetStats()
	assert.GreaterOrEqual(t, stats.SubscriptionCount, 1)

	pm.Unregister(t.Context(), "test-cascade")

	// 验证级联顺序：grandchild → child → root
	assert.Equal(t, []string{"grandchild", "child", "root"}, disposeOrder)

	// 验证订阅被清理
	stats = eb.GetStats()
	assert.Equal(t, 0, stats.SubscriptionCount)
}

// TestScope_OnDisposeOrder 验证 OnDispose 回调按逆序执行。
func TestScope_OnDisposeOrder(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	var order []string
	desc := &Descriptor{
		Name: "test-dispose-order",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.OnDispose(func() error { order = append(order, "a"); return nil })
			ctx.OnDispose(func() error { order = append(order, "b"); return nil })
			ctx.OnDispose(func() error { order = append(order, "c"); return nil })
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(desc))
	pm.Unregister(t.Context(), "test-dispose-order")
	assert.Equal(t, []string{"c", "b", "a"}, order)
}

// TestService_ResolveValue 验证 Service[T] 正确返回依赖值。
func TestService_ResolveValue(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	type Counter struct{ N int }

	descProvider := &Descriptor{
		Name: "provider",
		Setup: func(ctx *SetupContext) (any, error) {
			return &Counter{N: 1}, nil
		},
	}
	require.NoError(t, pm.Register(descProvider))

	var svc *Counter
	descConsumer := &Descriptor{
		Name:    "consumer",
		Deps:    []string{"provider"},
		Version: "1.0.0",
		Setup: func(ctx *SetupContext) (any, error) {
			svc = ctx.Service[*Counter]("provider")
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(descConsumer))

	require.NotNil(t, svc)
	assert.Equal(t, 1, svc.N)
}

// TestService_NotAvailable 验证 Service[T] 返回直接指针。
func TestService_NotAvailable(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	desc := &Descriptor{
		Name: "temp-provider",
		Setup: func(ctx *SetupContext) (any, error) {
			return &struct{}{}, nil
		},
	}
	require.NoError(t, pm.Register(desc))

	var svc *struct{}
	descConsumer := &Descriptor{
		Name: "consumer",
		Deps: []string{"temp-provider"},
		Setup: func(ctx *SetupContext) (any, error) {
			svc = ctx.Service[*struct{}]("temp-provider")
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(descConsumer))
	require.NotNil(t, svc)

	// 卸载 provider 后，svc 指向已返回的实例（非 proxy，因此 svc 仍然可用）
	pm.Unregister(t.Context(), "temp-provider")
	require.NotNil(t, svc)
}

// TestTryService_Optional 验证 TryService 可选依赖。
func TestTryService_Optional(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	var svc *struct{}
	var ok bool
	desc := &Descriptor{
		Name: "optional-consumer",
		Setup: func(ctx *SetupContext) (any, error) {
			svc, ok = ctx.TryService[*struct{}]("nonexistent")
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(desc))
	assert.False(t, ok)
	assert.Nil(t, svc)
}

// TestMigrateState_VersionChange 验证版本变化触发 MigrateState。
func TestMigrateState_VersionChange(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	type State struct{ Count int }
	var migratedFrom, migratedTo string
	var migratedCount int

	desc := &Descriptor{
		Name:    "migrate-test",
		Version: "1.0.0",
		Advanced: &Advanced{
			SaveState: func() (any, error) {
				return &State{Count: 42}, nil
			},
			RestoreState: func(state any) error {
				s := state.(*State)
				migratedCount = s.Count
				return nil
			},
			MigrateState: func(oldState any, oldVer, newVer string) (any, error) {
				migratedFrom = oldVer
				migratedTo = newVer
				s := oldState.(*State)
				s.Count *= 2
				return s, nil
			},
		},
		Setup: func(ctx *SetupContext) (any, error) {
			return &State{}, nil
		},
	}
	require.NoError(t, pm.Register(desc))

	// 升级版本后重载
	inst, _ := pm.Get("migrate-test")
	inst.mu.Lock()
	inst.desc = &Descriptor{
		Name:     "migrate-test",
		Version:  "2.0.0",
		Advanced: desc.Advanced,
		Setup:    desc.Setup,
	}
	inst.mu.Unlock()
	require.NoError(t, pm.Reload(t.Context(), "migrate-test"))

	assert.Equal(t, "1.0.0", migratedFrom)
	assert.Equal(t, "2.0.0", migratedTo)
	assert.Equal(t, 84, migratedCount) // 42 * 2
}

// TestMigrateState_NoVersionChange 验证版本不变时不触发 MigrateState。
func TestMigrateState_NoVersionChange(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	pm := NewManager(eng)

	migrateCalled := false
	desc := &Descriptor{
		Name:    "no-migrate",
		Version: "1.0.0",
		Advanced: &Advanced{
			SaveState:    func() (any, error) { return "old", nil },
			RestoreState: func(state any) error { return nil },
			MigrateState: func(oldState any, oldVer, newVer string) (any, error) {
				migrateCalled = true
				return oldState, nil
			},
		},
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}
	require.NoError(t, pm.Register(desc))

	// 同版本重载——不应触发 Migration
	require.NoError(t, pm.Reload(t.Context(), "no-migrate"))
	assert.False(t, migrateCalled)
}
