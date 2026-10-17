package permission

// revoke_empty_entry_test.go — Revoke* 空结果条目的回归测试
//
// 回归背景：RevokeRole/RevokePermission 无条件把（可能为空的）过滤结果写回
// userRoles/userPerms。对不存在或已无角色/权限的用户调用会留下空切片条目：
// map 随调用次数无界增长，并被 ExportUserRoles/ExportUserPerms 带进持久化数据。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevokeRole_NoEmptyEntry(t *testing.T) {
	pm := NewPermissionManager()

	// 不存在的用户：不应产生任何条目
	pm.RevokeRole("ghost", "user")
	_, exists := pm.userRoles["ghost"]
	assert.False(t, exists, "不存在的用户不应产生空条目")
	assert.Empty(t, pm.ExportUserRoles()["ghost"])

	// 存在的用户：撤销最后一个角色后删除条目
	require.NoError(t, pm.AssignRole("u1", "user"))
	pm.RevokeRole("u1", "user")
	_, exists = pm.userRoles["u1"]
	assert.False(t, exists, "撤销最后一个角色后应删除条目")

	// 仍有其它角色：保留条目
	require.NoError(t, pm.AssignRole("u2", "user"))
	require.NoError(t, pm.AssignRole("u2", "admin"))
	pm.RevokeRole("u2", "admin")
	_, exists = pm.userRoles["u2"]
	assert.True(t, exists)
	assert.Equal(t, []string{"user"}, pm.GetUserRoles("u2"))
}

func TestRevokePermission_NoEmptyEntry(t *testing.T) {
	pm := NewPermissionManager()

	pm.RevokePermission("ghost", Permission{Resource: "x", Action: "y"})
	_, exists := pm.userPerms["ghost"]
	assert.False(t, exists, "不存在的用户不应产生空条目")

	perm := Permission{Resource: "x", Action: "y"}
	pm.GrantPermission("u1", perm)
	pm.RevokePermission("u1", perm)
	_, exists = pm.userPerms["u1"]
	assert.False(t, exists, "撤销最后一个权限后应删除条目")
	assert.Empty(t, pm.ExportUserPerms()["u1"])

	// 部分撤销：保留其余权限
	pm.GrantPermission("u2", perm)
	pm.GrantPermission("u2", Permission{Resource: "x", Action: "z"})
	pm.RevokePermission("u2", perm)
	assert.Len(t, pm.GetUserPermissions("u2"), 1)
}

// 反复撤销不得让映射规模增长（此前的实现每次调用都会写一个空条目）。
func TestRevoke_RepeatedCallsDoNotGrowMaps(t *testing.T) {
	pm := NewPermissionManager()

	for range 100 {
		pm.RevokeRole("ghost-role", "user")
		pm.RevokePermission("ghost-perm", Permission{Resource: "x", Action: "y"})
	}

	assert.Empty(t, pm.ExportUserRoles())
	assert.Empty(t, pm.ExportUserPerms())
}
