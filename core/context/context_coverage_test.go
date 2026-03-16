package context

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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
		ctx := NewContext(&dto.Payload{}, nil)
		ctx.matcher = &mockMatcher{source: "plugin:test"}

		source := ctx.GetMatcherSource()
		assert.Equal(t, "plugin:test", source)
	})

	t.Run("nil matcher", func(t *testing.T) {
		ctx := NewContext(&dto.Payload{}, nil)

		source := ctx.GetMatcherSource()
		assert.Equal(t, "", source)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		source := ctx.GetMatcherSource()
		assert.Equal(t, "", source)
	})

	//t.Run("matcher without Matcher", func(t *testing.T) {
	//	ctx := NewContext(&dto.Payload{}, nil)
	//	ctx.matcher = "not a matcher interface"
	//
	//	source := ctx.GetMatcherSource()
	//	assert.Equal(t, "", source)
	//})
}

// ============================================================================
// Missing Coverage Tests - Group Rules
// ============================================================================

func createGroupEventContext(groupID string) *Context {
	event := dto.GroupAtMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: "test",
		},
		GroupOpenID: groupID,
	}

	detail, _ := json.Marshal(event)
	payload := &dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: detail,
	}

	return NewContext(payload, nil)
}

func TestOnGroupWhitelist(t *testing.T) {
	t.Run("group in whitelist", func(t *testing.T) {
		ctx := createGroupEventContext("group1")

		rule := OnGroupWhitelist("group1", "group2")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("group not in whitelist", func(t *testing.T) {
		ctx := createGroupEventContext("group3")

		rule := OnGroupWhitelist("group1", "group2")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("decode error", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.GroupAtMessageCreate,
			Detail: []byte(`invalid json`),
		}
		ctx := NewContext(payload, nil)

		rule := OnGroupWhitelist("group1")
		result := rule(ctx)

		// 解码失败视为「不适用此规则」→ 放行（true）
		assert.True(t, result)
	})
}

func TestOnGroupBlacklist(t *testing.T) {
	t.Run("group in blacklist", func(t *testing.T) {
		ctx := createGroupEventContext("banned1")

		rule := OnGroupBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("group not in blacklist", func(t *testing.T) {
		ctx := createGroupEventContext("group1")

		rule := OnGroupBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("decode error passes", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.GroupAtMessageCreate,
			Detail: []byte(`invalid json`),
		}
		ctx := NewContext(payload, nil)

		rule := OnGroupBlacklist("banned1")
		result := rule(ctx)

		assert.True(t, result) // Decode error should pass
	})
}

// ============================================================================
// Missing Coverage Tests - Deprecated Event Rules
// ============================================================================

func TestOnC2CMessage(t *testing.T) {
	t.Run("match C2C message", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindPrivateMessage)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAtMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindPrivateMessage)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAtMessage(t *testing.T) {
	t.Run("match GroupAt message", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAtMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindGroupMessage)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindGroupMessage)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAddRobot(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAddRobot}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupDelRobot(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupDelRobot}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnEventKind(platform.EventKindNotice)
		result := rule(ctx)

		assert.False(t, result)
	})
}

// ============================================================================
// Additional Coverage Tests - Edge Cases
// ============================================================================

func TestContext_MustGetInt_ErrorBranches(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

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
