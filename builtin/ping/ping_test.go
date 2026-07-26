package ping_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ping"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

func TestPingDescriptor(t *testing.T) {
	d := ping.New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "ping" {
		t.Errorf("expected name %q, got %q", "ping", d.Name)
	}
	if d.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", d.Version)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "消息处理延迟检测" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
}

func TestPingSetup(t *testing.T) {
	d := ping.New()
	if d.Setup == nil {
		t.Fatal("Setup is nil")
	}

	ctx := plugintest.NewSetupContext("ping", nil)
	svc, err := d.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if svc != nil {
		t.Errorf("expected nil service, got %v", svc)
	}
}
