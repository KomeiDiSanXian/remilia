package moderation_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/moderation"
)

func TestNewPlugin(t *testing.T) {
	p := moderation.NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
}

func TestNewPluginWithStore(t *testing.T) {
	p := moderation.NewPlugin(moderation.WithStore(t.TempDir()))
	if p == nil {
		t.Fatal("NewPlugin with store returned nil")
	}
}

func TestDescriptor(t *testing.T) {
	d := moderation.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "moderation" {
		t.Errorf("expected name %q, got %q", "moderation", d.Name)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "群组管理插件：禁言、踢出、警告、清屏" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
	if !d.Privileged {
		t.Error("expected Privileged to be true")
	}
}

func TestNewPluginDescriptor(t *testing.T) {
	d := moderation.NewPlugin().Descriptor()
	if d.Name != "moderation" {
		t.Errorf("expected name %q, got %q", "moderation", d.Name)
	}
}
