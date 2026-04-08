package engine

// reset_group_middleware_test.go — Unit tests for Engine.ResetGroupMiddleware
//
// Covers:
//   - No-op behaviour when the group entry is absent
//   - The entry is removed from the middleware state after Reset
//   - Multiple resets are safe (idempotent)
//   - Empty group name is a no-op (whitespace-only name)
//   - Core regression: without Reset a second UseForGroup call doubles
//     the middleware chain; with Reset the chain stays at exactly one entry.

import (
	"sync/atomic"
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────────

// newCountingMiddleware returns a middleware that atomically increments *n each
// time it is entered, then calls next.
func newCountingMiddleware(n *int32) ctx.Middleware {
	return func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			atomic.AddInt32(n, 1)
			return next(c)
		}
	}
}

// groupEntryExists reports whether the engine's middleware state contains a
// non-nil snapshot for groupName.
func groupEntryExists(e *Engine, groupName string) bool {
	state := e.middleware.Load()
	snap, ok := state.groupMiddlewares[groupName]
	return ok && snap != nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

// TestResetGroupMiddleware_NoOp_WhenGroupAbsent verifies that calling
// ResetGroupMiddleware on a group that was never registered is safe and
// returns the engine for chaining.
func TestResetGroupMiddleware_NoOp_WhenGroupAbsent(t *testing.T) {
	eng := NewEngine()

	result := eng.ResetGroupMiddleware("nonexistent-group")

	assert.Equal(t, eng, result, "ResetGroupMiddleware must return the engine for chaining")
	assert.False(t, groupEntryExists(eng, "nonexistent-group"),
		"absent group must not be created by Reset")
}

// TestResetGroupMiddleware_ClearsGroupEntry verifies that after UseForGroup
// registers middleware for a group, ResetGroupMiddleware removes the entry.
func TestResetGroupMiddleware_ClearsGroupEntry(t *testing.T) {
	eng := NewEngine()
	var n int32

	eng.UseForGroup("weather", newCountingMiddleware(&n))
	require.True(t, groupEntryExists(eng, "weather"), "entry should exist after UseForGroup")

	eng.ResetGroupMiddleware("weather")

	assert.False(t, groupEntryExists(eng, "weather"),
		"entry should be removed after ResetGroupMiddleware")
}

// TestResetGroupMiddleware_Idempotent verifies that calling ResetGroupMiddleware
// multiple times in a row is safe and does not panic.
func TestResetGroupMiddleware_Idempotent(t *testing.T) {
	eng := NewEngine()
	var n int32
	eng.UseForGroup("weather", newCountingMiddleware(&n))

	require.NotPanics(t, func() {
		eng.ResetGroupMiddleware("weather")
		eng.ResetGroupMiddleware("weather")
		eng.ResetGroupMiddleware("weather")
	}, "multiple resets must not panic")

	assert.False(t, groupEntryExists(eng, "weather"))
}

// TestResetGroupMiddleware_EmptyGroupName_NoOp verifies that a blank / whitespace
// group name is rejected as a no-op (matching UseForGroup semantics).
func TestResetGroupMiddleware_EmptyGroupName_NoOp(t *testing.T) {
	eng := NewEngine()
	var n int32
	eng.UseForGroup("real-group", newCountingMiddleware(&n))

	require.NotPanics(t, func() {
		eng.ResetGroupMiddleware("")
		eng.ResetGroupMiddleware("   ")
	})

	// The "real-group" entry must be untouched
	assert.True(t, groupEntryExists(eng, "real-group"),
		"empty-name reset must not affect other groups")
}

// TestResetGroupMiddleware_PreventsDoubleExecution is the core regression test.
//
// Scenario (double-guard bug):
//  1. A plugin's combinedGuard is wired via UseForGroup("weather", guard).
//  2. The plugin is unloaded but the guard is NOT cleaned up (old bug).
//  3. The plugin is re-registered → UseForGroup is called again, appending a
//     SECOND guard.  Result: the user receives TWO "❌" replies.
//
// The fix (ResetGroupMiddleware before re-wiring) ensures the chain is always
// exactly one guard deep regardless of how many times the plugin was
// registered/unregistered.
func TestResetGroupMiddleware_PreventsDoubleExecution(t *testing.T) {
	const groupName = "weather"
	eventType := string(platform.EventKindGroupMessage)

	// ── Part A: demonstrate the bug (UseForGroup twice without Reset) ─────────
	t.Run("double UseForGroup accumulates guards", func(t *testing.T) {
		eng := NewEngine()
		var calls int32
		mw := newCountingMiddleware(&calls)

		// Register a matcher in the "weather" group
		eng.On(eventType).SetGroup(groupName).Handle(func(_ *ctx.Context) error { return nil })

		// Simulate re-register without Reset (the bug)
		eng.UseForGroup(groupName, mw)
		eng.UseForGroup(groupName, mw) // second wire – doubles the guard

		event := newTestPlatformEvent(platform.EventKindGroupMessage)
		c := ctx.AcquireContextFromEvent(event, nil)
		eng.ProcessEvent(c)

		assert.Equal(t, int32(2), atomic.LoadInt32(&calls),
			"without Reset, guard runs twice — this documents the bug")
	})

	// ── Part B: Reset before re-wiring prevents the double execution ──────────
	t.Run("Reset before UseForGroup keeps exactly one guard", func(t *testing.T) {
		eng := NewEngine()
		var calls int32
		mw := newCountingMiddleware(&calls)

		eng.On(eventType).SetGroup(groupName).Handle(func(_ *ctx.Context) error { return nil })

		// First load
		eng.UseForGroup(groupName, mw)

		// Simulate unload+reload: reset before re-wiring (the fix)
		eng.ResetGroupMiddleware(groupName) // clean up the old guard
		eng.UseForGroup(groupName, mw)      // re-wire exactly one guard

		event := newTestPlatformEvent(platform.EventKindGroupMessage)
		c := ctx.AcquireContextFromEvent(event, nil)
		eng.ProcessEvent(c)

		assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
			"with Reset+wire, guard runs exactly once — this verifies the fix")
	})
}

// TestResetGroupMiddleware_NewMatcherPicksUpFreshChain verifies that a matcher
// registered AFTER ResetGroupMiddleware + UseForGroup picks up the new,
// single-guard chain (not a stale one).
func TestResetGroupMiddleware_NewMatcherPicksUpFreshChain(t *testing.T) {
	const groupName = "weather"
	eventType := string(platform.EventKindGroupMessage)

	eng := NewEngine()
	var callsA, callsB int32

	// First registration cycle
	eng.UseForGroup(groupName, newCountingMiddleware(&callsA))

	// Reset then re-wire with a new (B) middleware
	eng.ResetGroupMiddleware(groupName)
	eng.UseForGroup(groupName, newCountingMiddleware(&callsB))

	// Register a matcher in the group AFTER the reset cycle
	eng.On(eventType).SetGroup(groupName).Handle(func(_ *ctx.Context) error { return nil })

	event := newTestPlatformEvent(platform.EventKindGroupMessage)
	c := ctx.AcquireContextFromEvent(event, nil)
	eng.ProcessEvent(c)

	assert.Equal(t, int32(0), atomic.LoadInt32(&callsA),
		"stale (A) middleware must not run after Reset")
	assert.Equal(t, int32(1), atomic.LoadInt32(&callsB),
		"fresh (B) middleware must run exactly once")
}
