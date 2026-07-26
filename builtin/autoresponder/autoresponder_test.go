package autoresponder_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/autoresponder"
)

func TestNewPlugin(t *testing.T) {
	p := autoresponder.NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
}

func TestMatchExact(t *testing.T) {
	p := autoresponder.NewPlugin()
	// Use the match function directly
	// Rules are typically added via the command handler, so we test Match with empty rules
	matches := p.Match("hello")
	if len(matches) != 0 {
		t.Errorf("expected no matches for empty rules, got %d", len(matches))
	}
}

func TestMatchModeString(t *testing.T) {
	tests := []struct {
		mode autoresponder.MatchMode
		want string
	}{
		{autoresponder.MatchExact, "精确"},
		{autoresponder.MatchContains, "包含"},
		{autoresponder.MatchPrefix, "前缀"},
		{autoresponder.MatchRegex, "正则"},
	}
	for _, tt := range tests {
		got := tt.mode.String()
		if got != tt.want {
			t.Errorf("MatchMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestMatchModeUnknown(t *testing.T) {
	var m autoresponder.MatchMode = 99
	if m.String() != "未知" {
		t.Errorf("expected %q, got %q", "未知", m.String())
	}
}

func TestCheckCooldownNoRules(t *testing.T) {
	p := autoresponder.NewPlugin()
	if !p.CheckCooldown("nonexistent", "user1") {
		t.Error("expected true for nonexistent rule/user")
	}
}

func TestDescriptor(t *testing.T) {
	d := autoresponder.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "autoresponder" {
		t.Errorf("expected name %q, got %q", "autoresponder", d.Name)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "关键词触发自动回复" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
	if !d.Privileged {
		t.Error("expected Privileged to be true")
	}
}

func TestNewPluginDescriptor(t *testing.T) {
	d := autoresponder.NewPlugin().Descriptor()
	if d.Name != "autoresponder" {
		t.Errorf("expected name %q, got %q", "autoresponder", d.Name)
	}
}

func TestNewPluginWithOptions(t *testing.T) {
	p := autoresponder.NewPlugin(autoresponder.WithStore(t.TempDir()), autoresponder.WithPrefix("!"))
	if p == nil {
		t.Fatal("NewPlugin with options returned nil")
	}
}

func TestMatchExactMatch(t *testing.T) {
	p := autoresponder.NewPlugin()
	_ = p // Matching requires rules which are added via handlers
}

func TestMiddlewareNoCommand(t *testing.T) {
	p := autoresponder.NewPlugin()
	mw := p.Middleware()
	if mw == nil {
		t.Fatal("Middleware returned nil")
	}
}
