package engine_test

// process_platform_test.go — ProcessPlatformEvent 集成测试
//
// 验证：
//   1. platform.Event 能正确路由到对应 EventType 的 Matcher
//   2. ctx.GetMessageContent() 返回 event.Content()
//   3. ctx.GetPlatformEvent() 返回原始 platform.Event
//   4. ctx.Reply() 通过 Sender 发送消息
//   5. nil event 不 panic
//   6. 引擎关闭后拒绝处理

import (
	stdctx "context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---

type stubEvent struct {
	platformID string
	kind       platform.EventKind
	rawType    string
	content    string
	chat       platform.ChatInfo
	sndr       platform.UserInfo
}

func (e *stubEvent) Platform() string          { return e.platformID }
func (e *stubEvent) Kind() platform.EventKind  { return e.kind }
func (e *stubEvent) RawType() string           { return e.rawType }
func (e *stubEvent) Content() string           { return e.content }
func (e *stubEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *stubEvent) Sender() platform.UserInfo { return e.sndr }
func (e *stubEvent) Timestamp() time.Time      { return time.Time{} }
func (e *stubEvent) RawPayload() any           { return nil }

type captureSender struct {
	mu       sync.Mutex
	received []platform.OutboundMessage
}

func (s *captureSender) Send(_ stdctx.Context, _ string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, msg)
	return nil
}

func newPlatformC2CEvent(content string) platform.Event {
	return &stubEvent{
		platformID: "qq",
		kind:       platform.EventKindPrivateMessage,
		rawType:    string(dto.C2CMessageCreate),
		content:    content,
		chat:       platform.ChatInfo{ID: "user001", IsGroup: false},
		sndr:       platform.UserInfo{ID: "user001"},
	}
}

func newDiscordEvent(content string) platform.Event {
	return &stubEvent{
		platformID: "discord",
		kind:       platform.EventKindGroupMessage,
		rawType:    "MESSAGE_CREATE",
		content:    content,
		chat:       platform.ChatInfo{ID: "channel001", IsGroup: true},
		sndr:       platform.UserInfo{ID: "discord_user"},
	}
}

// --- tests ---

func TestProcessPlatformEvent_RoutesByEventType(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var called atomic.Bool
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		called.Store(true)
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("hello"), &platform.NoopSender{})
	time.Sleep(20 * time.Millisecond)

	assert.True(t, called.Load(), "Handler should have been called for C2C_MESSAGE_CREATE")
}

func TestProcessPlatformEvent_GetMessageContent(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var got string
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		got = ctx.GetMessageContent()
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("ping pong"), &platform.NoopSender{})
	time.Sleep(20 * time.Millisecond)

	assert.Equal(t, "ping pong", got)
}

func TestProcessPlatformEvent_GetPlatformEvent(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var got platform.Event
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		got = ctx.GetPlatformEvent()
		return nil
	})

	original := newPlatformC2CEvent("check event ref")
	eng.ProcessPlatformEvent(original, &platform.NoopSender{})
	time.Sleep(20 * time.Millisecond)

	require.NotNil(t, got)
	assert.Equal(t, "check event ref", got.Content())
	assert.Equal(t, "qq", got.Platform())
}

func TestProcessPlatformEvent_Reply(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	sender := &captureSender{}
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		return ctx.Reply(platform.TextMessage("pong"))
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("ping"), sender)
	time.Sleep(20 * time.Millisecond)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	require.Len(t, sender.received, 1)
	assert.Equal(t, "pong", sender.received[0].Text)
}

func TestProcessPlatformEvent_NilEvent_NoPanic(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	assert.NotPanics(t, func() {
		eng.ProcessPlatformEvent(nil, &platform.NoopSender{})
	})
}

func TestProcessPlatformEvent_NonQQPlatform(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var called atomic.Bool
	// Discord rawType = "MESSAGE_CREATE"
	eng.On("MESSAGE_CREATE").Handle(func(ctx *corectx.Context) error {
		called.Store(true)
		assert.Equal(t, "discord", ctx.GetEventPlatform())
		return nil
	})

	eng.ProcessPlatformEvent(newDiscordEvent("hello discord"), &platform.NoopSender{})
	time.Sleep(20 * time.Millisecond)

	assert.True(t, called.Load(), "Discord event should be routed to MESSAGE_CREATE matcher")
}

func TestProcessPlatformEvent_AfterShutdown_NoPanic(t *testing.T) {
	eng := engine.NewEngine()
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer cancel()
	_ = eng.Shutdown(ctx)

	assert.NotPanics(t, func() {
		eng.ProcessPlatformEvent(newPlatformC2CEvent("test"), &platform.NoopSender{})
	})
}

func TestProcessPlatformEvent_IsPlatformContext(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var isPlatform bool
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		isPlatform = ctx.IsPlatformContext()
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("test"), &platform.NoopSender{})
	time.Sleep(20 * time.Millisecond)

	assert.True(t, isPlatform, "Context should be identified as platform context")
}
