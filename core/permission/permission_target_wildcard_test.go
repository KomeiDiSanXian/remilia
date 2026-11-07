package permission

// permission_target_wildcard_test.go — 请求侧通配符按字面值处理
//
// 安全约束：Permission.Match 只展开授权侧（pattern）的通配符。请求
// （target）侧的 "*" 必须按字面值比较，否则任何把用户可控字符串透传进
// HasPermission 的调用方都可能被 "*" 绕过检查。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPermission_TargetWildcardTreatedLiterally 验证普通用户不能凭 "*"
// 探测/绕过权限检查，而持有 "*:*" 的 admin 可匹配任意请求。
func TestPermission_TargetWildcardTreatedLiterally(t *testing.T) {
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
