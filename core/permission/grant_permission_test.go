package permission

// grant_permission_test.go — 直授权的幂等性
//
// 语义：GrantPermission 对完全相同的 (Resource, Action) 重复授权是幂等的，
// 不得在 userPerms 里堆叠重复条目（会被 ExportUserPerms 带进持久化数据）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPermission_GrantPermissionIdempotent 验证重复直授权只保留一条记录。
func TestPermission_GrantPermissionIdempotent(t *testing.T) {
	pm := NewPermissionManager()
	pm.GrantPermission("u2", Permission{Resource: "x", Action: "y"})
	pm.GrantPermission("u2", Permission{Resource: "x", Action: "y"})
	assert.Len(t, pm.GetUserPermissions("u2"), 1)
}
