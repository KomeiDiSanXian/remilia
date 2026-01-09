package remilia

// UserIDExt stores an override user ID in typed extensions.
//
// Phase 3 goal:
//   - Avoid scattering user-state string keys like "user_id" across the codebase.
//   - Avoid import cycles (remilia must not depend on middleware).
//
// Behavior contract:
//   - If set via typed extension, GetUserID(ctx) should return this value.
//   - Otherwise, GetUserID falls back to user-state key "user_id",
//     then event author ID.
//
// Note: this is an override. Usually user ID comes from the event.
type UserIDExt struct {
	UserID string
}

// SetUserID stores an override user ID into ctx typed extensions.
// It does NOT write the user-state key. (Callers can do both during migration if desired.)
func (ctx *Context) SetUserID(userID string) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), UserIDExt{UserID: userID})
}

// GetUserIDExt returns the typed extension user ID, if any.
func (ctx *Context) GetUserIDExt() (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ExtGet[UserIDExt](ctx.Ext())
	if !ok {
		return "", false
	}
	if v.UserID == "" {
		return "", false
	}
	return v.UserID, true
}
