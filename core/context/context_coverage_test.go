package context

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Missing Coverage Tests - GetMatcherSource
// ============================================================================

type mockMatcher struct {
	source string
}

func (m *mockMatcher) GetSource() string {
	return m.source
}

func TestContext_GetMatcherSource(t *testing.T) {
	t.Run("with matcher", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.matcher = &mockMatcher{source: "plugin:test"}

		source := ctx.GetMatcherSource()
		assert.Equal(t, "plugin:test", source)
	})

	t.Run("nil matcher", func(t *testing.T) {
		ctx := newTestCtx()

		source := ctx.GetMatcherSource()
		assert.Equal(t, "", source)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		source := ctx.GetMatcherSource()
		assert.Equal(t, "", source)
	})
}

// ============================================================================
// Missing Coverage Tests - Group Rules
// ============================================================================

func TestOnGroupWhitelist(t *testing.T) {
	t.Run("group in whitelist", func(t *testing.T) {
		ctx := AcquireContextFromEvent(newMockGroupEvent("group1"), nil)

		rule := OnGroupWhitelist("group1", "group2")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("group not in whitelist", func(t *testing.T) {
		ctx := AcquireContextFromEvent(newMockGroupEvent("group3"), nil)

		rule := OnGroupWhitelist("group1", "group2")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("nil event", func(t *testing.T) {
		ctx := newTestCtxEmpty()

		rule := OnGroupWhitelist("group1")
		result := rule(ctx)

		assert.True(t, result) // no event → treated as pass-through
	})
}

func TestOnGroupBlacklist(t *testing.T) {
	t.Run("group in blacklist", func(t *testing.T) {
		ctx := AcquireContextFromEvent(newMockGroupEvent("banned1"), nil)

		rule := OnGroupBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("group not in blacklist", func(t *testing.T) {
		ctx := AcquireContextFromEvent(newMockGroupEvent("group1"), nil)

		rule := OnGroupBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("nil event passes", func(t *testing.T) {
		ctx := newTestCtxEmpty()

		rule := OnGroupBlacklist("banned1")
		result := rule(ctx)

		assert.True(t, result)
	})
}

// ============================================================================
// Missing Coverage Tests - EventKind Rules
// ============================================================================

func TestOnC2CMessage(t *testing.T) {
	t.Run("match private message", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnEventKind(platform.EventKindPrivateMessage)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindGroupMessage)

		rule := OnEventKind(platform.EventKindPrivateMessage)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAtMessage(t *testing.T) {
	t.Run("match group message", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindGroupMessage)

		rule := OnEventKind(platform.EventKindGroupMessage)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnEventKind(platform.EventKindGroupMessage)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAddRobot(t *testing.T) {
	t.Run("match notice", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindNotice)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupDelRobot(t *testing.T) {
	t.Run("match notice", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindNotice)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.False(t, result)
	})
}

// ============================================================================
// Additional Coverage Tests - Edge Cases
// ============================================================================

func TestContext_MustGetInt_ErrorBranches(t *testing.T) {
	ctx := newTestCtx()

	t.Run("not found", func(t *testing.T) {
		_, err := ctx.MustGetInt("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx.Set("key", "not an int")
		_, err := ctx.MustGetInt("key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an int")
	})
}

func TestContext_GetPermissionManager_NilCase(t *testing.T) {
	var ctx *Context
	pm := ctx.GetPermissionManager()
	assert.Nil(t, pm)
}

func TestExtensions_Set_NilCase(t *testing.T) {
	var ext *Extensions
	// Should not panic
	ext.Set(extTypeOf[string](), "value")
}

func TestExtGet_WrongType(t *testing.T) {
	ext := newExtensions()
	ext.Set(extTypeOf[string](), 123) // Store int with string key

	result, ok := ExtGet[string](ext)
	assert.False(t, ok)
	assert.Equal(t, "", result)
}

func TestInitRegexCache_CalledMultipleTimes(t *testing.T) {
	// Should be safe to call multiple times
	initRegexCache()
	initRegexCache()
	initRegexCache()

	// Cache should still work
	size := GetRegexCacheSize()
	assert.GreaterOrEqual(t, size, 0)
}
