package permission

// assign_role_checked_test.go — AssignRoleChecked 的等级约束（提权防护）
//
// 回归背景：等级检查在 RLock 下读取、写入在释放锁之后，两次调用之间
// 角色等级/归属可能被并发修改，提权防护存在窗口期（TOCTOU）。

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPermission_AssignRoleCheckedSerialized 验证并发 AssignRoleChecked 下
// 等级约束始终成立（检查与写入在同一写锁内完成）。
func TestPermission_AssignRoleCheckedSerialized(t *testing.T) {
	pm := NewPermissionManager()
	pm.RegisterRole(&Role{Name: "admin", Level: 100, permissions: map[string]Permission{
		permKey(Permission{Resource: "*", Action: "*"}): {Resource: "*", Action: "*"},
	}})
	pm.RegisterRole(&Role{Name: "moderator", Level: 10})
	pm.RegisterRole(&Role{Name: "superadmin", Level: 200})

	require.NoError(t, pm.AssignRole("actor", "admin"))
	require.NoError(t, pm.AssignRole("actor", "moderator"))

	var wg sync.WaitGroup
	wg.Add(3)
	// 反复尝试越权授予 superadmin
	go func() {
		defer wg.Done()
		for range 200 {
			_ = pm.AssignRoleChecked("actor", "victim", "superadmin")
		}
	}()
	// 反复尝试把高等级用户降级
	go func() {
		defer wg.Done()
		for range 200 {
			_ = pm.AssignRoleChecked("actor", "chief", "moderator")
		}
	}()
	// 并发改写角色等级与用户归属
	go func() {
		defer wg.Done()
		for range 200 {
			pm.RegisterRole(&Role{Name: "moderator", Level: 10})
			_ = pm.AssignRole("chief", "superadmin")
		}
	}()
	wg.Wait()

	assert.Zero(t, pm.UserLevel("victim"), "低等级调用方不得授予高等级角色")
	assert.Equal(t, 200, pm.UserLevel("chief"), "不得修改高于自己的用户")

	// 正常路径仍然工作
	require.NoError(t, pm.AssignRoleChecked("actor", "someone", "moderator"))
	assert.Equal(t, 10, pm.UserLevel("someone"))
}
