package benchmark

import (
	"time"

	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// benchmarkTestEvent is a minimal platform.Event stub for benchmark tests.
type benchmarkTestEvent struct {
	content string
}

func (e *benchmarkTestEvent) Platform() string          { return "test" }
func (e *benchmarkTestEvent) Kind() platform.EventKind  { return platform.EventKindPrivateMessage }
func (e *benchmarkTestEvent) RawType() string           { return string(platform.EventKindPrivateMessage) }
func (e *benchmarkTestEvent) Content() string           { return e.content }
func (e *benchmarkTestEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{ID: "chat-001"} }
func (e *benchmarkTestEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: "sender-001"} }
func (e *benchmarkTestEvent) Timestamp() time.Time      { return time.Time{} }
func (e *benchmarkTestEvent) ID() string                { return "bench-event" }
func (e *benchmarkTestEvent) RawPayload() any           { return nil }

// newBenchmarkEvent creates a platform.Event with the given message content.
func newBenchmarkEvent(content string) platform.Event {
	return &benchmarkTestEvent{content: content}
}

// newBenchmarkContext creates a *rcontext.Context from a platform event with given content.
func newBenchmarkContext(content string) *rcontext.Context {
	return rcontext.AcquireContextFromEvent(newBenchmarkEvent(content), nil)
}
