package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	permissionplugin "github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPermTestCtx(userID string, group bool) *eventctx.Context {
	kind := platform.EventKindPrivateMessage
	chat := platform.ChatInfo{ID: "chat_1"}
	if group {
		kind = platform.EventKindGroupMessage
		chat = platform.ChatInfo{ID: "group_1", IsGroup: true}
	}
	evt := platform.NewSyntheticEvent(
		kind,
		"hi",
		platform.WithSyntheticSender(platform.UserInfo{ID: userID}),
		platform.WithSyntheticChat(chat),
	)
	return eventctx.NewContextFromEvent(evt, nil)
}

// TestFilterToolsByPermission_Plugin 验证接线权限插件后按角色注入：
// 普通用户看不到管理工具，admin（通配权限）可见。
func TestFilterToolsByPermission_Plugin(t *testing.T) {
	permPlugin := permissionplugin.NewPlugin()
	require.NoError(t, permPlugin.AssignRole("admin1", "admin"))

	p := &Plugin{perms: permPlugin}
	tools := []toolkit.Tool{
		{Name: "free", Categories: []string{"general"}},
		{Name: "admin_tool", Categories: []string{"admin"}, Permissions: []string{"acl.view"}},
		{Name: catalog.SendToToolName, Categories: []string{"general"}, Permissions: []string{catalog.SendToPermission}},
	}
	actions := toolkit.ActionsOf(tools)

	// 普通用户：只剩 free
	out := p.filterToolsByPermission(newPermTestCtx("user1", false), actions)
	require.Len(t, out, 1)
	assert.Equal(t, "free", out[0].Spec.Name)

	// admin：全部可见
	out = p.filterToolsByPermission(newPermTestCtx("admin1", false), actions)
	assert.Len(t, out, 3)
}

// TestFilterToolsByPermission_ContextFallback 验证权限插件未接线时回退到
// 上下文权限管理器（测试场景）。
func TestFilterToolsByPermission_ContextFallback(t *testing.T) {
	pm := eventctx.NewPermissionManager()
	pm.GrantPermission("user1", permission.Permission{Resource: "ai.message", Action: "send"})

	ctx := newPermTestCtx("user1", false)
	ctx.SetPermissionManager(pm)

	p := &Plugin{} // perms nil
	tools := []toolkit.Tool{
		{Name: "free", Categories: []string{"general"}},
		{Name: catalog.SendToToolName, Categories: []string{"general"}, Permissions: []string{catalog.SendToPermission}},
		{Name: "admin_tool", Categories: []string{"admin"}, Permissions: []string{"acl.view"}},
	}
	out := p.filterToolsByPermission(ctx, toolkit.ActionsOf(tools))
	require.Len(t, out, 2)
	names := map[string]bool{out[0].Spec.Name: true, out[1].Spec.Name: true}
	assert.True(t, names["free"])
	assert.True(t, names[catalog.SendToToolName])
}

// TestFilterToolsByPermission_FailClosed 验证权限插件与上下文管理器皆缺失时
// 带权限工具全部被过滤（安全默认）。
func TestFilterToolsByPermission_FailClosed(t *testing.T) {
	p := &Plugin{}
	tools := []toolkit.Tool{
		{Name: "free"},
		{Name: "admin_tool", Permissions: []string{"acl.view"}},
	}
	out := p.filterToolsByPermission(newPermTestCtx("user1", false), toolkit.ActionsOf(tools))
	require.Len(t, out, 1)
	assert.Equal(t, "free", out[0].Spec.Name)
}
