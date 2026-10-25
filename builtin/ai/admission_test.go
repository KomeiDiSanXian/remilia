package ai

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// busyTestEvent 是一个最小 platform.Event。
type busyTestEvent struct{ kind platform.EventKind }

func (e *busyTestEvent) Platform() string                   { return "test" }
func (e *busyTestEvent) Kind() platform.EventKind           { return e.kind }
func (e *busyTestEvent) RawType() string                    { return string(e.kind) }
func (e *busyTestEvent) ID() string                         { return "e" }
func (e *busyTestEvent) Chat() platform.ChatInfo            { return platform.ChatInfo{ID: "c"} }
func (e *busyTestEvent) Sender() platform.UserInfo          { return platform.UserInfo{ID: "u"} }
func (e *busyTestEvent) Timestamp() time.Time               { return time.Time{} }
func (e *busyTestEvent) RawPayload() any                    { return nil }
func (e *busyTestEvent) Attachments() []platform.Attachment { return nil }
func (e *busyTestEvent) Segments() []platform.Segment       { return nil }

// TestHandleBusy 忙时处理器只负责回一句文案：nil 上下文安全、正常上下文不报错。
func TestHandleBusy(t *testing.T) {
	p := &Plugin{}
	if err := p.handleBusy(nil); err != nil {
		t.Fatalf("nil ctx should be a no-op, got %v", err)
	}

	ctx := eventctx.NewContextFromEvent(&busyTestEvent{kind: platform.EventKindPrivateMessage}, &platform.NoopSender{})
	if err := p.handleBusy(ctx); err != nil {
		t.Fatalf("handleBusy should not fail: %v", err)
	}
}
