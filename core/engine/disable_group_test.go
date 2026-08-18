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

func TestListGroups_SnapshotsGroups(t *testing.T) {
	eng := newEngineForTest(t)

	m1 := eng.On(string(platform.EventKindPrivateMessage))
	m1.Handle(func(c *ctx.Context) error { return nil })
	eng.SetMatcherGroup(m1, "group-a", "test")

	m2 := eng.On(string(platform.EventKindGroupMessage))
	m2.Handle(func(c *ctx.Context) error { return nil })
	eng.SetMatcherGroup(m2, "group-a", "test")

	m3 := eng.On(string(platform.EventKindGroupMessage))
	m3.Handle(func(c *ctx.Context) error { return nil })
	eng.SetMatcherGroup(m3, "group-b", "test")

	groups := eng.ListGroups()
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	byName := make(map[string]MatcherGroupInfo, len(groups))
	for _, g := range groups {
		byName[g.Name] = g
	}
	if g := byName["group-a"]; g.Count != 2 || !g.Enabled {
		t.Errorf("group-a: expected count=2 enabled=true, got %+v", g)
	}
	if g := byName["group-b"]; g.Count != 1 || !g.Enabled {
		t.Errorf("group-b: expected count=1 enabled=true, got %+v", g)
	}

	eng.DisableGroup("group-a")
	groups = eng.ListGroups()
	byName = make(map[string]MatcherGroupInfo, len(groups))
	for _, g := range groups {
		byName[g.Name] = g
	}
	if g := byName["group-a"]; g.Enabled {
		t.Error("group-a should be disabled after DisableGroup")
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
