package conversation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/conversation"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// conversation.Plugin 无 Setup 初始化逻辑，直接 NewPlugin() 即可，无需走 manager 注册流程。
func newConvPlugin() *conversation.Plugin {
	return conversation.NewPlugin()
}
func makeC2CCtxUser(userID, content string, _ *testbot.MockAPI) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, content)
	return context.NewContextFromEvent(event, nil)
}
func makeGroupCtxUser(userID, groupID, content string) *context.Context {
	event := testbot.MakePlatformGroupEvent(userID, groupID, content)
	return context.NewContextFromEvent(event, nil)
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

func TestConversation_ErrStepRepeat(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	attempts := 0
	var finalCalled bool
	m := p.NewMachine("repeat_test").
		Step("ask", "", func(ctx *context.Context, s *conversation.Session) error {
			attempts++
			if attempts < 3 {
				return conversation.ErrStepRepeat
			}
			return nil
		}).
		Done(func(ctx *context.Context, s *conversation.Session) error {
			finalCalled = true
			return nil
		})

	p.Start(makeC2CCtxUser("u_rep", "/start", api), m)

	// First two dispatches repeat the step, no error
	for i := 1; i <= 2; i++ {
		if err := p.Dispatch(makeC2CCtxUser("u_rep", "try", api)); err != nil {
			t.Fatalf("dispatch %d should not error on ErrStepRepeat: %v", i, err)
		}
		if !p.HasActiveSession("u_rep") {
			t.Fatalf("session should still be active after repeat (attempt %d)", i)
		}
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts so far, got %d", attempts)
	}

	// Third dispatch advances past the step
	if err := p.Dispatch(makeC2CCtxUser("u_rep", "try", api)); err != nil {
		t.Fatalf("third dispatch: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 total attempts, got %d", attempts)
	}
	if !finalCalled {
		t.Error("done callback should be called after step completes")
	}
	if p.HasActiveSession("u_rep") {
		t.Error("session should be removed after completion")
	}
}

func TestConversation_StartWithData(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	var gotName string
	var gotAge int
	m := p.NewMachine("data_test").
		Step("greet", "", func(ctx *context.Context, s *conversation.Session) error {
			gotName, _ = s.Data["name"].(string)
			gotAge, _ = s.Data["age"].(int)
			return nil
		})

	p.StartWithData(makeC2CCtxUser("u_data", "/start", api), m, map[string]any{
		"name": "Alice",
		"age":  30,
	})
	if err := p.Dispatch(makeC2CCtxUser("u_data", "hello", api)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotName != "Alice" {
		t.Errorf("expected name=Alice, got %q", gotName)
	}
	if gotAge != 30 {
		t.Errorf("expected age=30, got %d", gotAge)
	}
}

func TestConversation_SessionNotFound(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	err := p.Dispatch(makeC2CCtxUser("nobody", "hi", api))
	if !errors.Is(err, conversation.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestConversation_DispatchFor(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	var stepCalled bool
	m := p.NewMachine("df_test").
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error {
			stepCalled = true
			return nil
		})

	p.Start(makeC2CCtxUser("u_df", "/start", api), m)

	handler := p.DispatchFor("df_test")

	// Unknown machine → ErrSessionNotFound
	handlerOther := p.DispatchFor("nonexistent")
	if err := handlerOther(makeC2CCtxUser("u_df", "msg", api)); !errors.Is(err, conversation.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound for wrong machine, got %v", err)
	}

	// Correct machine → step called
	if err := handler(makeC2CCtxUser("u_df", "msg", api)); err != nil {
		t.Fatalf("DispatchFor: %v", err)
	}
	if !stepCalled {
		t.Error("step should have been called via DispatchFor")
	}
	if p.HasActiveSession("u_df") {
		t.Error("session should be removed after completion")
	}
}

func TestConversation_DispatchFor_ExpiredSession(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m := p.NewMachine("df_exp").
		WithTimeout(20*time.Millisecond).
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })

	p.Start(makeC2CCtxUser("u_dfexp", "/start", api), m)
	time.Sleep(40 * time.Millisecond)

	handler := p.DispatchFor("df_exp")
	err := handler(makeC2CCtxUser("u_dfexp", "msg", api))
	if !errors.Is(err, conversation.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound for expired session via DispatchFor, got %v", err)
	}
}

func TestConversation_GroupChat_Isolation(t *testing.T) {
	p := newConvPlugin()
	var group1Step, group2Step int
	m1 := p.NewMachine("iso_flow").
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error {
			group1Step++
			return nil
		})
	m2 := p.NewMachine("iso_flow2").
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error {
			group2Step++
			return nil
		})

	// Start same user in two different groups (same machine name, different chatIDs)
	p.Start(makeGroupCtxUser("userA", "group1", "/start"), m1)
	p.Start(makeGroupCtxUser("userA", "group2", "/start"), m2)

	if p.ActiveSessions() < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", p.ActiveSessions())
	}

	// Dispatch from group1 — only group1 session advances
	if err := p.Dispatch(makeGroupCtxUser("userA", "group1", "msg")); err != nil {
		t.Fatalf("group1 dispatch: %v", err)
	}
	if group1Step != 1 {
		t.Errorf("group1 step should be 1, got %d", group1Step)
	}
	if group2Step != 0 {
		t.Errorf("group2 step should still be 0, got %d", group2Step)
	}

	// Dispatch from group2 — only group2 session advances
	if err := p.Dispatch(makeGroupCtxUser("userA", "group2", "msg")); err != nil {
		t.Fatalf("group2 dispatch: %v", err)
	}
	if group2Step != 1 {
		t.Errorf("group2 step should be 1, got %d", group2Step)
	}
}

func TestConversation_CancelInChat(t *testing.T) {
	p := newConvPlugin()
	m := p.NewMachine("cancel_chat").
		Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })

	p.Start(makeGroupCtxUser("userX", "chatRoom", "/start"), m)
	if p.ActiveSessions() != 1 {
		t.Fatalf("expected 1 active session, got %d", p.ActiveSessions())
	}

	// Cancel with wrong chatID → session should remain
	p.CancelInChat("g:otherRoom", "userX", "cancel_chat")
	if p.ActiveSessions() != 1 {
		t.Fatalf("session should remain after CancelInChat with wrong chatID, got %d", p.ActiveSessions())
	}

	// Cancel with correct chatID → session removed
	p.CancelInChat("g:chatRoom", "userX", "cancel_chat")
	if p.ActiveSessions() != 0 {
		t.Errorf("session should be removed after CancelInChat, got %d", p.ActiveSessions())
	}
}

func TestConversation_WaitFor(t *testing.T) {
	p := newConvPlugin()
	var step0Called, step1Called bool

	m := p.NewMachine("waitfor_flow").
		Step("challenge", "", func(ctx *context.Context, s *conversation.Session) error {
			step0Called = true
			return nil
		}).
		Step("accept", "", func(ctx *context.Context, s *conversation.Session) error {
			step1Called = true
			return nil
		}).
		WaitFor(func(s *conversation.Session) string {
			opponent, _ := s.Data["opponent"].(string)
			return opponent
		})

	// WaitFor is designed for group/shared chat: userA initiates, userB responds.
	const group = "arena"
	p.StartWithData(makeGroupCtxUser("userA", group, "/challenge"), m, map[string]any{
		"opponent": "userB",
	})

	// Step0 (challenge): userA dispatches, no WaitFor → advances
	if err := p.Dispatch(makeGroupCtxUser("userA", group, "go!")); err != nil {
		t.Fatalf("step0 dispatch: %v", err)
	}
	if !step0Called {
		t.Error("step0 should have been called")
	}

	// Step1 (accept): userA tries → ignored (WaitFor expects userB)
	if err := p.Dispatch(makeGroupCtxUser("userA", group, "I accept")); err != nil {
		t.Fatalf("step1 userA dispatch (should be silently ignored): %v", err)
	}
	if step1Called {
		t.Error("step1 should NOT be called when wrong user dispatches")
	}

	// Step1 (accept): userB dispatches → advances
	if err := p.Dispatch(makeGroupCtxUser("userB", group, "I accept")); err != nil {
		t.Fatalf("step1 userB dispatch: %v", err)
	}
	if !step1Called {
		t.Error("step1 should be called when expected user dispatches")
	}
}

func TestConversation_WaitForUser(t *testing.T) {
	p := newConvPlugin()
	var confirmed bool
	const group = "admin_room"

	m := p.NewMachine("confirm_flow").
		Step("request", "", func(ctx *context.Context, s *conversation.Session) error { return nil }).
		Step("confirm", "", func(ctx *context.Context, s *conversation.Session) error {
			confirmed = true
			return nil
		}).
		WaitForUser("admin")

	p.Start(makeGroupCtxUser("user1", group, "/request"), m)
	// Advance past step0
	if err := p.Dispatch(makeGroupCtxUser("user1", group, "please confirm")); err != nil {
		t.Fatalf("step0: %v", err)
	}

	// Non-admin tries to confirm → silently ignored
	if err := p.Dispatch(makeGroupCtxUser("user1", group, "confirm")); err != nil {
		t.Fatalf("user1 step1 dispatch: %v", err)
	}
	if confirmed {
		t.Error("should not be confirmed by non-admin user")
	}

	// Admin confirms → advances
	if err := p.Dispatch(makeGroupCtxUser("admin", group, "confirmed")); err != nil {
		t.Fatalf("admin step1 dispatch: %v", err)
	}
	if !confirmed {
		t.Error("admin should have triggered the confirm step")
	}
}

func TestConversation_InSession_GroupVsC2C(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m := p.NewMachine("scope_test").Step("s1", "", func(ctx *context.Context, s *conversation.Session) error { return nil })

	// Start C2C session for userZ
	p.Start(makeC2CCtxUser("userZ", "/start", api), m)

	ruleC2C := p.InSession("scope_test")
	ruleGroup := p.InSession("scope_test")

	// C2C context → session exists
	if !ruleC2C(makeC2CCtxUser("userZ", "msg", api)) {
		t.Error("InSession should match userZ in C2C context")
	}
	// Group context → different chatID → no session
	if ruleGroup(makeGroupCtxUser("userZ", "someGroup", "msg")) {
		t.Error("InSession should NOT match userZ in a group context (different chatID)")
	}
}

func TestConversation_Cancel_Legacy(t *testing.T) {
	p := newConvPlugin()
	api := testbot.NewMockAPI()
	m1 := p.NewMachine("legacy1").Step("s", "", func(ctx *context.Context, s *conversation.Session) error { return nil })
	m2 := p.NewMachine("legacy2").Step("s", "", func(ctx *context.Context, s *conversation.Session) error { return nil })

	p.Start(makeC2CCtxUser("uLeg", "/s", api), m1)
	p.Start(makeC2CCtxUser("uLeg", "/s", api), m2)

	if p.ActiveSessions() < 2 {
		t.Fatalf("expected >= 2 sessions, got %d", p.ActiveSessions())
	}

	// Cancel only legacy1
	p.Cancel("uLeg", "legacy1")

	// legacy2 should remain
	rule := p.InSession("legacy2")
	if !rule(makeC2CCtxUser("uLeg", "x", api)) {
		t.Error("legacy2 session should still be active after cancelling legacy1")
	}
	// legacy1 should be gone
	rule1 := p.InSession("legacy1")
	if rule1(makeC2CCtxUser("uLeg", "x", api)) {
		t.Error("legacy1 session should have been cancelled")
	}
}
