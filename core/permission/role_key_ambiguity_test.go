package permission

// role_key_ambiguity_test.go — 权限内部 key 无歧义与 Role 注册参数校验的回归测试
//
// 回归背景：
//   - Role.permissions 以 Permission.String()（"resource:action"）为 key：
//     {command:echo, execute} 与 {command, echo:execute} 得到同一个
//     "command:echo:execute"，导致误命中与误删除；
//   - RegisterRole(nil) 直接 panic；空名角色可注册但永远无法被授予
//     （AssignRole 按名字查找），等于"配了权限却全不生效"。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRole_InternalKeysAreUnambiguous(t *testing.T) {
	a := Permission{Resource: "command:echo", Action: "execute"}
	b := Permission{Resource: "command", Action: "echo:execute"}

	assert.Equal(t, a.String(), b.String(), "展示格式相同（API 兼容）")
	require.NotEqual(t, permKey(a), permKey(b), "内部 key 必须能区分二者")

	role := NewRole("r", a)
	assert.True(t, role.HasPermission(a), "已授予的权限必须命中")
	assert.False(t, role.HasPermission(b), "不同权限不得因 key 冲突误命中")

	// 误删保护：移除 a 后 b 仍然不生效，且 a 正确消失
	role.AddPermission(b)
	role.RemovePermission(a)
	assert.False(t, role.HasPermission(a))
	assert.True(t, role.HasPermission(b))

	// 全局通配仍在精确 key 之后生效
	admin := NewRole("admin", Permission{Resource: "*", Action: "*"})
	assert.True(t, admin.HasPermission(a))
}

func TestRegisterRole_RejectsNilAndEmptyName(t *testing.T) {
	pm := NewPermissionManager()
	before := len(pm.ListRoles())

	require.NotPanics(t, func() { pm.RegisterRole(nil) })
	assert.Len(t, pm.ListRoles(), before, "nil 角色不得注册")

	pm.RegisterRole(&Role{Name: ""})
	assert.Len(t, pm.ListRoles(), before, "空名角色不得注册")

	// 正常角色不受影响
	pm.RegisterRole(NewRole("ops", Permission{Resource: "command:*", Action: "execute"}))
	assert.Len(t, pm.ListRoles(), before+1)
}
