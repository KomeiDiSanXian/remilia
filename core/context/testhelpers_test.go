package context

// testhelpers_test.go — 内部测试桩
//
// 为 core/context 包的内部（whitebox）测试提供 platform.Event mock 实现。

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// mockEvent 是 platform.Event 的最小测试桩实现。
type mockEvent struct {
	kind     platform.EventKind
	content  string
	id       string
	rawType  string
	chat     platform.ChatInfo
	sender   platform.UserInfo
	platform string
}

func (e *mockEvent) Platform() string                          { return e.platform }
func (e *mockEvent) Kind() platform.EventKind                  { return e.kind }
func (e *mockEvent) RawType() string                           { return e.rawType }
func (e *mockEvent) Content() string                           { return e.content }
func (e *mockEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *mockEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *mockEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *mockEvent) ID() string                                { return e.id }
func (e *mockEvent) RawPayload() any                           { return nil }
func (e *mockEvent) Attachments() []platform.InboundAttachment { return nil }

// newMockEvent 创建指定 EventKind 的测试桩（content 为空，chat ID 为 "chat001"）。
func newMockEvent(kind platform.EventKind) *mockEvent {
	return &mockEvent{
		kind:     kind,
		rawType:  string(kind),
		platform: "test",
		chat:     platform.ChatInfo{ID: "chat001"},
	}
}

// newMockEventWithContent 创建带内容的测试桩。
func newMockEventWithContent(kind platform.EventKind, content string) *mockEvent {
	e := newMockEvent(kind)
	e.content = content
	return e
}

// newMockEventWithID 创建带事件 ID 的测试桩。
func newMockEventWithID(kind platform.EventKind, id string) *mockEvent {
	e := newMockEvent(kind)
	e.id = id
	return e
}

// newMockGroupEvent 创建群组消息测试桩（指定 groupID 作为 chat.ID）。
func newMockGroupEvent(groupID string) *mockEvent {
	e := newMockEvent(platform.EventKindGroupMessage)
	e.chat = platform.ChatInfo{ID: groupID}
	return e
}

// newMockEventWithSender 创建带发送者信息的测试桩。
func newMockEventWithSender(kind platform.EventKind, senderID string) *mockEvent {
	e := newMockEvent(kind)
	e.sender = platform.UserInfo{ID: senderID}
	return e
}

// newTestCtx 创建一个用于测试的 Context（私聊消息类型）。
func newTestCtx() *Context {
	return NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
}

// newTestCtxWithKind 创建指定事件类型的测试 Context。
func newTestCtxWithKind(kind platform.EventKind) *Context {
	return NewContextFromEvent(newMockEvent(kind), nil)
}

// newTestCtxEmpty 创建一个无事件的测试 Context（用于只测试 Context 状态管理的场景）。
func newTestCtxEmpty() *Context {
	return NewContextFromEvent(nil, nil)
}
