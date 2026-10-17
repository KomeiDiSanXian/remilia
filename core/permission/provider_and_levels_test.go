package permission

// provider_and_levels_test.go — 外部权限源故障可见性与角色等级的回归测试
//
// 回归背景：
//   - Provider 错误被静默吞掉（`if err == nil`），下游故障表现为"全员无权限"
//     且没有日志；两个调用都发出去时，provider 不可用会让每次拒绝付出两次
//     外部请求；
//   - 内核没有等级原语，AssignRole 不做任何限制，"谁能给谁授权"由各命令层
//     重复实现（漏一处即提权）。

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingProvider 记录调用次数并始终失败。
type failingProvider struct {
	permCalls atomic.Int32
	roleCalls atomic.Int32
}

func (p *failingProvider) GetUserPermissions(string) ([]Permission, error) {
	p.permCalls.Add(1)
	return nil, errors.New("provider down")
}

func (p *failingProvider) GetUserRoles(string) ([]string, error) {
	p.roleCalls.Add(1)
	return nil, errors.New("provider down")
}

func TestHasPermission_ProviderFailureIsVisibleAndNotDoubled(t *testing.T) {
	pm := NewPermissionManager()
	provider := &failingProvider{}
	pm.SetPermissionProvider(provider)

	var reported atomic.Int32
	pm.SetProviderErrorHandler(func(userID string, err error) {
		assert.Equal(t, "u1", userID)
		assert.Error(t, err)
		reported.Add(1)
	})

	// 默认权限不命中 → 走到 provider 路径
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "admin:panel", Action: "view"}))

	assert.Equal(t, int32(1), reported.Load(), "provider 故障必须通过回调暴露")
	assert.Equal(t, int32(1), provider.permCalls.Load())
	assert.Equal(t, int32(0), provider.roleCalls.Load(),
		"首个调用失败后不应继续发起第二次外部请求")

	// 再查一次：仍然只调用第一个方法（每次拒绝最多一次外部调用）
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "admin:panel", Action: "view"}))
	assert.Equal(t, int32(2), provider.permCalls.Load())
	assert.Equal(t, int32(0), provider.roleCalls.Load())
}

func TestManager_RoleLevelsAndCheckedAssign(t *testing.T) {
	pm := NewPermissionManager()
	moderator := NewRole("moderator", Permission{Resource: "command:*", Action: "execute"})
	moderator.Level = 10
	pm.RegisterRole(moderator)
	pm.RegisterRole(&Role{Name: "superadmin", Level: 100})

	require.NoError(t, pm.AssignRole("admin-user", "superadmin"))
	require.NoError(t, pm.AssignRole("mod-user", "moderator"))
	assert.Equal(t, 100, pm.UserLevel("admin-user"))
	assert.Equal(t, 10, pm.UserLevel("mod-user"))

	// 高等级可授予低等级
	require.NoError(t, pm.AssignRoleChecked("admin-user", "someone", "moderator"))
	assert.Equal(t, 10, pm.UserLevel("someone"))

	// 低等级不能授予高于自己的角色（提权防护）
	err := pm.AssignRoleChecked("mod-user", "other", "superadmin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "above actor level")

	// 不能改动与自己同级或更高的用户
	require.Error(t, pm.AssignRoleChecked("mod-user", "admin-user", "moderator"))

	// 不能借"授予低等级角色"给高等级用户降级
	require.Error(t, pm.AssignRoleChecked("admin-user", "admin-user", "moderator"))

	// 等级判定原语
	assert.True(t, pm.HasLevelAtLeast("admin-user", 100))
	assert.False(t, pm.HasLevelAtLeast("mod-user", 100))
	assert.False(t, pm.HasLevelAtLeast("nobody", 1))
}
