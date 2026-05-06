package engine

import (
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestDisableGroup_PausesMatchers(t *testing.T) {
	eng := newEngineForTest(t)

	triggered := false
	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(c *ctx.Context) error {
		triggered = true
		return nil
	})
	eng.SetMatcherGroup(m, "testplugin", "test")

	// Process once — should trigger
	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	if !triggered {
		t.Fatal("handler should have been triggered before DisableGroup")
	}

	// Disable the group
	eng.DisableGroup("testplugin")
	triggered = false

	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	if triggered {
		t.Error("handler should NOT trigger after DisableGroup")
	}
}

func TestEnableGroup_ResumesMatchers(t *testing.T) {
	eng := newEngineForTest(t)

	triggered := false
	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(c *ctx.Context) error {
		triggered = true
		return nil
	})
	eng.SetMatcherGroup(m, "resume-plugin", "test")

	eng.DisableGroup("resume-plugin")
	eng.EnableGroup("resume-plugin")

	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	if !triggered {
		t.Error("handler should trigger after EnableGroup")
	}
}

func TestDisableGroup_DoesNotDeleteMatchers(t *testing.T) {
	eng := newEngineForTest(t)

	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(c *ctx.Context) error { return nil })
	eng.SetMatcherGroup(m, "persist-plugin", "test")

	stateBefore := eng.state.Load()
	before := len(stateBefore.groupIndex["persist-plugin"])

	eng.DisableGroup("persist-plugin")

	stateAfter := eng.state.Load()
	after := len(stateAfter.groupIndex["persist-plugin"])

	if before != after {
		t.Errorf("DisableGroup should not delete matchers: before=%d after=%d", before, after)
	}
}

func TestDisableGroup_NonExistent_NoError(t *testing.T) {
	eng := newEngineForTest(t)
	// Should not panic
	eng.DisableGroup("nonexistent-group")
}

func TestEnableGroup_NonExistent_NoError(t *testing.T) {
	eng := newEngineForTest(t)
	eng.EnableGroup("nonexistent-group")
}

func TestMatcherIsDisabled(t *testing.T) {
	m := &Matcher{}
	if m.IsDisabled() {
		t.Error("new matcher should not be disabled")
	}
	m.disable()
	if !m.IsDisabled() {
		t.Error("matcher should be disabled after disable()")
	}
	m.enable()
	if m.IsDisabled() {
		t.Error("matcher should not be disabled after enable()")
	}
}
