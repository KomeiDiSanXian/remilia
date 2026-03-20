package chaos

import (
	"time"

	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// chaosTestEvent is a minimal platform.Event stub for chaos tests.
type chaosTestEvent struct {
	content string
}

func (e *chaosTestEvent) Platform() string                          { return "test" }
func (e *chaosTestEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *chaosTestEvent) RawType() string                           { return string(platform.EventKindPrivateMessage) }
func (e *chaosTestEvent) Content() string                           { return e.content }
func (e *chaosTestEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: "chat-001"} }
func (e *chaosTestEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "sender-001"} }
func (e *chaosTestEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *chaosTestEvent) ID() string                                { return "chaos-event" }
func (e *chaosTestEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *chaosTestEvent) RawPayload() any                           { return nil }

// newChaosEvent creates a platform.Event with the given message content.
func newChaosEvent(content string) platform.Event {
	return &chaosTestEvent{content: content}
}

// newChaosContext creates a *rcontext.Context from a platform event with given content.
func newChaosContext(content string) *rcontext.Context {
	return rcontext.AcquireContextFromEvent(newChaosEvent(content), nil)
}
