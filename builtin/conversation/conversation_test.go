package conversation_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/conversation"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// conversation.Plugin 无 Setup 初始化逻辑，直接 NewPlugin() 即可，无需走 manager 注册流程。
func newConvPlugin() *conversation.Plugin {
	return conversation.NewPlugin()
}
func makeC2CCtxUser(userID, content string, api *testbot.MockAPI) *context.Context {
	detail, _ := json.Marshal(dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{Content: content, Author: dto.Author{UserOpenID: userID}},
	})
	return context.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, api)
}
func TestConversation_StartAndAdvance(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	var step1Called, step2Called, doneCalled bool
	m := p.NewMachine("test_flow").
		Step("step1", "", func(ctx *context.Context, s *conversation.Session) error {
			step1Called = true
			return nil
		}).
		Step("step2", "", func(ctx *context.Context, s *conversation.Session) error {
			step2Called = true
			return nil
		}).
		Done(func(ctx *context.Context, s *conversation.Session) error {
			doneCalled = true
			return nil
		})
	p.Start(makeC2CCtxUser("user1", "/start", api), m)
	if !p.HasActiveSession("user1") {
		t.Fatal("should have active session")
	}
	if err := p.Dispatch(makeC2CCtxUser("user1", "Alice", api)); err != nil {
		t.Fatalf("Dispatch step1: %v", err)
	}
	if !step1Called {
		t.Error("step1 should have been called")
	}
	if err := p.Dispatch(makeC2CCtxUser("user1", "25", api)); err != nil {
		t.Fatalf("Dispatch step2: %v", err)
	}
	if !step2Called {
		t.Error("step2 should have been called")
	}
	if !doneCalled {
		t.Error("done should have been called")
	}
	if p.HasActiveSession("user1") {
		t.Error("session should be removed after completion")
	}
}
func TestConversation_InSession_Rule(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m := p.NewMachine("rule_test").Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	rule := p.InSession("rule_test")
	if rule(makeC2CCtxUser("u2", "x", api)) {
		t.Error("rule should not match before Start")
	}
	p.Start(makeC2CCtxUser("u2", "/start", api), m)
	if !rule(makeC2CCtxUser("u2", "x", api)) {
		t.Error("rule should match after Start")
	}
}
func TestConversation_ErrStepDone(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	doneCalled := false
	m := p.NewMachine("done_test").
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error {
			return conversation.ErrStepDone
		}).
		Done(func(ctx *context.Context, s *conversation.Session) error {
			doneCalled = true
			return nil
		})
	p.Start(makeC2CCtxUser("u3", "/start", api), m)
	p.Dispatch(makeC2CCtxUser("u3", "x", api))
	if !doneCalled {
		t.Error("done should be called on ErrStepDone")
	}
}
func TestConversation_Cancel(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m := p.NewMachine("cancel_test").Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	p.Start(makeC2CCtxUser("u4", "/start", api), m)
	p.Cancel("u4", "cancel_test")
	if p.HasActiveSession("u4") {
		t.Error("session should be removed after Cancel")
	}
}
func TestConversation_SessionExpiry(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m := p.NewMachine("expiry_test").
		WithTimeout(30*time.Millisecond).
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	p.Start(makeC2CCtxUser("u5", "/start", api), m)
	time.Sleep(50 * time.Millisecond)
	p.GC()
	if p.HasActiveSession("u5") {
		t.Error("expired session should be removed by GC")
	}
}
func TestConversation_ActiveSessions(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m1 := p.NewMachine("as_m1").Step("s", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	m2 := p.NewMachine("as_m2").Step("s", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	p.Start(makeC2CCtxUser("u10", "/s", api), m1)
	p.Start(makeC2CCtxUser("u11", "/s", api), m2)
	if p.ActiveSessions() < 2 {
		t.Errorf("expected >= 2, got %d", p.ActiveSessions())
	}
}
