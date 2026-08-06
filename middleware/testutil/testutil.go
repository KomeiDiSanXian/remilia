package testutil

import (
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type MiddlewareTestEvent struct {
	EventID      string
	EventKind    platform.EventKind
	EventContent string
}

func (e *MiddlewareTestEvent) Platform() string                   { return "test" }
func (e *MiddlewareTestEvent) Kind() platform.EventKind           { return e.EventKind }
func (e *MiddlewareTestEvent) RawType() string                    { return string(e.EventKind) }
func (e *MiddlewareTestEvent) Content() string                    { return e.EventContent }
func (e *MiddlewareTestEvent) ID() string                         { return e.EventID }
func (e *MiddlewareTestEvent) Chat() platform.ChatInfo            { return platform.ChatInfo{ID: "chat-001"} }
func (e *MiddlewareTestEvent) Sender() platform.UserInfo          { return platform.UserInfo{ID: "sender-001"} }
func (e *MiddlewareTestEvent) Timestamp() time.Time               { return time.Time{} }
func (e *MiddlewareTestEvent) RawPayload() any                    { return nil }
func (e *MiddlewareTestEvent) Attachments() []platform.Attachment { return nil }
func (e *MiddlewareTestEvent) Segments() []platform.Segment {
	if e.EventContent == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.EventContent}}
}

func MockHandler(err error, delay time.Duration) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Context().Done():
				return ctx.Context().Err()
			}
		}
		return err
	}
}

func MockPanicHandler(panicValue any) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		panic(panicValue)
	}
}

func CreateTestContext() *eventctx.Context {
	event := &MiddlewareTestEvent{
		EventID:   "test-event",
		EventKind: platform.EventKindPrivateMessage,
	}
	return eventctx.NewContextFromEvent(event, &platform.NoopSender{})
}

func CreatePlatformContextWithID(id string) *eventctx.Context {
	event := &MiddlewareTestEvent{
		EventID:   id,
		EventKind: platform.EventKindPrivateMessage,
	}
	return eventctx.NewContextFromEvent(event, &platform.NoopSender{})
}
