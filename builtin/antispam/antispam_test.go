package antispam_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/antispam"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// antispam.Plugin 的初始化全在 NewPlugin() 中完成（不依赖 Setup），
// 直接构造即可，无需走 manager 注册流程。
func newAntiSpamPlugin(cfg antispam.Config) *antispam.Plugin {
	return antispam.NewPlugin(cfg)
}

// makeC2CCtxPlatform 使用平台无关路径创建测试 Context（推荐）
func makeC2CCtxPlatform(userID string) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, "test")
	return context.NewContextFromEvent(event, nil)
}

// makeC2CCtx 使用平台无关路径创建测试 Context
func makeC2CCtx(userID string) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, "test")
	return context.NewContextFromEvent(event, nil)
}

func TestAntiSpam_Ban_Unban(t *testing.T) {
	p := newAntiSpamPlugin(antispam.DefaultConfig())
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
	synctest.Test(t, func(t *testing.T) {
		p := newAntiSpamPlugin(antispam.DefaultConfig())
		p.Ban("uid_exp", 30*time.Millisecond)
		if !p.IsBanned("uid_exp") {
			t.Error("user should be banned initially")
		}
		time.Sleep(50 * time.Millisecond)
		if p.IsBanned("uid_exp") {
			t.Error("ban should have expired")
		}
	})
}
func TestAntiSpam_PermanentBan(t *testing.T) {
	p := newAntiSpamPlugin(antispam.DefaultConfig())
	p.Ban("uid_perm", 0)
	if !p.IsBanned("uid_perm") {
		t.Error("permanent ban should hold")
	}
}

// TestAntiSpam_Rule_BlocksBanned_Platform tests the Rule using the platform-agnostic path.
func TestAntiSpam_Rule_BlocksBanned_Platform(t *testing.T) {
	p := newAntiSpamPlugin(antispam.Config{UserRate: 100, UserBurst: 100})
	rule := p.Rule()
	p.Ban("banned_user", 1*time.Hour)
	if rule(makeC2CCtxPlatform("banned_user")) {
		t.Error("rule should block banned user (platform path)")
	}
}

// TestAntiSpam_Rule_AllowsNormal_Platform tests normal users pass via platform-agnostic path.
func TestAntiSpam_Rule_AllowsNormal_Platform(t *testing.T) {
	p := newAntiSpamPlugin(antispam.Config{UserRate: 100, UserBurst: 100})
	rule := p.Rule()
	if !rule(makeC2CCtxPlatform("normal_user")) {
		t.Error("normal user should be allowed (platform path)")
	}
}

func TestAntiSpam_Rule_BlocksBanned(t *testing.T) {
	p := newAntiSpamPlugin(antispam.Config{UserRate: 100, UserBurst: 100})
	rule := p.Rule()
	p.Ban("banned_user", 1*time.Hour)
	if rule(makeC2CCtx("banned_user")) {
		t.Error("rule should block banned user")
	}
}
func TestAntiSpam_Rule_AllowsNormal(t *testing.T) {
	p := newAntiSpamPlugin(antispam.Config{UserRate: 100, UserBurst: 100})
	rule := p.Rule()
	if !rule(makeC2CCtx("normal_user")) {
		t.Error("normal user should be allowed")
	}
}
func TestAntiSpam_Rule_RateLimits(t *testing.T) {
	p := newAntiSpamPlugin(antispam.Config{UserRate: 1, UserBurst: 1})
	rule := p.Rule()
	ctx := makeC2CCtxPlatform("rl_user")
	if !rule(ctx) {
		t.Error("first call should be allowed")
	}
	if rule(ctx) {
		t.Error("second rapid call should be blocked")
	}
}
