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
	pm.RegisterV2(&PluginDescriptor{
		Name:  "alpha",
		Deps:  nil,
		Setup: func(ctx *SetupContext) error { return nil },
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
	// still registered
	if !pm.IsLoaded("alpha") {
		t.Error("plugin should remain in Loaded state after Disable()")
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
	pm.RegisterV2(&PluginDescriptor{
		Name:  "beta",
		Setup: func(ctx *SetupContext) error { return nil },
	})

	pm.Disable("beta")
	// second disable should not error
	if err := pm.Disable("beta"); err != nil {
		t.Fatalf("second Disable should not error: %v", err)
	}
}

func TestEnable_WhenNotDisabled(t *testing.T) {
	pm := NewManager(engine.NewEngine())
	pm.RegisterV2(&PluginDescriptor{
		Name:  "gamma",
		Setup: func(ctx *SetupContext) error { return nil },
	})
	// Enable on a non-disabled plugin should not error
	if err := pm.Enable("gamma"); err != nil {
		t.Fatalf("Enable on non-disabled plugin should not error: %v", err)
	}
}
