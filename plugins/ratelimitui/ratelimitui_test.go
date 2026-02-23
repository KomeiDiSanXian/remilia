package ratelimitui_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugins/antispam"
	"github.com/KomeiDiSanXian/remilia/plugins/cooldown"
	"github.com/KomeiDiSanXian/remilia/plugins/ratelimitui"
)

func TestNewPlugin_NoBindings(t *testing.T) {
	p := ratelimitui.NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin should not return nil")
	}
	if p.HasAntispam() {
		t.Error("should not have antispam before binding")
	}
	if p.HasCooldown() {
		t.Error("should not have cooldown before binding")
	}
}

func TestBind_Antispam(t *testing.T) {
	p := ratelimitui.NewPlugin()
	ap := antispam.NewPlugin(antispam.DefaultConfig())
	p.BindAntispam(ap)
	if !p.HasAntispam() {
		t.Error("HasAntispam should be true after BindAntispam")
	}
}

func TestBind_Cooldown(t *testing.T) {
	p := ratelimitui.NewPlugin()
	cp := cooldown.NewPlugin()
	p.BindCooldown(cp)
	if !p.HasCooldown() {
		t.Error("HasCooldown should be true after BindCooldown")
	}
}

func TestGetBanSummary(t *testing.T) {
	p := ratelimitui.NewPlugin()
	ap := antispam.NewPlugin(antispam.DefaultConfig())
	ap.Ban("user1", 10*time.Minute)
	ap.Ban("user2", 0)
	p.BindAntispam(ap)

	bans := p.ListBanSummary()
	if len(bans) != 2 {
		t.Fatalf("expected 2 bans, got %d", len(bans))
	}
}

func TestGetStats_BothPlugins(t *testing.T) {
	p := ratelimitui.NewPlugin()
	ap := antispam.NewPlugin(antispam.DefaultConfig())
	ap.Ban("bad1", time.Hour)
	p.BindAntispam(ap)

	cp := cooldown.NewPlugin()
	cp.Allow("u1", "cmd", time.Hour)
	p.BindCooldown(cp)

	stats := p.GetStats()
	if stats.BanCount != 1 {
		t.Errorf("expected 1 ban, got %d", stats.BanCount)
	}
	if stats.ActiveCooldowns != 1 {
		t.Errorf("expected 1 cooldown, got %d", stats.ActiveCooldowns)
	}
}

func TestUnban_DelegatesToAntispam(t *testing.T) {
	p := ratelimitui.NewPlugin()
	ap := antispam.NewPlugin(antispam.DefaultConfig())
	ap.Ban("victim", time.Hour)
	p.BindAntispam(ap)

	if err := p.Unban("victim"); err != nil {
		t.Fatalf("Unban: %v", err)
	}
	if ap.IsBanned("victim") {
		t.Error("user should not be banned after Unban")
	}
}

func TestUnban_NoAntispam(t *testing.T) {
	p := ratelimitui.NewPlugin()
	if err := p.Unban("anyone"); err == nil {
		t.Error("expected error when antispam not bound")
	}
}

func TestResetCooldown(t *testing.T) {
	p := ratelimitui.NewPlugin()
	cp := cooldown.NewPlugin()
	cp.Allow("alice", "daily", time.Hour)
	p.BindCooldown(cp)

	if cp.Allow("alice", "daily", time.Hour) {
		t.Fatal("should be in cooldown")
	}

	if err := p.ResetCooldown("alice", "daily"); err != nil {
		t.Fatalf("ResetCooldown: %v", err)
	}

	if !cp.Allow("alice", "daily", time.Hour) {
		t.Error("should be allowed after reset")
	}
}
