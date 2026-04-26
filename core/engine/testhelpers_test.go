package engine

// testhelpers_test.go — 平台无关事件测试桩
//
// 提供给 engine 包内所有 _test.go 使用的 platform.Event 测试实现。
// 迁移自旧路径（dto.Payload）测试，对应 Phase 3 前置条件。

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// engineTestEvent 是 platform.Event 的测试桩，供 engine 包测试使用。
type engineTestEvent struct {
	kind     platform.EventKind
	content  string
	senderID string
	chatID   string
	rawType  string
	id       string
}

func (e *engineTestEvent) Platform() string                          { return "test" }
func (e *engineTestEvent) Kind() platform.EventKind                  { return e.kind }
func (e *engineTestEvent) RawType() string                           { return e.rawType }
func (e *engineTestEvent) Content() string                           { return e.content }
func (e *engineTestEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *engineTestEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: e.senderID} }
func (e *engineTestEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *engineTestEvent) ID() string                                { return e.id }
func (e *engineTestEvent) RawPayload() any                           { return nil }
func (e *engineTestEvent) Attachments() []platform.InboundAttachment { return nil }

// newTestPlatformEvent 创建指定 EventKind 的测试桩事件。
func newTestPlatformEvent(kind platform.EventKind) platform.Event {
	return &engineTestEvent{
		kind:     kind,
		content:  "test message",
		senderID: "sender-001",
		chatID:   "chat-001",
		rawType:  string(kind),
	}
}

// newTestPlatformEventWithContent 创建指定 EventKind 和消息内容的测试桩事件。
func newTestPlatformEventWithContent(kind platform.EventKind, content string) platform.Event {
	return &engineTestEvent{
		kind:     kind,
		content:  content,
		senderID: "sender-001",
		chatID:   "chat-001",
		rawType:  string(kind),
	}
}

// newEngineForTest 创建一个 Engine 并绑定测试生命周期。
//
// 默认禁用后台工作者（temp cleaner / pending delete），避免 goroutine 泄漏。
// 测试结束时自动调用 Shutdown。同时适用于 *testing.T 和 *testing.B。
func newEngineForTest(t testing.TB, opts ...Option) *Engine {
	t.Helper()
	e := NewEngine(append([]Option{WithNoBackgroundWorkers()}, opts...)...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
	})
	return e
}
