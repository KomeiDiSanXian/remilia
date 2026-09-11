package remilia

// bot_permission_test.go — 固定"Bot 把 RBAC 权限管理器写入事件 Context"这一接线。
//
// ctx.GetPermissionManager() 是 core/context 的权限规则（OnHasRole /
// OnHasPermission）、middleware/auth 的权限中间件（RequireRole 等）以及插件内
// RBAC 判定（如 AI 插件的 isAdmin / isSuperAdmin）唯一的角色数据来源。
// 缺少注入时它们一律按"权限系统未初始化"处理——规则恒不命中、中间件
// fail-closed 拒绝、插件内角色判定恒为 false，表现为超管也被判为无权。

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSuperAdminManager 构造一个带 superadmin 角色（*:* 通配）的权限管理器。
//
// 与 permission 插件一致：superadmin 是插件注册的预置角色，不是
// NewPermissionManager 的默认角色，因此测试中显式注册。
func newSuperAdminManager(t *testing.T) *permission.Manager {
	t.Helper()
	pm := permission.NewPermissionManager()
	pm.RegisterRole(permission.NewRole("superadmin", permission.Permission{Resource: "*", Action: "*"}))
	require.NoError(t, pm.AssignRole("u1", "superadmin"))
	return pm
}

// dispatchOnce 派发一条私聊事件并返回 handler 观察到的权限管理器。
func dispatchOnce(t *testing.T, bot *Bot, eng *engine.Engine) *permission.Manager {
	t.Helper()

	got := make(chan *permission.Manager, 1)
	eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *eventctx.Context) error {
		got <- ctx.GetPermissionManager()
		return nil
	})

	bot.handlePlatformEvent(platform.NewSyntheticEvent(
		platform.EventKindPrivateMessage, "hi",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
	))

	select {
	case m := <-got:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("handler 未被调用：事件未派发到 Engine")
		return nil
	}
}

// TestBot_InjectsPermissionManager 接线后 handler 必须能通过
// ctx.GetPermissionManager() 拿到同一个管理器与其中的角色。
func TestBot_InjectsPermissionManager(t *testing.T) {
	eng := engine.NewEngine()
	bot := MustNewBot(nil, eng)

	pm := newSuperAdminManager(t)
	bot.UsePermissionManager(pm)

	got := dispatchOnce(t, bot, eng)
	require.NotNil(t, got, "handler 应看到注入的权限管理器")
	assert.Same(t, pm, got, "handler 拿到的应是 Bot 注入的同一实例")
	assert.Contains(t, got.GetUserRoles("u1"), "superadmin")
	assert.True(t, got.HasPermission("u1", permission.Permission{Resource: "*", Action: "*"}))
}

// TestBot_WithoutPermissionManager 未接线时 ctx.GetPermissionManager() 返回
// nil（调用方按 fail-closed 处理），不得 panic。
func TestBot_WithoutPermissionManager(t *testing.T) {
	eng := engine.NewEngine()
	bot := MustNewBot(nil, eng)

	assert.Nil(t, dispatchOnce(t, bot, eng),
		"未注入权限管理器时 ctx.GetPermissionManager() 应为 nil")
}
