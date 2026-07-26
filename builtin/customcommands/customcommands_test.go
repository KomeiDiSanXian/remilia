package customcommands_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/customcommands"
)

func TestNewPlugin(t *testing.T) {
	p := customcommands.NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
}

func TestNewPluginWithStore(t *testing.T) {
	p := customcommands.NewPlugin(customcommands.WithStore(t.TempDir()))
	if p == nil {
		t.Fatal("NewPlugin with store returned nil")
	}
}

func TestDescriptor(t *testing.T) {
	d := customcommands.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "customcommands" {
		t.Errorf("expected name %q, got %q", "customcommands", d.Name)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "用户自定义命令，无需写 Go 代码即可添加聊天命令" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
	if !d.Privileged {
		t.Error("expected Privileged to be true")
	}
}

func TestNewPluginDescriptor(t *testing.T) {
	d := customcommands.NewPlugin().Descriptor()
	if d.Name != "customcommands" {
		t.Errorf("expected name %q, got %q", "customcommands", d.Name)
	}
}
