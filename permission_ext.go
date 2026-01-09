package remilia

// PermissionManagerExt stores PermissionManager in Context typed extensions.
//
// This is a Phase 3 migration target to avoid using string-key user state
// like "permission_manager".
//
// Note: pointer is treated as immutable after set.
type PermissionManagerExt struct {
	PM *PermissionManager
}

// SetPermissionManager stores pm into ctx typed extensions.
func (ctx *Context) SetPermissionManager(pm *PermissionManager) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), PermissionManagerExt{PM: pm})
}

// GetPermissionManager returns pm from typed extensions.
func (ctx *Context) GetPermissionManager() (*PermissionManager, bool) {
	if ctx == nil {
		return nil, false
	}
	v, ok := ExtGet[PermissionManagerExt](ctx.Ext())
	if !ok {
		return nil, false
	}
	if v.PM == nil {
		return nil, false
	}
	return v.PM, true
}
