package ratelimitui_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/ratelimitui"
)

// TestBindPermission verifies BindPermission and HasPermissionPlugin (Bug 2.12 fix).
func TestBindPermission(t *testing.T) {
	p := ratelimitui.NewPlugin()
	if p.HasPermissionPlugin() {
		t.Fatal("should not have permission plugin initially")
	}
	perm := permission.NewPlugin()
	p.BindPermission(perm)
	if !p.HasPermissionPlugin() {
		t.Fatal("should have permission plugin after BindPermission")
	}
	t.Log("✓ Bug 2.12 修复：BindPermission/HasPermissionPlugin 公开 API 正常")
}

// TestPermission_NilSafe verifies that nil permission plugin doesn't panic.
func TestPermission_NilSafe(t *testing.T) {
	p := ratelimitui.NewPlugin()
	if p.HasPermissionPlugin() {
		t.Error("should return false when no permission plugin")
	}
	t.Log("✓ Bug 2.12 修复：permission 插件未绑定时 HasPermissionPlugin 返回 false")
}
