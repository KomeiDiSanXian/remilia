package broadcast_test

import (
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugins/broadcast"
)

// TestBroadcast_ErrAPINotSet verifies that sending without SetAPI returns ErrAPINotSet
// in the Result and does NOT panic (Bug 2.3 fix).
func TestBroadcast_ErrAPINotSet(t *testing.T) {
	p := broadcast.NewPlugin(broadcast.DefaultConfig())

	// ToGroups without SetAPI
	result := p.ToGroups([]string{"group1", "group2"}, &dto.Message{Content: "test"})
	if result.Total != 2 {
		t.Errorf("expected Total=2, got %d", result.Total)
	}
	if result.Failed != 2 {
		t.Errorf("expected Failed=2, got %d", result.Failed)
	}
	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(result.Errors))
	}
	for i, err := range result.Errors {
		if !errors.Is(err, broadcast.ErrAPINotSet) {
			t.Errorf("error[%d]: expected ErrAPINotSet, got %v", i, err)
		}
	}

	// ToC2C without SetAPI
	result2 := p.ToC2C([]string{"user1"}, &dto.Message{Content: "test"})
	if result2.Failed != 1 {
		t.Errorf("expected Failed=1, got %d", result2.Failed)
	}
	if !errors.Is(result2.Errors[0], broadcast.ErrAPINotSet) {
		t.Errorf("expected ErrAPINotSet, got %v", result2.Errors[0])
	}

	t.Log("✓ Bug 2.3 修复：SetAPI 未调用时返回 ErrAPINotSet 而不是 panic")
}

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
