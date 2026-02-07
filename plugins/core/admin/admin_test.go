package admin

import (
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminPlugin_Load(t *testing.T) {
	eng := engine.NewEngine()
	adminPlugin := New()

	err := adminPlugin.Load(eng)
	require.NoError(t, err)
}

func TestAdminPlugin_SetPluginManager(t *testing.T) {
	eng := engine.NewEngine()
	manager := plugin.NewManager(eng)
	adminPlugin := New()

	adminPlugin.SetPluginManager(manager)
	assert.NotNil(t, adminPlugin.pluginManager)
}

func TestAdminPlugin_SetPermissionPlugin(t *testing.T) {
	adminPlugin := New()
	permPlugin := permission.New()

	adminPlugin.SetPermissionPlugin(permPlugin)
	assert.NotNil(t, adminPlugin.permPlugin)
}

func TestAdminPlugin_CheckPermission(t *testing.T) {
	adminPlugin := New()
	permPlugin := permission.New()
	adminPlugin.SetPermissionPlugin(permPlugin)

	// Create mock context
	ctx := &eventctx.Context{}
	ctx.Set("user_id", "user123")

	// Grant permission
	permPlugin.Grant("user123", "test.permission")

	// Check permission (should pass)
	hasPermission := adminPlugin.checkPermission(ctx, "test.permission")
	assert.True(t, hasPermission)

	// Check non-existent permission (should fail)
	hasPermission = adminPlugin.checkPermission(ctx, "nonexistent")
	assert.False(t, hasPermission)
}

func TestAdminPlugin_WithoutPermissionPlugin(t *testing.T) {
	adminPlugin := New()

	// Create mock context
	ctx := &eventctx.Context{}

	// Without permission plugin, should default to true
	hasPermission := adminPlugin.checkPermission(ctx, "any.permission")
	assert.True(t, hasPermission)
}

func TestAdminPlugin_Dependencies(t *testing.T) {
	adminPlugin := New()
	deps := adminPlugin.Dependencies()
	assert.Contains(t, deps, "permission")
}

func TestAdminPlugin_Metadata(t *testing.T) {
	adminPlugin := New()
	meta := adminPlugin.Metadata()

	assert.Equal(t, "admin", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "系统", meta.Category)
	assert.Contains(t, meta.Tags, "管理")
}

// Integration test with permission plugin
func TestAdminPlugin_Integration(t *testing.T) {
	eng := engine.NewEngine()
	manager := plugin.NewManager(eng)

	// Create and register permission plugin
	permPlugin := permission.New()
	err := manager.Register(permPlugin)
	require.NoError(t, err)

	// Create and register admin plugin
	adminPlugin := New()
	adminPlugin.SetPluginManager(manager)
	adminPlugin.SetPermissionPlugin(permPlugin)
	err = manager.Register(adminPlugin)
	require.NoError(t, err)

	// Verify plugins are loaded
	plugins := manager.List()
	assert.Contains(t, plugins, "permission")
	assert.Contains(t, plugins, "admin")
}
