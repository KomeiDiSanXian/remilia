// permission_bridge.go — backward-compatibility shims for the permission types
// that were formerly defined in this package.
//
// All canonical definitions now live in core/permission.
// The aliases below ensure that existing code using the context-qualified names
// (e.g. eventctx.Permission, eventctx.Manager) continues to compile
// without modification.
package context

import coreperm "github.com/KomeiDiSanXian/remilia/core/permission"

// Permission is an alias for core/permission.Permission.
type Permission = coreperm.Permission

// Role is an alias for core/permission.Role.
type Role = coreperm.Role

// PermissionManager is an alias for core/permission.Manager.
type PermissionManager = coreperm.Manager

// PermissionProvider is an alias for core/permission.Provider.
type PermissionProvider = coreperm.Provider

// NewRole is re-exported from core/permission for backward compatibility.
var NewRole = coreperm.NewRole

// NewPermissionManager is re-exported from core/permission for backward compatibility.
var NewPermissionManager = coreperm.NewPermissionManager

// ErrPermissionDenied is re-exported from core/permission for backward compatibility.
var ErrPermissionDenied = coreperm.ErrPermissionDenied

// PermissionManagerExt stores a *PermissionManager in the Context typed-extension
// store.  This type lives here (not in core/permission) because it is an
// integration detail between the permission system and the event Context.
type PermissionManagerExt struct {
	PM *PermissionManager
}
