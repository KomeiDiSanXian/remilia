package remilia

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

// MockOpenAPI is a mock implementation of openapi.OpenAPI for testing
type MockOpenAPI struct {
	GroupChatCalled  bool
	SingleChatCalled bool
	LastGroupID      string
	LastOpenID       string
	LastMessage      *dto.Message
}

func (m *MockOpenAPI) GroupChat(groupID string, msg *dto.Message) (gjson.Result, error) {
	m.GroupChatCalled = true
	m.LastGroupID = groupID
	m.LastMessage = msg
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) SingleChat(openID string, msg *dto.Message) (gjson.Result, error) {
	m.SingleChatCalled = true
	m.LastOpenID = openID
	m.LastMessage = msg
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) SingleRichMedia(openid string, media *dto.Media) (gjson.Result, error) {
	// mark params as used in tests
	_ = openid
	_ = media
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) GroupRichMedia(groupID string, media *dto.Media) (gjson.Result, error) {
	_ = groupID
	_ = media
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) SingleReset(openid, messageID string) (gjson.Result, error) {
	_ = openid
	_ = messageID
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) GroupReset(groupID, messageID string) (gjson.Result, error) {
	_ = groupID
	_ = messageID
	return gjson.Result{}, nil
}

var _ openapi.OpenAPI = (*MockOpenAPI)(nil) // Ensure MockOpenAPI implements openapi.OpenAPI

func TestNewContext(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-id",
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	assert.NotNil(t, ctx)
	assert.Equal(t, event, ctx.event)
	assert.NotNil(t, ctx.userState)
	assert.NotNil(t, ctx.internalState)
	assert.Equal(t, mockAPI, ctx.api)
}

func TestContextGetEvent(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-id",
	}

	ctx := NewContext(event, nil)

	assert.Equal(t, event, ctx.GetEvent())
}

func TestContextGetEventType(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}

	ctx := NewContext(event, nil)

	assert.Equal(t, dto.GroupAtMessageCreate, ctx.GetEventType())
}

func TestContextState(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}

	ctx := NewContext(event, nil)

	// Test setting and getting state using thread-safe API
	ctx.SetState("key1", "value1")
	ctx.SetState("key2", 123)

	val1, ok1 := ctx.GetState("key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	val2, ok2 := ctx.GetState("key2")
	assert.True(t, ok2)
	assert.Equal(t, 123, val2)

	// Test GetAllState
	allState := ctx.GetAllState()
	assert.Equal(t, 2, len(allState))
	assert.Equal(t, "value1", allState["key1"])
	assert.Equal(t, 123, allState["key2"])

	// Test DeleteState
	ctx.DeleteState("key1")
	_, ok := ctx.GetState("key1")
	assert.False(t, ok)
}

func TestContextSendGroupMessage(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	msg := &dto.Message{
		Content: "test message",
	}

	_, err := ctx.SendGroupMessage("test-group-id", msg)

	assert.NoError(t, err)
	assert.True(t, mockAPI.GroupChatCalled)
	assert.Equal(t, "test-group-id", mockAPI.LastGroupID)
	assert.Equal(t, msg, mockAPI.LastMessage)
}

func TestContextSendGroupMessage_NilAPI(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}

	ctx := NewContext(event, nil)

	msg := &dto.Message{
		Content: "test message",
	}

	assert.NotPanics(t, func() {
		_, err := ctx.SendGroupMessage("test-group-id", msg)
		assert.Error(t, err)
		assert.Equal(t, ErrNilAPI, err)
	})
}

func TestContextSendSingleMessage(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	msg := &dto.Message{
		Content: "test message",
	}

	_, err := ctx.SendSingleMessage("test-open-id", msg)

	assert.NoError(t, err)
	assert.True(t, mockAPI.SingleChatCalled)
	assert.Equal(t, "test-open-id", mockAPI.LastOpenID)
	assert.Equal(t, msg, mockAPI.LastMessage)
}

func TestContextSendSingleMessage_NilAPI(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}

	ctx := NewContext(event, nil)

	msg := &dto.Message{
		Content: "test message",
	}

	assert.NotPanics(t, func() {
		_, err := ctx.SendSingleMessage("test-open-id", msg)
		assert.Error(t, err)
		assert.Equal(t, ErrNilAPI, err)
	})
}

func TestContextGetMessageContent(t *testing.T) {
	t.Parallel()
	detailMap := map[string]interface{}{
		"content": "Hello World",
	}
	detailJSON, _ := json.Marshal(detailMap)

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	ctx := NewContext(event, nil)

	content := ctx.GetMessageContent()
	assert.Equal(t, "Hello World", content)
}

func TestContextGetMessageContent_EmptyDetail(t *testing.T) {
	t.Parallel()
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(""),
	}

	ctx := NewContext(event, nil)

	content := ctx.GetMessageContent()
	assert.Equal(t, "", content)
}

func TestContextGetMessageContent_NoContent(t *testing.T) {
	t.Parallel()
	detailMap := map[string]interface{}{
		"other_field": "value",
	}
	detailJSON, _ := json.Marshal(detailMap)

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	ctx := NewContext(event, nil)

	content := ctx.GetMessageContent()
	assert.Equal(t, "", content)
}

func TestContextReplyGroup(t *testing.T) {
	t.Parallel()
	groupEvent := dto.GroupAtMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID:      "event-123",
			Content: "test content",
		},
		GroupOpenID: "group-456",
	}

	detailJSON, _ := json.Marshal(groupEvent)
	event := &dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: detailJSON,
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	msg := &dto.Message{
		Content: "reply message",
	}

	_, err := ctx.ReplyGroup(msg)

	assert.NoError(t, err)
	assert.True(t, mockAPI.GroupChatCalled)
	assert.Equal(t, "group-456", mockAPI.LastGroupID)
	assert.Equal(t, dto.EventID("event-123"), mockAPI.LastMessage.MessageID)
}

func TestContextReplyPrivate(t *testing.T) {
	t.Parallel()
	c2cEvent := dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID: "event-789",
			Author: dto.Author{
				UserOpenID: "user-123",
			},
			Content: "test content",
		},
	}

	detailJSON, _ := json.Marshal(c2cEvent)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	msg := &dto.Message{
		Content: "reply message",
	}

	_, err := ctx.ReplyPrivate(msg)

	assert.NoError(t, err)
	assert.True(t, mockAPI.SingleChatCalled)
	assert.Equal(t, "user-123", mockAPI.LastOpenID)
	assert.Equal(t, dto.EventID("event-789"), mockAPI.LastMessage.MessageID)
}

func TestContextReplyGroup_WithExistingEventID(t *testing.T) {
	t.Parallel()
	groupEvent := dto.GroupAtMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID:      "event-123",
			Content: "test content",
		},
		GroupOpenID: "group-456",
	}

	detailJSON, _ := json.Marshal(groupEvent)
	event := &dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: detailJSON,
	}

	mockAPI := &MockOpenAPI{}
	ctx := NewContext(event, mockAPI)

	msg := &dto.Message{
		Content: "reply message",
		EventID: dto.EventID("existing-event-id"),
	}

	_, err := ctx.ReplyGroup(msg)

	assert.NoError(t, err)
	assert.Equal(t, dto.EventID("existing-event-id"), mockAPI.LastMessage.EventID, "Should preserve existing EventID")
}

func TestContextDecodeEvent(t *testing.T) {
	t.Parallel()
	groupEvent := dto.GroupAtMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID:      "event-123",
			Content: "test content",
		},
		GroupOpenID: "group-456",
	}

	detailJSON, _ := json.Marshal(groupEvent)
	event := &dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: detailJSON,
	}

	ctx := NewContext(event, nil)

	var decoded dto.GroupAtMessageCreateEvent
	err := ctx.DecodeEvent(&decoded)

	assert.NoError(t, err)
	assert.Equal(t, "event-123", string(decoded.ID))
	assert.Equal(t, "group-456", decoded.GroupOpenID)
	assert.Equal(t, "test content", decoded.Content)
}

func TestContextGetAuthor(t *testing.T) {
	t.Parallel()
	c2cEvent := dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID: "event-789",
			Author: dto.Author{
				UserOpenID: "user-123",
			},
		},
	}

	detailJSON, _ := json.Marshal(c2cEvent)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	ctx := NewContext(event, nil)

	author := ctx.GetAuthor()

	assert.NotNil(t, author)
	assert.Equal(t, "user-123", author.UserOpenID)
}

func TestContextRetainRelease_AsyncSafe(t *testing.T) {
	t.Parallel()
	// Prepare event with content
	payload := map[string]any{"content": "hello"}
	b, _ := json.Marshal(payload)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: b}
	ctx := NewContext(event, nil)

	// Set some state to verify visibility
	ctx.SetState("k", "v")

	done := make(chan struct{})

	go func() {
		defer close(done)
		time.Sleep(10 * time.Millisecond)
		// Should still be valid even if main goroutine released earlier
		content := ctx.GetMessageContent()
		assert.Equal(t, "hello", content)
		val, ok := ctx.GetState("k")
		assert.True(t, ok)
		assert.Equal(t, "v", val)
	}()
	<-done
}
