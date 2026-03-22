package engine

import (
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Simple Command Test
func TestEngine_OnCommand_Basic(t *testing.T) {
	eng := NewEngine()
	matcher := eng.OnCommand(string(platform.EventKindPrivateMessage), "/test")
	require.NotNil(t, matcher)
	assert.Equal(t, "/test", matcher.GetCommand())
}

// Group Tests
func TestEngine_WithMatcherGroupBatch(t *testing.T) {
	eng := NewEngine()
	var m1, m2 *Matcher

	eng.WithMatcherGroupBatch(func() {
		m1 = eng.On(string(platform.EventKindPrivateMessage))
		m2 = eng.On(string(platform.EventKindGroupMessage))
		eng.SetMatcherGroup(m1, "test-plugin", "source1")
		eng.SetMatcherGroup(m2, "test-plugin", "source2")
	})

	assert.Equal(t, "test-plugin", m1.group)
	assert.Equal(t, "test-plugin", m2.group)
}

func TestEngine_SetMatcherGroup(t *testing.T) {
	eng := NewEngine()
	matcher := eng.On(string(platform.EventKindPrivateMessage))
	eng.SetMatcherGroup(matcher, "my-plugin", "test-source")
	assert.Equal(t, "my-plugin", matcher.group)
	assert.Equal(t, "test-source", matcher.Source)
}

// Named Middleware Test
func TestEngine_Named(t *testing.T) {
	eng := NewEngine()
	executed := false
	mw := eng.Named("test-mw", func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			executed = true
			return next(c)
		}
	})
	require.NotNil(t, mw)
	eng.Use(mw)
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(c *ctx.Context) error { return nil })
	context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
	eng.ProcessEvent(context)
	assert.True(t, executed)
}

// RemoveGroup Tests
func TestEngine_RemoveGroup_WithMatchers(t *testing.T) {
	eng := NewEngine()
	var m1, m2 *Matcher

	eng.WithMatcherGroupBatch(func() {
		m1 = eng.On(string(platform.EventKindPrivateMessage))
		m2 = eng.On(string(platform.EventKindPrivateMessage))
		eng.SetMatcherGroup(m1, "plugin1", "s1")
		eng.SetMatcherGroup(m2, "plugin1", "s2")
	})

	initialCount := eng.GetMatcherCount()
	eng.RemoveGroup("plugin1")
	assert.Equal(t, initialCount-2, eng.GetMatcherCount())
}

// Matcher Edge Cases
func TestMatcher_NoopBehavior(t *testing.T) {
	matcher := &Matcher{Source: "noop"}
	result := matcher.SetPriority(100)
	assert.Equal(t, matcher, result)

	result = matcher.SetBlock(true)
	assert.Equal(t, matcher, result)

	// Handle is now a terminal (void) method; verify it does not panic on noop
	// and does not set a handler (noop matchers are inert).
	handler := func(c *ctx.Context) error { return nil }
	matcher.Handle(handler)
	assert.Nil(t, matcher.Handler, "noop matcher should not store a handler")
}

// TestMatcher_ReplaceHandler verifies that calling Handle twice on the *same*
// *Matcher variable (two separate statements) intentionally replaces the handler.
// Note: the chained form `.Handle(h1).Handle(h2)` is now a compile error because
// Handle is a void terminal method — this test uses the deliberate two-statement form.
func TestMatcher_ReplaceHandler(t *testing.T) {
	eng := NewEngine()
	matcher := eng.On(string(platform.EventKindPrivateMessage))
	var executed string

	handler1 := func(c *ctx.Context) error {
		executed = "handler1"
		return nil
	}
	handler2 := func(c *ctx.Context) error {
		executed = "handler2"
		return nil
	}

	matcher.Handle(handler1)
	matcher.Handle(handler2) // intentional replacement via separate statement

	context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
	eng.ProcessEvent(context)

	assert.Equal(t, "handler2", executed)
}

// Nil Safety Tests
func TestEngine_RebuildMatcherChain_NilSafe(t *testing.T) {
	eng := NewEngine()
	eng.RebuildMatcherChain(nil) // Should not panic
}

func TestEngine_SetMatcherGroup_NilSafe(t *testing.T) {
	eng := NewEngine()
	eng.SetMatcherGroup(nil, "test", "source") // Should not panic
}

// Multiple Middleware in Group
func TestEngine_UseForGroup_Multiple(t *testing.T) {
	eng := NewEngine()
	var order []int

	mw1 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			order = append(order, 1)
			return next(c)
		}
	}
	mw2 := func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			order = append(order, 2)
			return next(c)
		}
	}

	eng.UseForGroup("test", mw1)
	eng.UseForGroup("test", mw2)

	matcher := eng.On(string(platform.EventKindPrivateMessage))
	matcher.group = "test"
	matcher.Handle(func(c *ctx.Context) error { return nil })
	eng.RebuildMatcherChain(matcher)

	context := ctx.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
	eng.ProcessEvent(context)

	assert.Contains(t, order, 1)
	assert.Contains(t, order, 2)
}
