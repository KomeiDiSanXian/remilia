package context

import "github.com/KomeiDiSanXian/remilia/core/permission"

// NewRole is re-exported from core/permission for backward compatibility.
//
//go:fix inline
var NewRole = permission.NewRole

// NewPermissionManager is re-exported from core/permission for backward compatibility.
//
//go:fix inline
var NewPermissionManager = permission.NewPermissionManager

// PermissionManagerExt stores a *PermissionManager in the Context typed-extension
// store.  This type lives here (not in core/permission) because it is an
// integration detail between the permission system and the event Context.
type PermissionManagerExt struct {
	PM *permission.Manager
}

// GetPermissionManager 获取权限管理器（从 typed extensions）
func (ctx *Context) GetPermissionManager() *permission.Manager {
	if ctx == nil {
		return nil
	}
	if ext, ok := ExtGet[PermissionManagerExt](ctx.Ext()); ok {
		return ext.PM
	}
	return nil
}

// SetPermissionManager 设置权限管理器（到 typed extensions）
func (ctx *Context) SetPermissionManager(pm *permission.Manager) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), PermissionManagerExt{PM: pm})
}
