package plugin

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestManager_PublishesLifecycleEvents verifies that the manager publishes
// plugin.loaded, plugin.unloaded, and plugin.reloaded events to the EventBus
// after each lifecycle operation (Bug 2.8 fix: help plugin cache invalidation).
func TestManager_PublishesLifecycleEvents(t *testing.T) {
	pm := NewManager(nil)
	bus := pm.GetEventBus()

	var loadedName, unloadedName, reloadedName atomic.Value

	_, _ = bus.Subscribe("plugin.loaded", func(_ context.Context, data any) error {
		if name, ok := data.(string); ok {
			loadedName.Store(name)
		}
		return nil
	})
	_, _ = bus.Subscribe("plugin.unloaded", func(_ context.Context, data any) error {
		if name, ok := data.(string); ok {
			unloadedName.Store(name)
		}
		return nil
	})
	_, _ = bus.Subscribe("plugin.reloaded", func(_ context.Context, data any) error {
		if name, ok := data.(string); ok {
			reloadedName.Store(name)
		}
		return nil
	})

	desc := &Descriptor{
		Name: "event-test",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
	}

	// Register → expect plugin.loaded
	if err := pm.Register(desc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // EventBus is async
	if v := loadedName.Load(); v == nil || v.(string) != "event-test" {
		t.Errorf("expected plugin.loaded event with 'event-test', got %v", v)
	}

	// Reload → expect plugin.reloaded
	if err := pm.Reload(context.Background(), "event-test"); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if v := reloadedName.Load(); v == nil || v.(string) != "event-test" {
		t.Errorf("expected plugin.reloaded event with 'event-test', got %v", v)
	}

	// Unregister → expect plugin.unloaded
	if err := pm.Unregister(context.Background(), "event-test"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if v := unloadedName.Load(); v == nil || v.(string) != "event-test" {
		t.Errorf("expected plugin.unloaded event with 'event-test', got %v", v)
	}

	t.Log("✓ Bug 2.8 修复：Manager 在 loaded/unloaded/reloaded 时向 EventBus 发布生命周期事件")
}
