package permission

// permission_review_test.go — 2026-07 core 复查修复的回归测试：
// 请求（target）侧的 "*" 必须按字面值处理，不得被当作通配符放行。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReviewFix_TargetWildcardTreatedLiterally(t *testing.T) {
	pm := NewPermissionManager()
	assert.NoError(t, pm.AssignRole("u1", "user")) // user: command:*/execute, query:*/view

	// 正常查询不受影响
	assert.True(t, pm.HasPermission("u1", Permission{Resource: "command:echo", Action: "execute"}))
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "admin:panel", Action: "execute"}))

	// 请求侧的 "*" 只能按字面值处理：普通用户不得凭 "*" 探测/绕过权限检查
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "*", Action: "*"}))
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "command:echo", Action: "*"}))
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "*", Action: "execute"}))

	// admin 持有授权侧的 *:*，可以匹配任意请求（包括字面 "*"）
	assert.NoError(t, pm.AssignRole("root", "admin"))
	assert.True(t, pm.HasPermission("root", Permission{Resource: "*", Action: "*"}))
	assert.True(t, pm.HasPermission("root", Permission{Resource: "command:echo", Action: "execute"}))
}

func TestReviewFix_GrantPermissionIdempotent(t *testing.T) {
	pm := NewPermissionManager()
	pm.GrantPermission("u2", Permission{Resource: "x", Action: "y"})
	pm.GrantPermission("u2", Permission{Resource: "x", Action: "y"})
	assert.Len(t, pm.GetUserPermissions("u2"), 1)
}
