package permission

// wildcard_boundary_test.go — 前缀通配的命名空间边界回归测试
//
// 回归背景：matchWithWildcard 对 "prefix:*" 只做裸前缀比较（不要求其后是
// ":" 或结束），授权范围被意外放大："command:*" 命中 "commander"，
// "admin:*" 命中 "administrator"。权限判定是鉴权的最后一跳，边界错误即越权。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchWithWildcard_PrefixBoundary(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"command:*", "command:echo", true},
		{"command:*", "command", true},    // 命名空间根本身
		{"command:*", "commander", false}, // 旧实现误命中
		{"command:*", "commands:run", false},
		{"admin:*", "admin:panel", true},
		{"admin:*", "administrator", false}, // 旧实现误命中（越权）
		{"admin:*", "adminX", false},
		{"a:*", "a:b", true},
		{"command:*", "command:echo:extra", true}, // 子命名空间仍属于该前缀
		{"*", "anything", true},
		{"command:echo", "command:echo", true},
		{"command:echo", "command:echo2", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, matchWithWildcard(c.pattern, c.value),
			"matchWithWildcard(%q, %q)", c.pattern, c.value)
	}
}

func TestRole_HasPermission_PrefixWildcardBoundary(t *testing.T) {
	role := NewRole("ops", Permission{Resource: "admin:*", Action: "*"})

	assert.True(t, role.HasPermission(Permission{Resource: "admin:panel", Action: "view"}))
	assert.False(t, role.HasPermission(Permission{Resource: "administrator", Action: "view"}),
		"admin:* 不得放行 administrator")

	// 直授权路径同样受限
	pm := NewPermissionManager()
	pm.GrantPermission("u1", Permission{Resource: "admin:*", Action: "*"})
	assert.True(t, pm.HasPermission("u1", Permission{Resource: "admin:users", Action: "list"}))
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "administrator", Action: "list"}))
}

func TestManager_DefaultUserRole_CommandPrefixBoundary(t *testing.T) {
	pm := NewPermissionManager()
	require.NoError(t, pm.AssignRole("u1", "user")) // user: command:*/execute, query:*/view

	assert.True(t, pm.HasPermission("u1", Permission{Resource: "command:echo", Action: "execute"}))
	assert.False(t, pm.HasPermission("u1", Permission{Resource: "commander", Action: "execute"}),
		"command:* 不得放行 commander")
}
