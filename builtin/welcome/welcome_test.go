package welcome_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/welcome"
)

func TestNewPlugin(t *testing.T) {
	p := welcome.NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
}

func TestNewPluginWithOptions(t *testing.T) {
	p := welcome.NewPlugin(welcome.WithStore(t.TempDir()))
	if p == nil {
		t.Fatal("NewPlugin with store returned nil")
	}
}

func TestDescriptor(t *testing.T) {
	d := welcome.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "welcome" {
		t.Errorf("expected name %q, got %q", "welcome", d.Name)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "入群欢迎/退群告别消息" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
}

func TestNewPluginDescriptor(t *testing.T) {
	d := welcome.NewPlugin().Descriptor()
	if d.Name != "welcome" {
		t.Errorf("expected name %q, got %q", "welcome", d.Name)
	}
}

func TestGetOrCreateConfig(t *testing.T) {
	p := welcome.NewPlugin()
	_ = p // getOrCreateConfig is unexported, tested indirectly
}

func TestEffectiveConfig_FallsBackToGlobal(t *testing.T) {
	p := welcome.NewPlugin()
	p.SetGlobalWelcome("欢迎 {user} 加入本群！", true)

	cfg := p.EffectiveConfig("group-1")
	if !cfg.WelcomeEnabled {
		t.Fatal("expected welcome enabled from global fallback")
	}
	if cfg.WelcomeMessage != "欢迎 {user} 加入本群！" {
		t.Fatalf("unexpected welcome message: %q", cfg.WelcomeMessage)
	}
}

func TestEffectiveConfig_GroupOverridesGlobal(t *testing.T) {
	p := welcome.NewPlugin()
	p.SetGlobalWelcome("全局欢迎", true)
	p.SetGroupWelcome("group-1", "本群专属欢迎", true)
	p.SetGroupWelcome("group-1", "", false)

	cfg := p.EffectiveConfig("group-1")
	if cfg.WelcomeEnabled {
		t.Fatal("expected group-level off to override global on")
	}
	if cfg.WelcomeMessage != "" {
		t.Fatalf("expected empty group message, got %q", cfg.WelcomeMessage)
	}

	cfg = p.EffectiveConfig("group-2")
	if !cfg.WelcomeEnabled || cfg.WelcomeMessage != "全局欢迎" {
		t.Fatalf("expected group-2 to fall back to global, got %+v", cfg)
	}
}

func TestEffectiveConfig_EmptyPlugin(t *testing.T) {
	p := welcome.NewPlugin()
	cfg := p.EffectiveConfig("any-group")
	if cfg.WelcomeEnabled || cfg.FarewellEnabled {
		t.Fatal("expected empty plugin to be disabled everywhere")
	}
}
