package plugin

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// ---- Disable / Enable tests -----------------------------------------------

func TestDisable_UnknownPlugin(t *testing.T) {
	pm := NewManager(engine.NewEngine())
	err := pm.Disable("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestEnable_UnknownPlugin(t *testing.T) {
	pm := NewManager(engine.NewEngine())
	err := pm.Enable("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestDisable_ThenEnable(t *testing.T) {
	eng := engine.NewEngine()
	pm := NewManager(eng)

	// register a simple plugin
	pm.Register(&Descriptor{
		Name:  "alpha",
		Deps:  nil,
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	if pm.IsDisabled("alpha") {
		t.Fatal("plugin should not be disabled after registration")
	}

	// Disable
	if err := pm.Disable("alpha"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if !pm.IsDisabled("alpha") {
		t.Error("plugin should be disabled after Disable()")
	}
	// P1-3: Disable 后状态变为 Disabled，IsLoaded 返回 false
	// 但插件仍注册在 Manager 中（Count 不变）
	if pm.IsLoaded("alpha") {
		t.Error("plugin should NOT be in Loaded state after Disable() (state is Disabled)")
	}
	if _, exists := pm.Get("alpha"); !exists {
		t.Error("plugin should still be accessible via Get() after Disable()")
	}
	inst, _ := pm.Get("alpha")
	if inst.GetState() != Disabled {
		t.Errorf("expected Disabled state, got %v", inst.GetState())
	}

	// Enable
	if err := pm.Enable("alpha"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if pm.IsDisabled("alpha") {
		t.Error("plugin should not be disabled after Enable()")
	}
}

func TestDisable_Idempotent(t *testing.T) {
	pm := NewManager(engine.NewEngine())
	pm.Register(&Descriptor{
		Name:  "beta",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})

	pm.Disable("beta")
	// second disable should not error
	if err := pm.Disable("beta"); err != nil {
		t.Fatalf("second Disable should not error: %v", err)
	}
}

func TestEnable_WhenNotDisabled(t *testing.T) {
	pm := NewManager(engine.NewEngine())
	pm.Register(&Descriptor{
		Name:  "gamma",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	})
	// Enable on a non-disabled plugin should not error
	if err := pm.Enable("gamma"); err != nil {
		t.Fatalf("Enable on non-disabled plugin should not error: %v", err)
	}
}
