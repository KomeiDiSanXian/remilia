package middleware

// Phase 3 migration: avoid scattering raw string keys across middleware.
//
// NOTE: These are user-state keys (ctx.Set/ctx.Get). They are not framework-internal.
// We centralize them here to make future migrations (typed extensions or renaming) easier.

const (
	CtxKeyRequestID = "request_id"
	CtxKeyDegraded  = "degraded"
	CtxKeyUserID    = "user_id"
)
