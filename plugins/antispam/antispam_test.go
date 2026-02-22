package antispam_test

import (
	"encoding/json"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/antispam"
	"testing"
	"time"
)

func newAntiSpamPlugin(t *testing.T, cfg antispam.Config) *antispam.Plugin {
	t.Helper()
	p, desc := antispam.NewPlugin(cfg)
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)
	if err := pm.RegisterV2(desc); err != nil {
		t.Fatalf("register: %v", err)
	}
	return p
}
func makeC2CCtx(userID string) *context.Context {
	detail, _ := json.Marshal(dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: "test",
			Author:  dto.Author{UserOpenID: userID},
		},
	})
	return context.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
}
func TestAntiSpam_Ban_Unban(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.DefaultConfig())
	p.Ban("uid1", 1*time.Hour)
	if !p.IsBanned("uid1") {
		t.Error("user should be banned")
	}
	p.Unban("uid1")
	if p.IsBanned("uid1") {
		t.Error("user should not be banned after Unban")
	}
}
func TestAntiSpam_BanExpiry(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.DefaultConfig())
	p.Ban("uid_exp", 30*time.Millisecond)
	if !p.IsBanned("uid_exp") {
		t.Error("user should be banned initially")
	}
	time.Sleep(50 * time.Millisecond)
	if p.IsBanned("uid_exp") {
		t.Error("ban should have expired")
	}
}
func TestAntiSpam_PermanentBan(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.DefaultConfig())
	p.Ban("uid_perm", 0)
	if !p.IsBanned("uid_perm") {
		t.Error("permanent ban should hold")
	}
}
func TestAntiSpam_Rule_BlocksBanned(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.Config{UserRate: 100, UserBurst: 100, BanOnViolation: false})
	rule := p.Rule()
	p.Ban("banned_user", 1*time.Hour)
	if rule(makeC2CCtx("banned_user")) {
		t.Error("rule should block banned user")
	}
}
func TestAntiSpam_Rule_AllowsNormal(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.Config{UserRate: 100, UserBurst: 100, BanOnViolation: false})
	rule := p.Rule()
	if !rule(makeC2CCtx("normal_user_spam")) {
		t.Error("normal user should be allowed")
	}
}
func TestAntiSpam_Rule_RateLimits(t *testing.T) {
	p := newAntiSpamPlugin(t, antispam.Config{UserRate: 1, UserBurst: 1, BanOnViolation: false})
	rule := p.Rule()
	ctx := makeC2CCtx("rl_user")
	if !rule(ctx) {
		t.Error("first call should be allowed")
	}
	if rule(ctx) {
		t.Error("second rapid call should be blocked")
	}
}
