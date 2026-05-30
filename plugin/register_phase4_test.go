package plugin_test

import (
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ─── P4-1: RegisterMultipleAtomic ───────────────────────────────────────────

func TestRegisterMultipleV2Atomic_Success(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	descriptors := []*plugin.Descriptor{
		{Name: "base", Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil }},
		{Name: "mid", Deps: []string{"base"}, Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil }},
		{Name: "top", Deps: []string{"mid"}, Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil }},
	}

	if err := pm.RegisterMultipleAtomic(descriptors); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	for _, name := range []string{"base", "mid", "top"} {
		inst, ok := pm.Get(name)
		if !ok {
			t.Errorf("expected plugin %q to be registered", name)
		}
		if inst.GetState() != plugin.Loaded {
			t.Errorf("expected plugin %q to be Loaded, got %s", name, inst.GetState())
		}
	}
}

func TestRegisterMultipleV2Atomic_RollbackOnFailure(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	failSetup := func(ctx *plugin.SetupContext) (any, error) {
		return nil, errors.New("intentional setup failure")
	}

	descriptors := []*plugin.Descriptor{
		{Name: "ok1", Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil }},
		{Name: "ok2", Deps: []string{"ok1"}, Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil }},
		{Name: "fail", Deps: []string{"ok2"}, Setup: failSetup},
	}

	err := pm.RegisterMultipleAtomic(descriptors)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// After rollback, none of the batch plugins should be registered
	for _, name := range []string{"ok1", "ok2", "fail"} {
		if _, ok := pm.Get(name); ok {
			t.Errorf("plugin %q should have been rolled back but is still registered", name)
		}
	}
}

func TestRegisterMultipleV2Atomic_EmptySlice(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())
	if err := pm.RegisterMultipleAtomic([]*plugin.Descriptor{}); err != nil {
		t.Fatalf("empty slice should succeed, got: %v", err)
	}
}

// ─── P4-2: PluginError rich error messages ─────────────────────────────────────

func TestPluginError_Format(t *testing.T) {
	err := &plugin.PluginError{
		PluginName:        "antispam",
		Operation:         "register",
		Cause:             errors.New(`missing required dependency "storage"`),
		RegisteredPlugins: []string{"permission", "cache", "acl"},
		Hint:              `register "storage" before "antispam"`,
	}

	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	// Should contain plugin name, operation, cause, registered list, hint
	for _, want := range []string{"antispam", "register", "storage", "permission", "cache"} {
		if !contains(msg, want) {
			t.Errorf("error message missing %q, got: %s", want, msg)
		}
	}
}

func TestPluginError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	pe := &plugin.PluginError{Cause: cause}
	if !errors.Is(pe, cause) {
		t.Error("errors.Is should work through PluginError.Unwrap")
	}
}

func TestVersionConstraintError_Format(t *testing.T) {
	err := &plugin.VersionConstraintError{
		Plugin:     "myplugin",
		Dependency: "storage",
		Required:   ">=2.0.0",
		Have:       "1.5.3",
	}
	msg := err.Error()
	for _, want := range []string{"myplugin", "storage", ">=2.0.0", "1.5.3"} {
		if !contains(msg, want) {
			t.Errorf("version constraint error missing %q, got: %s", want, msg)
		}
	}
}

// ─── P4-3: Version constraint checking ─────────────────────────────────────────

func TestRegisterV2_VersionConstraint_Satisfied(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	// Register v2.0.0 "auth"
	if err := pm.Register(&plugin.Descriptor{
		Name:    "auth",
		Version: "2.0.0",
		Setup:   func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	// Register plugin that requires "auth@>=1.5.0" — should succeed
	if err := pm.Register(&plugin.Descriptor{
		Name:    "consumer",
		Deps:    []string{"auth@>=1.5.0"},
		Version: "1.0.0",
		Setup:   func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatalf("expected success for auth v2.0.0 >= 1.5.0, got: %v", err)
	}
}

func TestRegisterV2_VersionConstraint_NotSatisfied(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	// Register v1.0.0 "auth"
	if err := pm.Register(&plugin.Descriptor{
		Name:    "auth",
		Version: "1.0.0",
		Setup:   func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	// Register plugin that requires "auth@>=2.0.0" — should fail
	err := pm.Register(&plugin.Descriptor{
		Name:    "consumer",
		Deps:    []string{"auth@>=2.0.0"},
		Version: "1.0.0",
		Setup:   func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("expected version constraint error, got nil")
	}

	var verErr *plugin.VersionConstraintError
	if !errors.As(err, &verErr) {
		t.Fatalf("expected *VersionConstraintError, got %T: %v", err, err)
	}
	if verErr.Required != ">=2.0.0" {
		t.Errorf("expected required '>=2.0.0', got %q", verErr.Required)
	}
	if verErr.Have != "1.0.0" {
		t.Errorf("expected have '1.0.0', got %q", verErr.Have)
	}
}

func TestRegisterV2_VersionConstraint_Caret(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	if err := pm.Register(&plugin.Descriptor{
		Name:    "lib",
		Version: "2.3.1",
		Setup:   func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	// ^2.0.0 means major==2 and >=2.0.0 → 2.3.1 satisfies
	if err := pm.Register(&plugin.Descriptor{
		Name:  "app",
		Deps:  []string{"lib@^2.0.0"},
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatalf("expected ^2.0.0 satisfied by 2.3.1, got: %v", err)
	}
}

// ─── P4-4: ConfigSchema validation ─────────────────────────────────────────────

func TestRegisterV2_MissingDepRichError(t *testing.T) {
	pm := plugin.NewManager(engine.NewEngine())

	if err := pm.Register(&plugin.Descriptor{
		Name:  "a",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	pm.SetStrictDeps(true)

	err := pm.Register(&plugin.Descriptor{
		Name:  "b",
		Deps:  []string{"MISSING_DEP"},
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("expected error for missing dep in strict mode, got nil")
	}

	var pe *plugin.PluginError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PluginError, got %T: %v", err, err)
	}
	if pe.PluginName != "b" {
		t.Errorf("expected plugin name 'b', got %q", pe.PluginName)
	}
	if pe.Hint == "" {
		t.Error("expected non-empty hint in PluginError")
	}
	if len(pe.RegisteredPlugins) == 0 {
		t.Error("expected RegisteredPlugins to be non-empty")
	}
}

// ─── P4-5: ReloadStrategy type ─────────────────────────────────────────────────

func TestReloadStrategy_Constants(t *testing.T) {
	// Verify the constants are distinct and have expected iota values
	if plugin.ReloadUnloadLoad != 0 {
		t.Errorf("ReloadUnloadLoad should be 0 (default iota), got %d", plugin.ReloadUnloadLoad)
	}
	if plugin.ReloadInPlace != 1 {
		t.Errorf("ReloadInPlace should be 1, got %d", plugin.ReloadInPlace)
	}
	if plugin.ReloadBlueGreen != 2 {
		t.Errorf("ReloadBlueGreen should be 2, got %d", plugin.ReloadBlueGreen)
	}
}

func TestPluginAdvanced_ReloadStrategy_Field(t *testing.T) {
	adv := &plugin.Advanced{
		Strategy: plugin.ReloadBlueGreen,
	}
	if adv.Strategy != plugin.ReloadBlueGreen {
		t.Errorf("expected ReloadBlueGreen, got %d", adv.Strategy)
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
