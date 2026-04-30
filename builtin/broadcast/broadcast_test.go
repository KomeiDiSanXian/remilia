package broadcast_test

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/broadcast"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestBroadcast_NewPlugin verifies the plugin can be created with default config.
func TestBroadcast_NewPlugin(t *testing.T) {
	cfg := broadcast.DefaultConfig()
	if cfg.Rate <= 0 {
		t.Error("default rate should be positive")
	}
	if cfg.Burst <= 0 {
		t.Error("default burst should be positive")
	}

	p := broadcast.NewPlugin(cfg)
	if p == nil {
		t.Fatal("NewPlugin should not return nil")
	}
	t.Log("✓ broadcast.NewPlugin 创建正常")
}

// TestBroadcast_BroadcastNoSender verifies Broadcast returns errors when no sender is set.
func TestBroadcast_BroadcastNoSender(t *testing.T) {
	p := broadcast.NewPlugin(broadcast.DefaultConfig())

	result := p.Broadcast(context.Background(), []platform.ChatInfo{{ID: "chat001"}, {ID: "chat002"}}, platform.TextMessage("test"))
	if result.Total != 2 {
		t.Errorf("expected Total=2, got %d", result.Total)
	}
	if result.Failed != 2 {
		t.Errorf("expected Failed=2, got %d", result.Failed)
	}
	t.Log("✓ Broadcast without sender returns errors")
}
