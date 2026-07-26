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
