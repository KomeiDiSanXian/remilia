package integration

import (
	"time"

	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// integrationTestEvent is a minimal platform.Event stub for integration tests.
type integrationTestEvent struct {
	content string
}

func (e *integrationTestEvent) Platform() string         { return "test" }
func (e *integrationTestEvent) Kind() platform.EventKind { return platform.EventKindPrivateMessage }
func (e *integrationTestEvent) RawType() string          { return string(platform.EventKindPrivateMessage) }
func (e *integrationTestEvent) Content() string          { return e.content }

func (e *integrationTestEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *integrationTestEvent) Chat() platform.ChatInfo            { return platform.ChatInfo{ID: "test-chat"} }
func (e *integrationTestEvent) Sender() platform.UserInfo          { return platform.UserInfo{ID: "test_user"} }
func (e *integrationTestEvent) Timestamp() time.Time               { return time.Time{} }
func (e *integrationTestEvent) ID() string                         { return "integration-event" }
func (e *integrationTestEvent) Attachments() []platform.Attachment { return nil }
func (e *integrationTestEvent) RawPayload() any                    { return nil }

// newIntegrationEvent creates a platform.Event with the given message content.
func newIntegrationEvent(content string) platform.Event {
	return &integrationTestEvent{content: content}
}

// newIntegrationContext creates a *rcontext.Context with the given message content.
func newIntegrationContext(content string) *rcontext.Context {
	return rcontext.NewContextFromEvent(newIntegrationEvent(content), nil)
}
