package context

import (
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Command Helper Tests
// ============================================================================

func TestOnCommandMatch(t *testing.T) {
	parser := command.NewParser("/")
	parser.Register(&command.Definition{
		Name: "test",
	})

	t.Run("match success", func(t *testing.T) {
		ctx := createTestContextWithMessage("/test arg1")
		rule := OnCommandMatch(parser)

		result := rule(ctx)
		assert.True(t, result)

		// Verify parsed command is stored
		parsed := ctx.GetParsedCommand()
		require.NotNil(t, parsed)
	})

	t.Run("match failure", func(t *testing.T) {
		ctx := createTestContextWithMessage("not a command")
		rule := OnCommandMatch(parser)

		result := rule(ctx)
		assert.False(t, result)
	})
}

func TestExecuteCommandDefinition(t *testing.T) {
	t.Run("execute with handler", func(t *testing.T) {
		executed := false
		handler := func(ctx any) {
			executed = true
		}

		parser := command.NewParser("/")
		parser.Register(&command.Definition{
			Name:    "test",
			Handler: handler,
		})

		ctx := createTestContextWithMessage("/test")
		ctx.MatchCommand(parser)

		err := ExecuteCommandDefinition(ctx)
		assert.NoError(t, err)
		assert.True(t, executed)
	})

	t.Run("no parsed command", func(t *testing.T) {
		ctx := newTestCtx()

		err := ExecuteCommandDefinition(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no command parsed")
	})

	t.Run("no handler", func(t *testing.T) {
		parser := command.NewParser("/")
		parser.Register(&command.Definition{
			Name: "test",
		})

		ctx := createTestContextWithMessage("/test")
		ctx.MatchCommand(parser)

		err := ExecuteCommandDefinition(ctx)
		assert.NoError(t, err) // No error even without handler
	})
}

// ============================================================================
// Command Extension Tests
// ============================================================================

func TestWithCommand(t *testing.T) {
	ctx := newTestCtx()
	ext := WithCommand(ctx)

	assert.NotNil(t, ext)
	assert.Equal(t, ctx, ext.ctx)
}

func TestCommandExtension_ParseCommand(t *testing.T) {
	t.Run("parse success", func(t *testing.T) {
		ctx := createTestContextWithMessage("/test arg1 arg2")
		ext := WithCommand(ctx)

		args, err := ext.ParseCommand()
		assert.NoError(t, err)
		require.NotNil(t, args)
		assert.Equal(t, "/test", args.Command)
		assert.Equal(t, 2, len(args.Positional))
	})

	t.Run("parse empty message", func(t *testing.T) {
		ctx := createTestContextWithMessage("")
		ext := WithCommand(ctx)

		args, err := ext.ParseCommand()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty message content")
		assert.Nil(t, args)
	})

	t.Run("cached result", func(t *testing.T) {
		ctx := createTestContextWithMessage("/test arg1")
		ext := WithCommand(ctx)

		// First call
		args1, err1 := ext.ParseCommand()
		assert.NoError(t, err1)

		// Second call should return cached result
		args2, err2 := ext.ParseCommand()
		assert.NoError(t, err2)
		assert.Equal(t, args1, args2) // Same pointer
	})

	t.Run("nil context", func(t *testing.T) {
		ext := CommandExtension{ctx: nil}

		args, err := ext.ParseCommand()
		assert.NoError(t, err)
		assert.Nil(t, args)
	})
}

func TestParseCommand_Functional(t *testing.T) {
	ctx := createTestContextWithMessage("/test arg1")

	args, err := ParseCommand(ctx)
	assert.NoError(t, err)
	require.NotNil(t, args)
}

// ============================================================================
// Permission Tests
// ============================================================================

func TestPermission_String(t *testing.T) {
	perm := permission.Permission{Resource: "command", Action: "execute"}
	assert.Equal(t, "command:execute", perm.String())
}

func TestPermission_Match(t *testing.T) {
	tests := []struct {
		name     string
		perm     permission.Permission
		target   permission.Permission
		expected bool
	}{
		{
			name:     "exact match",
			perm:     permission.Permission{Resource: "command", Action: "execute"},
			target:   permission.Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "wildcard all",
			perm:     permission.Permission{Resource: "*", Action: "*"},
			target:   permission.Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "resource prefix wildcard",
			perm:     permission.Permission{Resource: "command:*", Action: "execute"},
			target:   permission.Permission{Resource: "command:test", Action: "execute"},
			expected: true,
		},
		{
			name:     "action wildcard",
			perm:     permission.Permission{Resource: "command", Action: "*"},
			target:   permission.Permission{Resource: "command", Action: "execute"},
			expected: true,
		},
		{
			name:     "no match",
			perm:     permission.Permission{Resource: "command", Action: "execute"},
			target:   permission.Permission{Resource: "query", Action: "view"},
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

func TestRole(t *testing.T) {
	t.Run("NewRole", func(t *testing.T) {
		perm1 := permission.Permission{Resource: "cmd1", Action: "execute"}
		perm2 := permission.Permission{Resource: "cmd2", Action: "execute"}

		role := NewRole("test-role", perm1, perm2)

		assert.Equal(t, "test-role", role.Name)
		assert.Equal(t, 2, len(role.Permissions()))
	})

	t.Run("AddPermission", func(t *testing.T) {
		role := NewRole("test")
		perm := permission.Permission{Resource: "cmd", Action: "execute"}

		role.AddPermission(perm)

		assert.Equal(t, 1, len(role.Permissions()))
		assert.True(t, role.HasPermission(perm))
	})

	t.Run("RemovePermission", func(t *testing.T) {
		perm := permission.Permission{Resource: "cmd", Action: "execute"}
		role := NewRole("test", perm)

		role.RemovePermission(perm)

		assert.Equal(t, 0, len(role.Permissions()))
		assert.False(t, role.HasPermission(perm))
	})

	t.Run("HasPermission", func(t *testing.T) {
		perm := permission.Permission{Resource: "cmd", Action: "execute"}
		role := NewRole("test", perm)

		assert.True(t, role.HasPermission(perm))

		other := permission.Permission{Resource: "other", Action: "execute"}
		assert.False(t, role.HasPermission(other))
	})
}

func TestPermissionManager(t *testing.T) {
	t.Run("NewPermissionManager", func(t *testing.T) {
		pm := NewPermissionManager()

		require.NotNil(t, pm)
		// Should have default roles
		_, ok := pm.GetRole("admin")
		assert.True(t, ok)
	})

	t.Run("RegisterRole and GetRole", func(t *testing.T) {
		pm := NewPermissionManager()
		role := NewRole("custom", permission.Permission{Resource: "test", Action: "execute"})

		pm.RegisterRole(role)

		retrieved, ok := pm.GetRole("custom")
		assert.True(t, ok)
		assert.Equal(t, role.Name, retrieved.Name)
	})

	t.Run("AssignRole", func(t *testing.T) {
		pm := NewPermissionManager()

		err := pm.AssignRole("user1", "admin")
		assert.NoError(t, err)

		roles := pm.GetUserRoles("user1")
		assert.Equal(t, 1, len(roles))
		assert.Equal(t, "admin", roles[0])
	})

	t.Run("AssignRole duplicate", func(t *testing.T) {
		pm := NewPermissionManager()

		_ = pm.AssignRole("user1", "admin")
		err := pm.AssignRole("user1", "admin")
		assert.NoError(t, err) // Should not error

		roles := pm.GetUserRoles("user1")
		assert.Equal(t, 1, len(roles)) // Should not duplicate
	})

	t.Run("AssignRole nonexistent", func(t *testing.T) {
		pm := NewPermissionManager()

		err := pm.AssignRole("user1", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "role not found")
	})

	t.Run("RevokeRole", func(t *testing.T) {
		pm := NewPermissionManager()
		_ = pm.AssignRole("user1", "admin")

		pm.RevokeRole("user1", "admin")

		roles := pm.GetUserRoles("user1")
		assert.Equal(t, 0, len(roles))
	})

	t.Run("GrantPermission and RevokePermission", func(t *testing.T) {
		pm := NewPermissionManager()
		perm := permission.Permission{Resource: "cmd", Action: "execute"}

		pm.GrantPermission("user1", perm)
		assert.True(t, pm.HasPermission("user1", perm))

		pm.RevokePermission("user1", perm)
		assert.False(t, pm.HasPermission("user1", perm))
	})

	t.Run("HasPermission via role", func(t *testing.T) {
		pm := NewPermissionManager()
		_ = pm.AssignRole("user1", "admin")

		// Admin has wildcard permission
		perm := permission.Permission{Resource: "anything", Action: "do"}
		assert.True(t, pm.HasPermission("user1", perm))
	})

	t.Run("GetUserPermissions", func(t *testing.T) {
		pm := NewPermissionManager()
		directPerm := permission.Permission{Resource: "direct", Action: "execute"}

		pm.GrantPermission("user1", directPerm)
		_ = pm.AssignRole("user1", "user")

		perms := pm.GetUserPermissions("user1")
		assert.Greater(t, len(perms), 0)
	})
}

// Mock PermissionProvider
type mockPermissionProvider struct {
	roles []string
	perms []permission.Permission
	err   error
}

func (m *mockPermissionProvider) GetUserRoles(_ string) ([]string, error) {
	return m.roles, m.err
}

func (m *mockPermissionProvider) GetUserPermissions(_ string) ([]permission.Permission, error) {
	return m.perms, m.err
}

func TestPermissionManager_WithProvider(t *testing.T) {
	t.Run("SetPermissionProvider", func(t *testing.T) {
		pm := NewPermissionManager()
		provider := &mockPermissionProvider{
			perms: []permission.Permission{{Resource: "external", Action: "execute"}},
		}

		pm.SetPermissionProvider(provider)

		assert.True(t, pm.HasPermission("user1", permission.Permission{Resource: "external", Action: "execute"}))
	})

	t.Run("provider with roles", func(t *testing.T) {
		pm := NewPermissionManager()
		provider := &mockPermissionProvider{
			roles: []string{"admin"},
		}

		pm.SetPermissionProvider(provider)

		// Admin role has wildcard permission
		assert.True(t, pm.HasPermission("user1", permission.Permission{Resource: "anything", Action: "do"}))
	})
}

// ============================================================================
// Context Permission Methods Tests
// ============================================================================

func TestContext_PermissionMethods(t *testing.T) {
	t.Run("SetPermissionManager and GetPermissionManager", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()

		ctx.SetPermissionManager(pm)

		retrieved := ctx.GetPermissionManager()
		assert.Equal(t, pm, retrieved)
	})

	t.Run("GetUserID and SetUserID", func(t *testing.T) {
		ctx := newTestCtx()

		ctx.SetUserID("user123")

		userID := ctx.GetUserID()
		assert.Equal(t, "user123", userID)
	})
}

// ============================================================================
// Convenience Rules Tests
// ============================================================================

func TestOnHasPermission(t *testing.T) {
	t.Run("has permission", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()
		pm.GrantPermission("user1", permission.Permission{Resource: "cmd", Action: "execute"})

		ctx.SetPermissionManager(pm)
		ctx.SetUserID("user1")

		rule := OnHasPermission("cmd", "execute")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no permission", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()

		ctx.SetPermissionManager(pm)
		ctx.SetUserID("user1")

		rule := OnHasPermission("cmd", "execute")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("no permission manager", func(t *testing.T) {
		ctx := newTestCtx()

		rule := OnHasPermission("cmd", "execute")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("no user ID", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()
		ctx.SetPermissionManager(pm)

		rule := OnHasPermission("cmd", "execute")
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnHasRole(t *testing.T) {
	t.Run("has role", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()
		_ = pm.AssignRole("user1", "admin")

		ctx.SetPermissionManager(pm)
		ctx.SetUserID("user1")

		rule := OnHasRole("admin")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no role", func(t *testing.T) {
		ctx := newTestCtx()
		pm := NewPermissionManager()

		ctx.SetPermissionManager(pm)
		ctx.SetUserID("user1")

		rule := OnHasRole("admin")
		result := rule(ctx)

		assert.False(t, result)
	})
}

// ============================================================================
// Advanced Rules Tests
// ============================================================================

func TestOnRegexCompiled(t *testing.T) {
	re := regexp.MustCompile(`\d+`)
	ctx := createTestContextWithMessage("test123")

	rule := OnRegexCompiled(re)
	result := rule(ctx)

	assert.True(t, result)
}

func TestAnd(t *testing.T) {
	t.Run("all true", func(t *testing.T) {
		ctx := newTestCtx()

		rule := And(
			func(ctx *Context) bool { return true },
			func(ctx *Context) bool { return true },
		)

		assert.True(t, rule(ctx))
	})

	t.Run("one false", func(t *testing.T) {
		ctx := newTestCtx()

		rule := And(
			func(ctx *Context) bool { return true },
			func(ctx *Context) bool { return false },
		)

		assert.False(t, rule(ctx))
	})

	t.Run("short circuit", func(t *testing.T) {
		ctx := newTestCtx()
		called := false

		rule := And(
			func(ctx *Context) bool { return false },
			func(ctx *Context) bool {
				called = true
				return true
			},
		)

		assert.False(t, rule(ctx))
		assert.False(t, called) // Should not be called
	})
}

func TestOr(t *testing.T) {
	t.Run("all false", func(t *testing.T) {
		ctx := newTestCtx()

		rule := Or(
			func(ctx *Context) bool { return false },
			func(ctx *Context) bool { return false },
		)

		assert.False(t, rule(ctx))
	})

	t.Run("one true", func(t *testing.T) {
		ctx := newTestCtx()

		rule := Or(
			func(ctx *Context) bool { return false },
			func(ctx *Context) bool { return true },
		)

		assert.True(t, rule(ctx))
	})

	t.Run("short circuit", func(t *testing.T) {
		ctx := newTestCtx()
		called := false

		rule := Or(
			func(ctx *Context) bool { return true },
			func(ctx *Context) bool {
				called = true
				return false
			},
		)

		assert.True(t, rule(ctx))
		assert.False(t, called) // Should not be called
	})
}

func TestNot(t *testing.T) {
	t.Run("invert true", func(t *testing.T) {
		ctx := newTestCtx()

		rule := Not(func(ctx *Context) bool { return true })

		assert.False(t, rule(ctx))
	})

	t.Run("invert false", func(t *testing.T) {
		ctx := newTestCtx()

		rule := Not(func(ctx *Context) bool { return false })

		assert.True(t, rule(ctx))
	})
}

func TestWithTimeout(t *testing.T) {
	t.Run("fast rule", func(t *testing.T) {
		ctx := newTestCtx()

		rule := WithTimeout(
			func(ctx *Context) bool { return true },
			100*time.Millisecond,
		)

		result := rule(ctx)
		assert.True(t, result)
	})

	t.Run("timeout exceeded", func(t *testing.T) {
		ctx := newTestCtx()

		rule := WithTimeout(
			func(ctx *Context) bool {
				time.Sleep(200 * time.Millisecond)
				return true
			},
			50*time.Millisecond,
		)

		result := rule(ctx)
		assert.False(t, result) // Should timeout
	})

	t.Run("panic recovery", func(t *testing.T) {
		ctx := newTestCtx()

		rule := WithTimeout(
			func(ctx *Context) bool {
				panic("test panic")
			},
			100*time.Millisecond,
		)

		result := rule(ctx)
		assert.False(t, result) // Should return false on panic
	})
}

func TestMonitorRule(t *testing.T) {
	t.Run("fast rule", func(t *testing.T) {
		ctx := newTestCtx()

		rule := MonitorRule("fast", func(ctx *Context) bool {
			return true
		}, 100*time.Millisecond)

		result := rule(ctx)
		assert.True(t, result)
	})

	t.Run("slow rule logs warning", func(t *testing.T) {
		ctx := newTestCtx()

		rule := MonitorRule("slow", func(ctx *Context) bool {
			time.Sleep(20 * time.Millisecond)
			return true
		}, 10*time.Millisecond)

		result := rule(ctx)
		assert.True(t, result) // Still returns result
	})
}

// ============================================================================
// Regex Cache Management Tests
// ============================================================================

func TestRegexCacheManagement(t *testing.T) {
	t.Run("ClearRegexCache", func(t *testing.T) {
		// Add some patterns
		OnRegex(`test\d+`)
		OnRegex(`hello\w+`)

		size := GetRegexCacheSize()
		assert.Greater(t, size, 0)

		ClearRegexCache()

		size = GetRegexCacheSize()
		assert.Equal(t, 0, size)
	})

	t.Run("GetRegexCacheMaxSize", func(t *testing.T) {
		maxSize := GetRegexCacheMaxSize()
		assert.Equal(t, 1000, maxSize)
	})

	t.Run("SetRegexCacheMaxSize", func(t *testing.T) {
		SetRegexCacheMaxSize(500)

		maxSize := GetRegexCacheMaxSize()
		assert.Equal(t, 500, maxSize)

		// Reset to default
		SetRegexCacheMaxSize(1000)
	})

	t.Run("SetRegexCacheMaxSize with invalid", func(t *testing.T) {
		SetRegexCacheMaxSize(-1)

		maxSize := GetRegexCacheMaxSize()
		assert.Equal(t, 1000, maxSize) // Should use default
	})
}

// ============================================================================
// Context Convenience Methods Tests
// ============================================================================

func TestContext_TypedGetters(t *testing.T) {
	ctx := newTestCtx()

	t.Run("MustGetString", func(t *testing.T) {
		ctx.Set("key", "value")

		val, err := ctx.MustGetString("key")
		assert.NoError(t, err)
		assert.Equal(t, "value", val)

		// Wrong type
		ctx.Set("key2", 123)
		_, err = ctx.MustGetString("key2")
		require.Error(t, err)

		// Not found
		_, err = ctx.MustGetString("nonexistent")
		require.Error(t, err)
	})

	t.Run("MustGetInt", func(t *testing.T) {
		ctx.Set("key", 123)

		val, err := ctx.MustGetInt("key")
		assert.NoError(t, err)
		assert.Equal(t, 123, val)

		// Wrong type
		ctx.Set("key2", "string")
		_, err = ctx.MustGetInt("key2")
		require.Error(t, err)
	})

	t.Run("GetString", func(t *testing.T) {
		ctx.Set("key", "value")
		assert.Equal(t, "value", ctx.GetString("key"))
		assert.Equal(t, "", ctx.GetString("nonexistent"))
	})

	t.Run("GetInt", func(t *testing.T) {
		ctx.Set("key", 123)
		assert.Equal(t, 123, ctx.GetInt("key"))
		assert.Equal(t, 0, ctx.GetInt("nonexistent"))
	})

	t.Run("GetInt64", func(t *testing.T) {
		ctx.Set("key", int64(123))
		assert.Equal(t, int64(123), ctx.GetInt64("key"))
		assert.Equal(t, int64(0), ctx.GetInt64("nonexistent"))
	})

	t.Run("GetBool", func(t *testing.T) {
		ctx.Set("key", true)
		assert.True(t, ctx.GetBool("key"))
		assert.False(t, ctx.GetBool("nonexistent"))
	})

	t.Run("GetFloat64", func(t *testing.T) {
		ctx.Set("key", 123.45)
		assert.Equal(t, 123.45, ctx.GetFloat64("key"))
		assert.Equal(t, 0.0, ctx.GetFloat64("nonexistent"))
	})
}

func TestContext_MessageAndEvent(t *testing.T) {
	t.Run("GetPlatformEvent", func(t *testing.T) {
		event := newMockEventWithID(platform.EventKindPrivateMessage, "test-1")
		ctx := AcquireContextFromEvent(event, nil)

		retrieved := ctx.GetPlatformEvent()
		assert.Equal(t, event, retrieved)
	})

	t.Run("GetEventType", func(t *testing.T) {
		ctx := AcquireContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)

		eventType := ctx.GetEventType()
		assert.Equal(t, string(platform.EventKindPrivateMessage), eventType)
	})
}

func TestContext_All(t *testing.T) {
	ctx := newTestCtx()

	ctx.Set("key1", "value1")
	ctx.Set("key2", 123)
	ctx.Set("key3", true)

	all := ctx.All()

	assert.Equal(t, 3, len(all))
	assert.Equal(t, "value1", all["key1"])
	assert.Equal(t, 123, all["key2"])
	assert.Equal(t, true, all["key3"])
}

// ============================================================================
// Concurrent Permission Tests
// ============================================================================

func TestPermissionManager_Concurrent(t *testing.T) {
	pm := NewPermissionManager()

	var wg sync.WaitGroup

	// Concurrent role assignments
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = pm.AssignRole("user1", "admin")
		}(i)
	}

	// Concurrent permission checks
	for range 100 {
		wg.Go(func() {
			pm.HasPermission("user1", permission.Permission{Resource: "test", Action: "execute"})
		})
	}

	wg.Wait()

	// Should not panic or race
}

func TestRole_Concurrent(t *testing.T) {
	role := NewRole("test")

	var wg sync.WaitGroup

	// Concurrent adds
	for range 50 {
		wg.Go(func() {
			role.AddPermission(permission.Permission{Resource: "test", Action: "execute"})
		})
	}

	// Concurrent checks
	for range 50 {
		wg.Go(func() {
			role.HasPermission(permission.Permission{Resource: "test", Action: "execute"})
		})
	}

	wg.Wait()
}

// ============================================================================
// Edge Cases and Bug Discovery
// ============================================================================

func TestBug_MatchCommandWithEmptyContent(t *testing.T) {
	parser := command.NewParser("/")
	parser.Register(&command.Definition{Name: "test"})

	ctx := createTestContextWithMessage("")
	result := ctx.MatchCommand(parser)

	assert.False(t, result)
}

// Benchmark tests
func BenchmarkPermissionMatch(b *testing.B) {
	perm := permission.Permission{Resource: "command:*", Action: "*"}
	target := permission.Permission{Resource: "command:test", Action: "execute"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perm.Match(target)
	}
}

func BenchmarkPermissionManager_HasPermission(b *testing.B) {
	pm := NewPermissionManager()
	_ = pm.AssignRole("user1", "admin")
	perm := permission.Permission{Resource: "test", Action: "execute"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.HasPermission("user1", perm)
	}
}

func BenchmarkParseCommand(b *testing.B) {
	ctx := createTestContextWithMessage("/test arg1 arg2 arg3")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseCommand(ctx)
	}
}
