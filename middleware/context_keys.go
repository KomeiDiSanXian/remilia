package middleware

// Phase 3 migration: avoid scattering raw string keys across middleware.
//
// NOTE: These are user-state keys (ctx.Set/ctx.Get). They are not framework-internal.
// We centralize them here to make future migrations (typed extensions or renaming) easier.

import "github.com/KomeiDiSanXian/remilia/middleware/ctxkeys"

const (
	CtxKeyRequestID = ctxkeys.CtxKeyRequestID
	CtxKeyDegraded  = ctxkeys.CtxKeyDegraded
	CtxKeyUserID    = ctxkeys.CtxKeyUserID
)
