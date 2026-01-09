package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestOnUserWhitelist tests user whitelist rule
func TestOnUserWhitelist(t *testing.T) {
	engine := NewEngine()
	whitelistRule := OnUserWhitelist("user1", "user2")

	// 使用 engine 注册带有白名单规则的处理器
	var allowed bool
	engine.OnC2C(whitelistRule).Handle(func(ctx *Context) {
		allowed = true
	})

	// Test with whitelisted user
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"author": {
				"user_openid": "user1"
			}
		}`),
	}
	ctx := NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放
	assert.True(t, allowed)

	// Reset flag
	allowed = false

	// Test with non-whitelisted user
	payload2 := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"author": {
				"user_openid": "user999"
			}
		}`),
	}
	ctx2 := NewContext(payload2, nil)
	engine.ProcessEvent(ctx2) // autoRelease 会自动释放
	assert.False(t, allowed)
}

// TestOnUserBlacklist tests user blacklist rule
func TestOnUserBlacklist(t *testing.T) {
	rule := OnUserBlacklist("banned1", "banned2")

	// Test with blacklisted user
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"author": {
				"user_openid": "banned1"
			}
		}`),
	}
	ctx := NewContext(event, nil)
	assert.False(t, rule(ctx))

	// Test with normal user
	event2 := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"author": {
				"user_openid": "normaluser"
			}
		}`),
	}
	ctx2 := NewContext(event2, nil)
	assert.True(t, rule(ctx2))
}

// TestOnGroupWhitelist tests group whitelist rule
func TestOnGroupWhitelist(t *testing.T) {
	rule := OnGroupWhitelist("group1", "group2")

	// Test with whitelisted group
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
		Detail: []byte(`{
			"group_openid": "group1"
		}`),
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Test with non-whitelisted group
	event2 := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
		Detail: []byte(`{
			"group_openid": "group999"
		}`),
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

// TestOnGroupBlacklist tests group blacklist rule
func TestOnGroupBlacklist(t *testing.T) {
	rule := OnGroupBlacklist("spam1", "spam2")

	// Test with blacklisted group
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
		Detail: []byte(`{
			"group_openid": "spam1"
		}`),
	}
	ctx := NewContext(event, nil)
	assert.False(t, rule(ctx))

	// Test with normal group
	event2 := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
		Detail: []byte(`{
			"group_openid": "normalgroup"
		}`),
	}
	ctx2 := NewContext(event2, nil)
	assert.True(t, rule(ctx2))
}

// TestOnHasPermission tests permission check rule
func TestOnHasPermission(t *testing.T) {
	pm := NewPermissionManager()
	assert.NoError(t, pm.AssignRole("user123", "admin"))

	rule := OnHasPermission("admin", "manage")

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// Set up permission manager and user ID
	ctx.SetPermissionManager(pm)
	ctx.SetUserID("user123")

	// Admin should have permission
	assert.True(t, rule(ctx))

	// User without permission
	ctx.SetUserID("user999")
	assert.False(t, rule(ctx))
}

// TestOnHasRole tests role check rule
func TestOnHasRole(t *testing.T) {
	pm := NewPermissionManager()
	assert.NoError(t, pm.AssignRole("user123", "admin"))

	rule := OnHasRole("admin")

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// Set up permission manager and user ID
	ctx.SetPermissionManager(pm)
	ctx.SetUserID("user123")

	// User has admin role
	assert.True(t, rule(ctx))

	// User without role
	ctx.SetUserID("user999")
	assert.False(t, rule(ctx))
}

// TestConvenienceRules_Integration tests rules in real engine
func TestConvenienceRules_Integration(t *testing.T) {
	engine := NewEngine()
	pm := NewPermissionManager()
	assert.NoError(t, pm.AssignRole("admin_user", "admin"))

	engine.Use(RequirePermissionMiddleware(pm))

	executed := false

	// Use multiple convenience rules
	engine.OnGroupAt(
		OnUserWhitelist("admin_user", "mod_user"),
		OnGroupWhitelist("allowed_group"),
		OnHasRole("admin"),
	).Handle(func(ctx *Context) {
		executed = true
	})

	// Test with matching event
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
		Detail: []byte(`{
			"group_openid": "allowed_group",
			"author": {
				"user_openid": "admin_user"
			}
		}`),
	}

	ctx := NewContext(event, nil)
	ctx.SetPermissionManager(pm)
	ctx.SetUserID("admin_user")

	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}
