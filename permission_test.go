package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestPermission_Match
func TestPermission_Match(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		perm     Permission
		target   Permission
		expected bool
	}{
		{
			name:     "",
			perm:     Permission{Resource: "command:weather", Action: "execute"},
			target:   Permission{Resource: "command:weather", Action: "execute"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "*", Action: "execute"},
			target:   Permission{Resource: "command:weather", Action: "execute"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "command:weather", Action: "*"},
			target:   Permission{Resource: "command:weather", Action: "execute"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "*", Action: "*"},
			target:   Permission{Resource: "command:weather", Action: "execute"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "command:time", Action: "execute"},
			target:   Permission{Resource: "command:weather", Action: "execute"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perm.Match(tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRole_HasPermission
func TestRole_HasPermission(t *testing.T) {
	t.Parallel()
	role := NewRole("test",
		Permission{Resource: "command:*", Action: "execute"},
		Permission{Resource: "admin:users", Action: "view"},
	)

	tests := []struct {
		name     string
		perm     Permission
		expected bool
	}{
		{
			name:     "",
			perm:     Permission{Resource: "command:weather", Action: "execute"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "admin:users", Action: "view"},
			expected: true,
		},
		{
			name:     "",
			perm:     Permission{Resource: "admin:users", Action: "delete"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := role.HasPermission(tt.perm)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRole_AddRemovePermission
func TestRole_AddRemovePermission(t *testing.T) {
	t.Parallel()
	role := NewRole("test")

	perm := Permission{Resource: "test", Action: "execute"}
	role.AddPermission(perm)
	assert.True(t, role.HasPermission(perm))

	role.RemovePermission(perm)
	assert.False(t, role.HasPermission(perm))
}

// TestPermissionManager_RegisterRole
func TestPermissionManager_RegisterRole(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()

	role := NewRole("custom", Permission{Resource: "custom", Action: "test"})
	pm.RegisterRole(role)

	retrieved, ok := pm.GetRole("custom")
	assert.True(t, ok)
	assert.Equal(t, "custom", retrieved.Name)
	assert.True(t, retrieved.HasPermission(Permission{Resource: "custom", Action: "test"}))
}

// TestPermissionManager_DefaultRoles
func TestPermissionManager_DefaultRoles(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()

	admin, ok := pm.GetRole("admin")
	assert.True(t, ok)
	assert.True(t, admin.HasPermission(Permission{Resource: "anything", Action: "anything"}))

	user, ok := pm.GetRole("user")
	assert.True(t, ok)
	assert.True(t, user.HasPermission(Permission{Resource: "command:test", Action: "execute"}))

	guest, ok := pm.GetRole("guest")
	assert.True(t, ok)
	assert.True(t, guest.HasPermission(Permission{Resource: "query:test", Action: "view"}))
	assert.False(t, guest.HasPermission(Permission{Resource: "command:test", Action: "execute"}))
}

// TestPermissionManager_AssignRole
func TestPermissionManager_AssignRole(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	err := pm.AssignRole(userID, "user")
	assert.NoError(t, err)

	roles := pm.GetUserRoles(userID)
	assert.Contains(t, roles, "user")

	err = pm.AssignRole(userID, "user")
	assert.NoError(t, err)
	roles = pm.GetUserRoles(userID)
	assert.Len(t, roles, 1)

	err = pm.AssignRole(userID, "nonexistent")
	assert.Error(t, err)
}

// TestPermissionManager_RevokeRole
func TestPermissionManager_RevokeRole(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	assert.NoError(t, pm.AssignRole(userID, "user"))
	assert.NoError(t, pm.AssignRole(userID, "guest"))
	assert.Len(t, pm.GetUserRoles(userID), 2)

	pm.RevokeRole(userID, "guest")
	roles := pm.GetUserRoles(userID)
	assert.Len(t, roles, 1)
	assert.Contains(t, roles, "user")
	assert.NotContains(t, roles, "guest")
}

// TestPermissionManager_GrantPermission 娴嬭瘯鐩存帴鎺堟潈
func TestPermissionManager_GrantPermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	perm := Permission{Resource: "special", Action: "execute"}
	pm.GrantPermission(userID, perm)

	assert.True(t, pm.HasPermission(userID, perm))
}

// TestPermissionManager_RevokePermission
func TestPermissionManager_RevokePermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	perm := Permission{Resource: "special", Action: "execute"}
	pm.GrantPermission(userID, perm)
	assert.True(t, pm.HasPermission(userID, perm))

	pm.RevokePermission(userID, perm)
	assert.False(t, pm.HasPermission(userID, perm))
}

// TestPermissionManager_HasPermission
func TestPermissionManager_HasPermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	pm.AssignRole(userID, "user")
	assert.True(t, pm.HasPermission(userID, Permission{Resource: "command:test", Action: "execute"}))

	pm.GrantPermission(userID, Permission{Resource: "special", Action: "execute"})
	assert.True(t, pm.HasPermission(userID, Permission{Resource: "special", Action: "execute"}))

	assert.False(t, pm.HasPermission(userID, Permission{Resource: "admin:delete", Action: "execute"}))
}

// TestPermissionManager_GetUserPermissions
func TestPermissionManager_GetUserPermissions(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"

	pm.AssignRole(userID, "guest")
	pm.GrantPermission(userID, Permission{Resource: "special", Action: "execute"})

	perms := pm.GetUserPermissions(userID)
	assert.GreaterOrEqual(t, len(perms), 2)
}

// TestContext_HasPermission
func TestContext_HasPermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "user")

	ctx := NewContext(&dto.Payload{}, nil)

	ctx.SetPermissionManager(pm)
	ctx.SetUserID(userID)

	assert.True(t, CheckPermission(ctx, "command:test", "execute"))
	assert.False(t, CheckPermission(ctx, "admin:delete", "execute"))
}

// TestContext_RequirePermission
func TestContext_RequirePermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "user")

	ctx := NewContext(&dto.Payload{}, nil)

	ctx.SetPermissionManager(pm)
	ctx.SetUserID(userID)

	err := EnsurePermission(ctx, "command:test", "execute")
	assert.NoError(t, err)

	err = EnsurePermission(ctx, "admin:delete", "execute")
	assert.Error(t, err)
	assert.Equal(t, ErrPermissionDenied, err)
}

// TestRequirePermissionMiddleware
func TestRequirePermissionMiddleware(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "user")

	engine := NewEngine()
	engine.Use(RequirePermissionMiddleware(pm))

	executed := false
	engine.OnC2C().Use(RequirePermission("command:test", "execute")).HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	ctx.SetUserID(userID)

	engine.ProcessEvent(ctx)

	assert.True(t, executed)
}

// TestRequireRole
func TestRequireRole(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "admin")

	engine := NewEngine()
	engine.Use(RequirePermissionMiddleware(pm))

	executed := false
	engine.OnC2C().Use(RequireRole("admin")).HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	ctx.SetUserID(userID)

	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

// TestRequireAnyPermission
func TestRequireAnyPermission(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "user")

	engine := NewEngine()
	engine.Use(RequirePermissionMiddleware(pm))

	executed := false
	engine.OnC2C().Use(RequireAnyPermission(
		Permission{Resource: "admin:*", Action: "*"},
		Permission{Resource: "command:*", Action: "execute"},
	)).HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	ctx.SetUserID(userID)

	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

// TestRequireAllPermissions
func TestRequireAllPermissions(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.GrantPermission(userID, Permission{Resource: "data", Action: "read"})
	pm.GrantPermission(userID, Permission{Resource: "data", Action: "write"})

	engine := NewEngine()
	engine.Use(RequirePermissionMiddleware(pm))

	executed := false
	engine.OnC2C().Use(RequireAllPermissions(
		Permission{Resource: "data", Action: "read"},
		Permission{Resource: "data", Action: "write"},
	)).HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	ctx.SetUserID(userID)

	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

// TestPermissionDenied
func TestPermissionDenied(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()
	userID := "user123"
	pm.AssignRole(userID, "guest")

	engine := NewEngine()
	engine.Use(RequirePermissionMiddleware(pm))

	executed := false
	var capturedError error

	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			err := next(ctx)
			if err != nil {
				capturedError = err
			}
			return err
		}
	})

	engine.OnC2C().Use(RequirePermission("command:test", "execute")).HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	ctx.SetUserID(userID)

	engine.ProcessEvent(ctx)

	assert.False(t, executed)
	assert.Error(t, capturedError)

	assert.Contains(t, capturedError.Error(), "permission denied")
}

// MockPermissionProvider
type MockPermissionProvider struct {
	userRoles map[string][]string
	userPerms map[string][]Permission
}

func (m *MockPermissionProvider) GetUserRoles(userID string) ([]string, error) {
	if roles, ok := m.userRoles[userID]; ok {
		return roles, nil
	}
	return nil, nil
}

func (m *MockPermissionProvider) GetUserPermissions(userID string) ([]Permission, error) {
	if perms, ok := m.userPerms[userID]; ok {
		return perms, nil
	}
	return nil, nil
}

// TestPermissionProvider
func TestPermissionProvider(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()

	provider := &MockPermissionProvider{
		userRoles: map[string][]string{
			"user123": {"admin"},
		},
		userPerms: map[string][]Permission{
			"user456": {
				{Resource: "external", Action: "execute"},
			},
		},
	}
	pm.SetPermissionProvider(provider)

	assert.True(t, pm.HasPermission("user123", Permission{Resource: "anything", Action: "anything"}))

	assert.True(t, pm.HasPermission("user456", Permission{Resource: "external", Action: "execute"}))
}

func TestPermissionManager_Concurrent(t *testing.T) {
	t.Parallel()
	pm := NewPermissionManager()

	done := make(chan bool, 100)

	for i := 0; i < 50; i++ {
		go func(id int) {
			userID := string(rune('A' + id%26))
			pm.AssignRole(userID, "user")
			pm.GrantPermission(userID, Permission{Resource: "test", Action: "execute"})
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		go func(id int) {
			userID := string(rune('A' + id%26))
			pm.HasPermission(userID, Permission{Resource: "test", Action: "execute"})
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

func BenchmarkPermission_Match(b *testing.B) {
	perm := Permission{Resource: "command:*", Action: "execute"}
	target := Permission{Resource: "command:weather", Action: "execute"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perm.Match(target)
	}
}

func BenchmarkPermissionManager_HasPermission(b *testing.B) {
	pm := NewPermissionManager()
	pm.AssignRole("user123", "user")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.HasPermission("user123", Permission{Resource: "command:test", Action: "execute"})
	}
}
