package degradation

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/middleware/ctxkeys"
)

// DegradedExt is a typed extension marker for degradation simplify strategy.
//
// Phase 3 goal:
//   - Prefer typed extension over user-state key to avoid collisions.
//   - Keep a compatibility window: set both typed extension and user-state key.
//   - Readers should prefer typed extension first, then fallback to user-state key.
//
// Note: this is a marker-only extension; no fields needed.
type DegradedExt struct{}

// SetDegraded marks the context as degraded.
func SetDegraded(ctx *context.Context) {
	if ctx == nil {
		return
	}
	ctx.Ext().SetTyped(DegradedExt{})
	// Compatibility (temporary): also set user-state key.
	ctx.Set(ctxkeys.CtxKeyDegraded, true)
}

// IsDegraded reports whether the context is marked as degraded.
// It checks typed extension first; if not found, it falls back to user-state key.
func IsDegraded(ctx *context.Context) bool {
	if ctx == nil {
		return false
	}
	if _, ok := ctx.Ext().GetTyped[DegradedExt](); ok {
		return true
	}
	v, ok := ctx.Get(ctxkeys.CtxKeyDegraded)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
