package pluginctrl_test

// pluginctrl_autowire_test.go — Integration tests for autoWireListener behaviour.
//
// Regression coverage for the double-guard bug:
//   When a plugin is unloaded and then re-registered, autoWireListener must NOT
//   append a second combinedGuard to the engine's group middleware chain.
//   Without engine.ResetGroupMiddleware being called before re-wiring,
//   UseForGroup would accumulate guards, causing the user to receive TWO "❌"
//   replies when a feature is disabled.

import (
	stdctx "context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ──────────────────────────────────────────────────────────────────────────────
// helpers — local to this file
// ──────────────────────────────────────────────────────────────────────────────

// awTestEvent is a minimal platform.Event used by the auto-wire tests.
// (pcTestEvent is declared in pluginctrl_user_test.go and shared by the package.)
type awTestEvent struct {
	groupID string
	userID  string
}

func (e *awTestEvent) Platform() string         { return "test" }
func (e *awTestEvent) Kind() platform.EventKind { return platform.EventKindGroupMessage }
func (e *awTestEvent) RawType() string          { return string(platform.EventKindGroupMessage) }
func (e *awTestEvent) Segments() []platform.Segment {
	return []platform.Segment{{Type: platform.SegmentText, Text: "hello"}}
}
func (e *awTestEvent) ID() string           { return "test-aw-event" }
func (e *awTestEvent) Timestamp() time.Time { return time.Time{} }
func (e *awTestEvent) RawPayload() any      { return nil }
func (e *awTestEvent) Chat() platform.ChatInfo {
	return platform.ChatInfo{ID: e.groupID, IsGroup: true}
}
func (e *awTestEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: e.userID} }

// countingSender counts how many times ctx.Reply (→ sender.Send) is called.
// Used to detect whether a guard fires once or multiple times per event.
type countingSender struct {
	n int32
}

func (s *countingSender) Send(_ stdctx.Context, _ platform.SendRequest) (platform.SendResult, error) {
	atomic.AddInt32(&s.n, 1)
	return platform.SendResult{}, nil
}

func (s *countingSender) calls() int { return int(atomic.LoadInt32(&s.n)) }
func (s *countingSender) reset()     { atomic.StoreInt32(&s.n, 0) }

// newAWGroupEvent creates an engine context whose replies go to cs.
func newAWGroupEvent(groupID string, cs *countingSender) *eventctx.Context {
	return eventctx.NewContextFromEvent(
		&awTestEvent{groupID: groupID, userID: "user1"},
		cs,
	)
}

// mustGetCtrl extracts the *pluginctrl.Plugin exported by New().Setup from the
// plugin container.  Fails the test immediately if the cast does not succeed.
func mustGetCtrl(t *testing.T, pm *plugin.Manager) *pluginctrl.Plugin {
	t.Helper()
	raw, ok := pm.GetContainer().Get("pluginctrl")
	require.True(t, ok, "pluginctrl must be present in container after Register")
	ctrl, ok := raw.(*pluginctrl.Plugin)
	require.True(t, ok, "pluginctrl container value must be *pluginctrl.Plugin")
	return ctrl
}

// weatherDesc returns a minimal "weather" plugin descriptor.
// handlerCalled is incremented whenever the handler actually executes (i.e.
// the guard did not block the request).
func weatherDesc(handlerCalled *int32) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name: "weather",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
				Handle(func(c *eventctx.Context) error {
					atomic.AddInt32(handlerCalled, 1)
					return nil
				})
			return nil, nil
		},
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

// TestAutoWire_NormalLoad_GuardFiredOnce checks the happy path:
// after a normal first-time plugin registration, the combinedGuard that
// autoWireListener injects fires exactly once per event.
func TestAutoWire_NormalLoad_GuardFiredOnce(t *testing.T) {
	eng := engine.NewEngine(engine.WithExecPoolDisabled())
	t.Cleanup(func() { _ = eng.Shutdown(stdctx.Background()) })
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin"))))
	ctrl := mustGetCtrl(t, pm)

	var handlerCalled int32
	require.NoError(t, pm.Register(weatherDesc(&handlerCalled)))

	// Disable weather in group1 so the guard blocks and replies "❌"
	require.NoError(t, ctrl.SetGroupEnabled("group1", "weather", false))

	cs := &countingSender{}
	eng.ProcessEvent(newAWGroupEvent("group1", cs))
	eng.WaitForDispatcher()

	assert.Equal(t, 1, cs.calls(),
		"combinedGuard must fire exactly once on a first load")
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalled),
		"weather handler must not run when the plugin is disabled")
}

// TestAutoWire_UnloadReregister_NoDoubleGuard is the primary regression test.
//
// Sequence:
//  1. Register weather → autoWireListener wires one combinedGuard.
//  2. Unregister weather → autoWireListener resets the group middleware entry.
//  3. Re-register weather → autoWireListener wires one combinedGuard again.
//  4. Disable weather → process event → expect exactly ONE "❌" reply.
//
// Before the fix, step 2 skipped the Reset, so step 3 appended a SECOND guard
// and the user received two replies.
func TestAutoWire_UnloadReregister_NoDoubleGuard(t *testing.T) {
	eng := engine.NewEngine(engine.WithExecPoolDisabled())
	t.Cleanup(func() { _ = eng.Shutdown(stdctx.Background()) })
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin"))))
	ctrl := mustGetCtrl(t, pm)

	var handlerCalled int32
	desc := weatherDesc(&handlerCalled)

	// ── First load ────────────────────────────────────────────────────────────
	require.NoError(t, pm.Register(desc))
	require.NoError(t, ctrl.SetGroupEnabled("group1", "weather", false))

	cs := &countingSender{}
	eng.ProcessEvent(newAWGroupEvent("group1", cs))
	eng.WaitForDispatcher()
	assert.Equal(t, 1, cs.calls(), "first load: guard must fire exactly once")

	// ── Unload ────────────────────────────────────────────────────────────────
	require.NoError(t, pm.Unregister(stdctx.Background(), "weather"),
		"Unregister must succeed and trigger OnPluginUnloaded → groupResetFn")

	// ── Re-register (regression scenario) ────────────────────────────────────
	require.NoError(t, pm.Register(desc))
	// Re-disable weather (it was reset to the default enabled state on re-load)
	require.NoError(t, ctrl.SetGroupEnabled("group1", "weather", false))

	cs.reset()
	eng.ProcessEvent(newAWGroupEvent("group1", cs))
	eng.WaitForDispatcher()

	// Without the fix: cs.calls() == 2  (double guard)
	// With  the fix:  cs.calls() == 1
	assert.Equal(t, 1, cs.calls(),
		"after unload+re-register, combinedGuard must fire exactly once (no double guard)")
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalled),
		"weather handler must not run when the plugin is disabled")
}

// TestAutoWire_MultipleReloadCycles_GuardAlwaysOnce verifies that the fix
// holds across several consecutive unload/re-register cycles.
func TestAutoWire_MultipleReloadCycles_GuardAlwaysOnce(t *testing.T) {
	eng := engine.NewEngine(engine.WithExecPoolDisabled())
	t.Cleanup(func() { _ = eng.Shutdown(stdctx.Background()) })
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin"))))
	ctrl := mustGetCtrl(t, pm)

	var handlerCalled int32
	desc := weatherDesc(&handlerCalled)

	const cycles = 3
	for i := range cycles {
		require.NoError(t, pm.Register(desc), "cycle %d: Register must succeed", i)
		require.NoError(t, ctrl.SetGroupEnabled("group1", "weather", false),
			"cycle %d: disable must succeed", i)

		cs := &countingSender{}
		eng.ProcessEvent(newAWGroupEvent("group1", cs))
		eng.WaitForDispatcher()
		assert.Equal(t, 1, cs.calls(),
			"cycle %d: combinedGuard must fire exactly once", i)

		require.NoError(t, pm.Unregister(stdctx.Background(), "weather"), "cycle %d: Unregister must succeed", i)
	}
}

// TestAutoWire_PhantomEntry_AbsentAfterUnload verifies that after unloading a
// plugin its "weather" group no longer has matchers wired, so an incoming event
// does not trigger any guard-related reply.
func TestAutoWire_PhantomEntry_AbsentAfterUnload(t *testing.T) {
	eng := engine.NewEngine(engine.WithExecPoolDisabled())
	t.Cleanup(func() { _ = eng.Shutdown(stdctx.Background()) })
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin"))))
	ctrl := mustGetCtrl(t, pm)

	var handlerCalled int32
	require.NoError(t, pm.Register(weatherDesc(&handlerCalled)))
	require.NoError(t, ctrl.SetGroupEnabled("group1", "weather", false))

	// Unload the plugin — both its matchers and its group middleware are removed
	require.NoError(t, pm.Unregister(stdctx.Background(), "weather"))

	// No weather matchers exist → the guard never applies → 0 replies
	cs := &countingSender{}
	eng.ProcessEvent(newAWGroupEvent("group1", cs))
	eng.WaitForDispatcher()

	assert.Equal(t, 0, cs.calls(),
		"after unload there are no weather matchers; no guard should fire")
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalled),
		"handler must not run after the plugin is unloaded")
}
