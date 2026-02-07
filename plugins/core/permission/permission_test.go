package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermission_BasicOperations(t *testing.T) {
	p := New()

	userID := "user123"

	// Initially no permissions
	assert.False(t, p.HasPermission(userID, "test.read"))

	// Grant permission
	err := p.Grant(userID, "test.read")
	require.NoError(t, err)
	assert.True(t, p.HasPermission(userID, "test.read"))

	// Revoke permission
	err = p.Revoke(userID, "test.read")
	require.NoError(t, err)
	assert.False(t, p.HasPermission(userID, "test.read"))
}

func TestPermission_Roles(t *testing.T) {
	p := New()

	userID := "user123"

	// Assign user role
	err := p.AssignRole(userID, "user")
	require.NoError(t, err)

	// Should have user permissions
	assert.True(t, p.HasPermission(userID, "command.use"))
	assert.True(t, p.HasPermission(userID, "message.send"))

	// Should not have moderator permissions
	assert.False(t, p.HasPermission(userID, "message.delete"))

	// Assign moderator role
	err = p.AssignRole(userID, "moderator")
	require.NoError(t, err)

	// Should have moderator permissions
	assert.True(t, p.HasPermission(userID, "message.delete"))
	assert.True(t, p.HasPermission(userID, "user.mute"))

	// Remove moderator role
	err = p.RemoveRole(userID, "moderator")
	require.NoError(t, err)

	// Should not have moderator permissions anymore
	assert.False(t, p.HasPermission(userID, "message.delete"))

	// But still have user permissions
	assert.True(t, p.HasPermission(userID, "command.use"))
}

func TestPermission_AdminRole(t *testing.T) {
	p := New()

	userID := "admin123"

	// Assign admin role
	err := p.AssignRole(userID, "admin")
	require.NoError(t, err)

	// Admin should have all permissions (wildcard)
	assert.True(t, p.HasPermission(userID, "any.permission"))
	assert.True(t, p.HasPermission(userID, "test.read"))
	assert.True(t, p.HasPermission(userID, "test.write"))
	assert.True(t, p.HasPermission(userID, "message.delete"))
}

func TestPermission_CustomRole(t *testing.T) {
	p := New()

	// Define custom role
	err := p.DefineRole("editor", []string{
		"post.create",
		"post.edit",
		"post.delete",
		"comment.moderate",
	})
	require.NoError(t, err)

	userID := "editor123"

	// Assign custom role
	err = p.AssignRole(userID, "editor")
	require.NoError(t, err)

	// Should have editor permissions
	assert.True(t, p.HasPermission(userID, "post.create"))
	assert.True(t, p.HasPermission(userID, "post.edit"))
	assert.True(t, p.HasPermission(userID, "comment.moderate"))

	// Should not have other permissions
	assert.False(t, p.HasPermission(userID, "user.ban"))
}

func TestPermission_WildcardPermissions(t *testing.T) {
	p := New()

	// Define role with wildcard permissions
	err := p.DefineRole("superuser", []string{
		"user.*", // All user permissions
		"post.*", // All post permissions
	})
	require.NoError(t, err)

	userID := "super123"
	err = p.AssignRole(userID, "superuser")
	require.NoError(t, err)

	// Should match wildcard
	assert.True(t, p.HasPermission(userID, "user.create"))
	assert.True(t, p.HasPermission(userID, "user.delete"))
	assert.True(t, p.HasPermission(userID, "post.publish"))

	// Should not match other prefixes
	assert.False(t, p.HasPermission(userID, "comment.delete"))
}

func TestPermission_GetUserPermissions(t *testing.T) {
	p := New()

	userID := "user123"

	// Grant direct permissions
	p.Grant(userID, "test.read")
	p.Grant(userID, "test.write")

	// Assign role
	p.AssignRole(userID, "user")

	// Get all permissions
	perms := p.GetUserPermissions(userID)

	// Should contain direct permissions and role permissions
	assert.Contains(t, perms, "test:read")
	assert.Contains(t, perms, "test:write")
	assert.Contains(t, perms, "command:use")
	assert.Contains(t, perms, "message:send")
}

func TestPermission_GetUserRoles(t *testing.T) {
	p := New()

	userID := "user123"

	// Initially no roles
	roles := p.GetUserRoles(userID)
	assert.Empty(t, roles)

	// Assign roles
	p.AssignRole(userID, "user")
	p.AssignRole(userID, "moderator")

	// Get roles
	roles = p.GetUserRoles(userID)
	assert.Len(t, roles, 2)
	assert.Contains(t, roles, "user")
	assert.Contains(t, roles, "moderator")
}

func TestPermission_ListRoles(t *testing.T) {
	p := New()

	roles := p.ListRoles()

	// Should have default roles (now includes guest)
	assert.Contains(t, roles, "admin")
	assert.Contains(t, roles, "moderator")
	assert.Contains(t, roles, "user")
	assert.Contains(t, roles, "guest")
	assert.Len(t, roles, 4)

	// Add custom role
	p.DefineRole("custom", []string{"test.permission"})

	// Note: ListRoles() is hardcoded and doesn't reflect dynamic roles
	// This is a known limitation - in real usage, you would track roles separately
	// or use the underlying manager's capabilities
	roles = p.ListRoles()
	// Custom role won't appear in the hardcoded list
	assert.Len(t, roles, 4) // Still 4 hardcoded roles
}

func TestPermission_GetRole(t *testing.T) {
	p := New()

	// Get existing role
	perms, err := p.GetRole("moderator")
	require.NoError(t, err)
	assert.Contains(t, perms, "message:delete")
	assert.Contains(t, perms, "user:mute")

	// Get non-existing role
	_, err = p.GetRole("nonexistent")
	assert.Error(t, err)
}

func TestPermission_DuplicateRoleAssignment(t *testing.T) {
	p := New()

	userID := "user123"

	// Assign role twice
	p.AssignRole(userID, "user")
	p.AssignRole(userID, "user")

	// Should only have one instance
	roles := p.GetUserRoles(userID)
	assert.Len(t, roles, 1)
}

func TestPermission_Dependencies(t *testing.T) {
	p := New()
	deps := p.Dependencies()
	assert.Empty(t, deps)
}

func BenchmarkPermission_HasPermission(b *testing.B) {
	p := New()
	userID := "user123"
	p.AssignRole(userID, "user")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.HasPermission(userID, "command.use")
	}
}

func BenchmarkPermission_Grant(b *testing.B) {
	p := New()
	userID := "user123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Grant(userID, "test.permission")
	}
}

func BenchmarkPermission_Concurrent(b *testing.B) {
	p := New()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			userID := "user123"
			if i%2 == 0 {
				p.HasPermission(userID, "command.use")
			} else {
				p.Grant(userID, "test.permission")
			}
			i++
		}
	})
}
