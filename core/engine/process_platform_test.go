package engine_test

// process_platform_test.go — ProcessPlatformEvent 集成测试
//
// 架构决策 B：新路径 GetEventType() 返回 EventKind 字符串（如 "PRIVATE_MESSAGE"），
// 而非平台原始类型（如 "C2C_MESSAGE_CREATE"）。
// 因此，Matcher 注册时应使用 platform.EventKindXxx，而非 dto.C2CMessageCreate 等 QQ 专属常量。
//
// 验证：
//   1. platform.Event 按 EventKind 路由到正确的 Matcher
//   2. ctx.GetMessageContent() 返回 event.Content()
//   3. ctx.GetPlatformEvent() 返回原始 platform.Event
//   4. ctx.Reply() 通过 Sender 发送消息
//   5. nil event 不 panic
//   6. 引擎关闭后拒绝处理
//   7. 非 QQ 平台（Discord）按 EventKind 路由
//   8. QQ 旧路径匹配器（dto.C2CMessageCreate）不匹配新路径事件

import (
	stdctx "context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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

func (e *stubEvent) Platform() string                   { return e.platformID }
func (e *stubEvent) Kind() platform.EventKind           { return e.kind }
func (e *stubEvent) RawType() string                    { return e.rawType }
func (e *stubEvent) Content() string                    { return e.content }
func (e *stubEvent) Attachments() []platform.Attachment { return nil }
func (e *stubEvent) Chat() platform.ChatInfo            { return e.chat }
func (e *stubEvent) Sender() platform.UserInfo          { return e.sndr }
func (e *stubEvent) Timestamp() time.Time               { return time.Time{} }
func (e *stubEvent) ID() string                         { return "" }
func (e *stubEvent) RawPayload() any                    { return nil }

type captureSender struct {
	mu       sync.Mutex
	received []platform.OutboundMessage
}

func (s *captureSender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, req.Message)
	return platform.SendResult{}, nil
}

func newPlatformC2CEvent(content string) platform.Event {
	return &stubEvent{
		platformID: "qq",
		kind:       platform.EventKindPrivateMessage,
		rawType:    dto.C2CMessageCreate,
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

// TestProcessPlatformEvent_RoutesByEventKind verifies architecture decision B:
// new-path events are routed by EventKind string, not by raw platform type.
func TestProcessPlatformEvent_RoutesByEventKind(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var called atomic.Bool
	// Register with EventKind string — the correct way for new-path events
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *corectx.Context) error {
		called.Store(true)
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("hello"), &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	assert.True(t, called.Load(), "Handler should be called when matched by EventKind PRIVATE_MESSAGE")
}

// TestProcessPlatformEvent_OldQQMatcher_DoesNotMatchNewPath verifies that QQ-specific
// matchers (dto.C2CMessageCreate = "C2C_MESSAGE_CREATE") do NOT match new-path events
// (architecture decision B intentionally separates old and new routing).
func TestProcessPlatformEvent_OldQQMatcher_DoesNotMatchNewPath(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var called atomic.Bool
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *corectx.Context) error {
		called.Store(true)
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("hello"), &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	assert.False(t, called.Load(), "QQ-specific (C2C_MESSAGE_CREATE) matcher must NOT match new-path events")
}

// Kept for naming consistency but now uses EventKind routing.
func TestProcessPlatformEvent_RoutesByEventType(t *testing.T) {
	TestProcessPlatformEvent_RoutesByEventKind(t)
}

func TestProcessPlatformEvent_GetMessageContent(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var got string
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *corectx.Context) error {
		got = ctx.GetMessageContent()
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("ping pong"), &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	assert.Equal(t, "ping pong", got)
}

func TestProcessPlatformEvent_GetPlatformEvent(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var got platform.Event
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *corectx.Context) error {
		got = ctx.GetPlatformEvent()
		return nil
	})

	original := newPlatformC2CEvent("check event ref")
	eng.ProcessPlatformEvent(original, &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	require.NotNil(t, got)
	assert.Equal(t, "check event ref", got.Content())
	assert.Equal(t, "qq", got.Platform())
}

func TestProcessPlatformEvent_Reply(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	sender := &captureSender{}
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *corectx.Context) error {
		ctx.Reply(platform.TextMessage("pong"))
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("ping"), sender)
	eng.WaitForAsyncHandlers()
	eng.WaitForDispatcher()

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

// TestProcessPlatformEvent_NonQQPlatform verifies that Discord events are routed
// by their EventKind ("GROUP_MESSAGE"), not by raw type ("MESSAGE_CREATE").
func TestProcessPlatformEvent_NonQQPlatform(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck

	var called atomic.Bool
	// Register with EventKind — platform-agnostic routing
	eng.On(string(platform.EventKindGroupMessage)).Handle(func(ctx *corectx.Context) error {
		called.Store(true)
		assert.Equal(t, "discord", ctx.GetEventPlatform())
		return nil
	})

	eng.ProcessPlatformEvent(newDiscordEvent("hello discord"), &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	assert.True(t, called.Load(), "Discord event should be routed by EventKind GROUP_MESSAGE")
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
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(ctx *corectx.Context) error {
		isPlatform = ctx.IsPlatformContext()
		return nil
	})

	eng.ProcessPlatformEvent(newPlatformC2CEvent("test"), &platform.NoopSender{})
	eng.WaitForAsyncHandlers()

	assert.True(t, isPlatform, "Context should be identified as platform context")
}
