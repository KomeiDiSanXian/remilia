package context_test

// platform_event_test.go — 平台无关 Context 新路径测试

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- stub ---

type testPlatformEvent struct {
	platformID string
	kind       platform.EventKind
	rawType    string
	content    string
	chat       platform.ChatInfo
	sender     platform.UserInfo
}

func (e *testPlatformEvent) Platform() string                          { return e.platformID }
func (e *testPlatformEvent) Kind() platform.EventKind                  { return e.kind }
func (e *testPlatformEvent) RawType() string                           { return e.rawType }
func (e *testPlatformEvent) Content() string                           { return e.content }
func (e *testPlatformEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *testPlatformEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *testPlatformEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *testPlatformEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *testPlatformEvent) ID() string                                { return "" }
func (e *testPlatformEvent) RawPayload() any                           { return nil }

func makeTestEvent(platformID, rawType, content string, kind platform.EventKind) platform.Event {
	return &testPlatformEvent{
		platformID: platformID,
		rawType:    rawType,
		content:    content,
		kind:       kind,
		chat:       platform.ChatInfo{ID: "chat001"},
	}
}

// --- AcquireContextFromEvent ---

func TestAcquireContextFromEvent_BasicFields(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hello", platform.EventKindPrivateMessage)
	ctx := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	defer context.ReleaseContextFromEvent(ctx)

	assert.True(t, ctx.IsPlatformContext())
	assert.Equal(t, event, ctx.GetPlatformEvent())
	assert.NotNil(t, ctx.GetPlatformSender())
}

func TestAcquireContextFromEvent_GetMessageContent(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "test content", platform.EventKindPrivateMessage)
	ctx := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	defer context.ReleaseContextFromEvent(ctx)

	assert.Equal(t, "test content", ctx.GetMessageContent())
	// 二次调用走 Once 缓存，不应再调用 event.Content()
	assert.Equal(t, "test content", ctx.GetMessageContent())
}

func TestAcquireContextFromEvent_GetEventType(t *testing.T) {
	// Architecture decision B: new-path GetEventType() returns the EventKind string,
	// not the platform-specific raw type, so OnEventKind-based matchers work universally.
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "", platform.EventKindPrivateMessage)
	ctx := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	defer context.ReleaseContextFromEvent(ctx)

	// New path returns EventKind string ("PRIVATE_MESSAGE"), not raw type ("C2C_MESSAGE_CREATE")
	assert.Equal(t, string(platform.EventKindPrivateMessage), ctx.GetEventType())
	// Raw type is still accessible via GetPlatformEvent().RawType()
	assert.Equal(t, "C2C_MESSAGE_CREATE", ctx.GetPlatformEvent().RawType())
}

func TestAcquireContextFromEvent_GetEventPlatform(t *testing.T) {
	event := makeTestEvent("discord", "MESSAGE_CREATE", "", platform.EventKindGroupMessage)
	ctx := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	defer context.ReleaseContextFromEvent(ctx)

	assert.Equal(t, "discord", ctx.GetEventPlatform())
}

func TestAcquireContextFromEvent_GetEventKind(t *testing.T) {
	event := makeTestEvent("telegram", "PRIVATE_MSG", "", platform.EventKindPrivateMessage)
	ctx := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	defer context.ReleaseContextFromEvent(ctx)

	assert.Equal(t, platform.EventKindPrivateMessage, ctx.GetEventKind())
}

func TestAcquireContextFromEvent_Reply(t *testing.T) {
	type msg struct {
		chatID string
		text   string
	}
	var got *msg

	sender := &captureTestSender{fn: func(chat platform.ChatInfo, m platform.OutboundMessage) {
		got = &msg{chatID: chat.ID, text: m.Text}
	}}

	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	// 重写 event 的 chat.ID
	event.(*testPlatformEvent).chat.ID = "user_abc"

	ctx := context.AcquireContextFromEvent(event, sender)
	defer context.ReleaseContextFromEvent(ctx)

	err := ctx.Reply(platform.TextMessage("pong"))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "user_abc", got.chatID)
	assert.Equal(t, "pong", got.text)
}

func TestAcquireContextFromEvent_ReplyWithContext(t *testing.T) {
	sender := &captureTestSender{}
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	ctx := context.AcquireContextFromEvent(event, sender)
	defer context.ReleaseContextFromEvent(ctx)

	err := ctx.ReplyWithContext(stdctx.Background(), platform.TextMessage("hello"))
	assert.NoError(t, err)
}

// --- Pool reuse ---

func TestReleaseContextFromEvent_PoolReuse(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "msg", platform.EventKindPrivateMessage)

	ctx1 := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	ptr1 := ctx1
	context.ReleaseContextFromEvent(ctx1)

	ctx2 := context.AcquireContextFromEvent(event, &platform.NoopSender{})
	ptr2 := ctx2
	context.ReleaseContextFromEvent(ctx2)

	// 池化后应复用同一对象（不保证，但通常成立）
	_ = ptr1
	_ = ptr2
	// 主要验证 Release 后不会 panic 且可以再次 Acquire
}

// --- helper ---

type captureTestSender struct {
	fn func(chat platform.ChatInfo, msg platform.OutboundMessage)
}

func (s *captureTestSender) Send(ctx stdctx.Context, msg platform.OutboundMessage) error {
	if s.fn != nil {
		chat, _ := platform.ChatInfoFromContext(ctx)
		s.fn(chat, msg)
	}
	return nil
}
