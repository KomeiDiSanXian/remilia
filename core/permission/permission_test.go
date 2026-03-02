package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMatchWithWildcard_EdgeCases tests the unexported matchWithWildcard helper
// (originally TestBug_PermissionWildcardEdgeCases in core/context).
func TestMatchWithWildcard_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		{"empty pattern", "", "test", false},
		{"empty value", "test", "", false},
		{"both empty", "", "", false},
		{"prefix without colon", "test*", "test:sub", false},
		{"exact colon match", "test:", "test:", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWithWildcard(tt.pattern, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPermission_Match(t *testing.T) {
	tests := []struct {
		name     string
		perm     Permission
		target   Permission
		expected bool
	}{
		{
			name:     "exact match",
			perm:     Permission{Resource: "command", Action: "execute"},
			target:   Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "wildcard all",
			perm:     Permission{Resource: "*", Action: "*"},
			target:   Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "prefix wildcard resource",
			perm:     Permission{Resource: "command:*", Action: "execute"},
			target:   Permission{Resource: "command:test", Action: "execute"},
			expected: true,
		},
		{
			name:     "wildcard action",
			perm:     Permission{Resource: "command", Action: "*"},
			target:   Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "no match",
			perm:     Permission{Resource: "command", Action: "execute"},
			target:   Permission{Resource: "query", Action: "view"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.perm.Match(tt.target))
		})
	}
}

func TestPermissionManager_Basic(t *testing.T) {
	t.Run("NewPermissionManager creates default roles", func(t *testing.T) {
		pm := NewPermissionManager()
		_, hasAdmin := pm.GetRole("admin")
		_, hasUser := pm.GetRole("user")
		_, hasGuest := pm.GetRole("guest")
		assert.True(t, hasAdmin)
		assert.True(t, hasUser)
		assert.True(t, hasGuest)
	})

	t.Run("AssignRole and HasPermission", func(t *testing.T) {
		pm := NewPermissionManager()
		assert.NoError(t, pm.AssignRole("u1", "admin"))
		assert.True(t, pm.HasPermission("u1", Permission{Resource: "anything", Action: "do"}))
	})

	t.Run("GrantPermission direct", func(t *testing.T) {
		pm := NewPermissionManager()
		perm := Permission{Resource: "cmd", Action: "execute"}
		pm.GrantPermission("u2", perm)
		assert.True(t, pm.HasPermission("u2", perm))
	})

	t.Run("RevokeRole", func(t *testing.T) {
		pm := NewPermissionManager()
		_ = pm.AssignRole("u3", "admin")
		pm.RevokeRole("u3", "admin")
		roles := pm.GetUserRoles("u3")
		assert.Empty(t, roles)
	})

	t.Run("ExportImport UserRoles", func(t *testing.T) {
		pm := NewPermissionManager()
		_ = pm.AssignRole("u4", "user")
		exported := pm.ExportUserRoles()
		assert.Contains(t, exported["u4"], "user")

		pm2 := NewPermissionManager()
		pm2.LoadUserRoles(exported)
		assert.True(t, pm2.HasPermission("u4", Permission{Resource: "command:weather", Action: "execute"}))
	})
}

func TestRole_AddRemovePermission(t *testing.T) {
	role := NewRole("test")
	perm := Permission{Resource: "cmd", Action: "execute"}
	other := Permission{Resource: "other", Action: "execute"}

	role.AddPermission(perm)
	assert.True(t, role.HasPermission(perm))

	role.RemovePermission(perm)
	assert.False(t, role.HasPermission(perm))
	assert.False(t, role.HasPermission(other))
}

func BenchmarkPermissionMatch(b *testing.B) {
	perm := Permission{Resource: "command:*", Action: "*"}
	target := Permission{Resource: "command:test", Action: "execute"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perm.Match(target)
	}
}

func BenchmarkPermissionManager_HasPermission(b *testing.B) {
	pm := NewPermissionManager()
	_ = pm.AssignRole("user1", "admin")
	perm := Permission{Resource: "test", Action: "execute"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.HasPermission("user1", perm)
	}
}
