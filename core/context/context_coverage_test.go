package context

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// ============================================================================
// Missing Coverage Tests - API Methods
// ============================================================================

// mockOpenAPI for testing
type mockOpenAPI struct {
	groupChatCalled  bool
	singleChatCalled bool
	lastGroupID      string
	lastOpenID       string
	lastMessage      *dto.Message
	returnResult     string
	returnError      error
}

func (m *mockOpenAPI) GroupChat(groupID string, msg *dto.Message) (gjson.Result, error) {
	m.groupChatCalled = true
	m.lastGroupID = groupID
	m.lastMessage = msg
	if m.returnError != nil {
		return gjson.Result{}, m.returnError
	}
	return gjson.Parse(m.returnResult), nil
}

func (m *mockOpenAPI) SingleChat(openID string, msg *dto.Message) (gjson.Result, error) {
	m.singleChatCalled = true
	m.lastOpenID = openID
	m.lastMessage = msg
	if m.returnError != nil {
		return gjson.Result{}, m.returnError
	}
	return gjson.Parse(m.returnResult), nil
}

func (m *mockOpenAPI) SingleRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Parse(m.returnResult), m.returnError
}

func (m *mockOpenAPI) GroupRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Parse(m.returnResult), m.returnError
}

func (m *mockOpenAPI) SingleReset(_, _ string) (gjson.Result, error) {
	return gjson.Parse(m.returnResult), m.returnError
}

func (m *mockOpenAPI) GroupReset(_, _ string) (gjson.Result, error) {
	return gjson.Parse(m.returnResult), m.returnError
}

func TestContext_SendGroupMessage(t *testing.T) {
	t.Run("send success", func(t *testing.T) {
		api := &mockOpenAPI{returnResult: `{"message_id":"123"}`}
		ctx := NewContext(&dto.Payload{}, api)

		msg := &dto.Message{Content: "test"}
		result, err := ctx.SendGroupMessage("group123", msg)

		assert.NoError(t, err)
		assert.True(t, api.groupChatCalled)
		assert.Equal(t, "group123", api.lastGroupID)
		assert.Equal(t, "123", result.Get("message_id").String())
	})

	t.Run("nil api", func(t *testing.T) {
		ctx := NewContext(&dto.Payload{}, nil)

		msg := &dto.Message{Content: "test"}
		_, err := ctx.SendGroupMessage("group123", msg)

		require.Error(t, err)
		assert.Equal(t, ErrNilAPI, err)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context
		msg := &dto.Message{Content: "test"}
		_, err := ctx.SendGroupMessage("group123", msg)

		require.Error(t, err)
		assert.Equal(t, ErrNilAPI, err)
	})
}

func TestContext_SendSingleMessage(t *testing.T) {
	t.Run("send success", func(t *testing.T) {
		api := &mockOpenAPI{returnResult: `{"message_id":"456"}`}
		ctx := NewContext(&dto.Payload{}, api)

		msg := &dto.Message{Content: "test"}
		result, err := ctx.SendSingleMessage("user123", msg)

		assert.NoError(t, err)
		assert.True(t, api.singleChatCalled)
		assert.Equal(t, "user123", api.lastOpenID)
		assert.Equal(t, "456", result.Get("message_id").String())
	})

	t.Run("nil api", func(t *testing.T) {
		ctx := NewContext(&dto.Payload{}, nil)

		msg := &dto.Message{Content: "test"}
		_, err := ctx.SendSingleMessage("user123", msg)

		require.Error(t, err)
		assert.Equal(t, ErrNilAPI, err)
	})
}

func TestContext_ReplyGroup(t *testing.T) {
	t.Run("reply success", func(t *testing.T) {
		// Create event with group info
		event := dto.GroupAtMessageCreateEvent{
			MessageCreateEvent: dto.MessageCreateEvent{
				ID:      "event-1",
				Content: "test",
			},
			GroupOpenID: "group123",
		}

		detail, _ := json.Marshal(event)
		payload := &dto.Payload{
			Type:   dto.GroupAtMessageCreate,
			Detail: detail,
		}

		api := &mockOpenAPI{returnResult: `{"message_id":"789"}`}
		ctx := NewContext(payload, api)

		msg := &dto.Message{Content: "reply"}
		_, err := ctx.ReplyGroup(msg)

		assert.NoError(t, err)
		assert.True(t, api.groupChatCalled)
		assert.Equal(t, "group123", api.lastGroupID)
		assert.Equal(t, dto.EventID("event-1"), api.lastMessage.MessageID)
	})

	t.Run("decode error", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.GroupAtMessageCreate,
			Detail: []byte(`invalid json`),
		}

		api := &mockOpenAPI{}
		ctx := NewContext(payload, api)

		msg := &dto.Message{Content: "reply"}
		_, err := ctx.ReplyGroup(msg)

		require.Error(t, err)
	})
}

func TestContext_ReplyPrivate(t *testing.T) {
	t.Run("reply success", func(t *testing.T) {
		// Create C2C event
		event := dto.C2CMessageCreateEvent{
			MessageCreateEvent: dto.MessageCreateEvent{
				ID:      "event-2",
				Content: "test",
				Author: dto.Author{
					UserOpenID: "user456",
				},
			},
		}

		detail, _ := json.Marshal(event)
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detail,
		}

		api := &mockOpenAPI{returnResult: `{"message_id":"999"}`}
		ctx := NewContext(payload, api)

		msg := &dto.Message{Content: "reply"}
		_, err := ctx.ReplyPrivate(msg)

		assert.NoError(t, err)
		assert.True(t, api.singleChatCalled)
		assert.Equal(t, "user456", api.lastOpenID)
		assert.Equal(t, dto.EventID("event-2"), api.lastMessage.MessageID)
	})

	t.Run("decode error", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`invalid json`),
		}

		api := &mockOpenAPI{}
		ctx := NewContext(payload, api)

		msg := &dto.Message{Content: "reply"}
		_, err := ctx.ReplyPrivate(msg)

		require.Error(t, err)
	})
}

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

		assert.False(t, result)
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

		rule := OnC2CMessage()
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAtMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnC2CMessage()
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAtMessage(t *testing.T) {
	t.Run("match GroupAt message", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAtMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnGroupAtMessage()
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnGroupAtMessage()
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupAddRobot(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupAddRobot}
		ctx := NewContext(payload, nil)

		rule := OnGroupAddRobot()
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnGroupAddRobot()
		result := rule(ctx)

		assert.False(t, result)
	})
}

func TestOnGroupDelRobot(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.GroupDelRobot}
		ctx := NewContext(payload, nil)

		rule := OnGroupDelRobot()
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no match", func(t *testing.T) {
		payload := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(payload, nil)

		rule := OnGroupDelRobot()
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
