package context_test

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPlatformEvent struct {
	platformID string
	kind       platform.EventKind
	rawType    string
	content    string
	chat       platform.ChatInfo
	sender     platform.UserInfo
}

func (e *testPlatformEvent) Platform() string         { return e.platformID }
func (e *testPlatformEvent) Kind() platform.EventKind { return e.kind }
func (e *testPlatformEvent) RawType() string          { return e.rawType }
func (e *testPlatformEvent) Content() string          { return e.content }

func (e *testPlatformEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *testPlatformEvent) Attachments() []platform.Attachment { return nil }
func (e *testPlatformEvent) Chat() platform.ChatInfo            { return e.chat }
func (e *testPlatformEvent) Sender() platform.UserInfo          { return e.sender }
func (e *testPlatformEvent) Timestamp() time.Time               { return time.Time{} }
func (e *testPlatformEvent) ID() string                         { return "" }
func (e *testPlatformEvent) RawPayload() any                    { return nil }

func makeTestEvent(platformID, rawType, content string, kind platform.EventKind) platform.Event {
	return &testPlatformEvent{
		platformID: platformID,
		rawType:    rawType,
		content:    content,
		kind:       kind,
		chat:       platform.ChatInfo{ID: "chat001"},
	}
}

func TestNewContextFromEvent_BasicFields(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hello", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.True(t, ctx.IsPlatformContext())
	assert.Equal(t, event, ctx.GetPlatformEvent())
	assert.NotNil(t, ctx.GetPlatformSender())
}

func TestNewContextFromEvent_GetMessageContent(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "test content", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.Equal(t, "test content", ctx.GetMessageContent())
	assert.Equal(t, "test content", ctx.GetMessageContent())
}

func TestNewContextFromEvent_GetEventType(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.Equal(t, string(platform.EventKindPrivateMessage), ctx.GetEventType())
	assert.Equal(t, "C2C_MESSAGE_CREATE", platform.RawType(ctx.GetPlatformEvent()))
}

func TestNewContextFromEvent_GetEventPlatform(t *testing.T) {
	event := makeTestEvent("discord", "MESSAGE_CREATE", "", platform.EventKindGroupMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.Equal(t, "discord", ctx.GetEventPlatform())
}

func TestNewContextFromEvent_GetEventKind(t *testing.T) {
	event := makeTestEvent("telegram", "PRIVATE_MSG", "", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.Equal(t, platform.EventKindPrivateMessage, ctx.GetEventKind())
}

// testDispatcher 同步执行提交的任务，用于测试。
type testDispatcher struct {
	taskFn func(ctx stdctx.Context)
}

func (d *testDispatcher) Submit(_ string, task func(stdctx.Context) error) error {
	if d != nil && d.taskFn != nil {
		d.taskFn(stdctx.Background())
	}
	if task != nil {
		return task(stdctx.Background())
	}
	return nil
}

func TestNewContextFromEvent_Reply(t *testing.T) {
	type msg struct {
		chatID string
		text   string
	}
	var got *msg

	sender := &captureTestSender{fn: func(chat platform.ChatInfo, m platform.OutboundMessage) {
		got = &msg{chatID: chat.ID, text: m.Text}
	}}

	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "ping", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).chat.ID = "user_abc"

	ctx := context.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(&testDispatcher{})
	ctx.Reply(platform.TextMessage("pong"))
	require.NotNil(t, got)
	assert.Equal(t, "user_abc", got.chatID)
	assert.Equal(t, "pong", got.text)
}

func TestNewContextFromEvent_ReplyWithContext(t *testing.T) {
	sender := &captureTestSender{}
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, sender)
	ctx.SetDispatcher(&testDispatcher{})

	ctx.ReplyWithContext(stdctx.Background(), platform.TextMessage("hello"))
}

type captureTestSender struct {
	fn func(chat platform.ChatInfo, msg platform.OutboundMessage)
}

func (s *captureTestSender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if s.fn != nil {
		s.fn(req.Target, req.Message)
	}
	return platform.SendResult{}, nil
}

func TestIsFromSelf_NoBotID(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "hi", platform.EventKindPrivateMessage)
	event.(*testPlatformEvent).sender = platform.UserInfo{ID: "user123"}

	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})
	assert.False(t, ctx.IsFromSelf())
}

func TestIsFromSelf_BotIDSet_Match(t *testing.T) {
	event := makeTestEvent("discord", "MESSAGE_CREATE", "hello", platform.EventKindGroupMessage)
	event.(*testPlatformEvent).sender = platform.UserInfo{ID: "bot-001"}

	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})
	ctx.SetBotID("bot-001")
	assert.True(t, ctx.IsFromSelf())
}

func TestIsFromSelf_BotIDSet_NoMatch(t *testing.T) {
	event := makeTestEvent("discord", "MESSAGE_CREATE", "hello", platform.EventKindGroupMessage)
	event.(*testPlatformEvent).sender = platform.UserInfo{ID: "user-xyz"}

	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})
	ctx.SetBotID("bot-001")
	assert.False(t, ctx.IsFromSelf())
}

func TestIsFromSelf_GetBotID(t *testing.T) {
	event := makeTestEvent("qq", "C2C_MESSAGE_CREATE", "", platform.EventKindPrivateMessage)
	ctx := context.NewContextFromEvent(event, &platform.NoopSender{})

	assert.Equal(t, "", ctx.GetBotID())
	ctx.SetBotID("my-bot-id")
	assert.Equal(t, "my-bot-id", ctx.GetBotID())
}

func TestIsFromSelf_NilContext(t *testing.T) {
	var ctx *context.Context
	assert.False(t, ctx.IsFromSelf())
	assert.Equal(t, "", ctx.GetBotID())
}
