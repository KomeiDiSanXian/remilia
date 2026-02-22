package testbot_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

func TestMockAPI_SingleChat(t *testing.T) {
	api := testbot.NewMockAPI()
	msg := &dto.Message{Type: dto.TextMessage, Content: "hello"}
	api.SingleChat("user1", msg)
	sent := api.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent, got %d", len(sent))
	}
	if sent[0].Target != "user1" || sent[0].IsGroup {
		t.Errorf("unexpected sent: %+v", sent[0])
	}
}
func TestMockAPI_GroupChat(t *testing.T) {
	api := testbot.NewMockAPI()
	msg := &dto.Message{Type: dto.TextMessage, Content: "broadcast"}
	api.GroupChat("group1", msg)
	last := api.LastSent()
	if last == nil || last.Target != "group1" || !last.IsGroup {
		t.Errorf("unexpected last sent: %+v", last)
	}
}
func TestMockAPI_Clear(t *testing.T) {
	api := testbot.NewMockAPI()
	api.SingleChat("u", &dto.Message{Content: "x"})
	api.Clear()
	if len(api.Sent()) != 0 {
		t.Error("expected empty after Clear")
	}
}
func TestBot_SendGroupAt_AssertReplied(t *testing.T) {
	tb := testbot.New()
	// Register a simple echo handler
	tb.Engine().OnGroupAt(context.OnCommand("/echo")).Handle(func(ctx *context.Context) error {
		content := ctx.GetMessageContent()
		var gae dto.GroupAtMessageCreateEvent
		_ = ctx.DecodeEvent(&gae)
		msg := &dto.Message{Type: dto.TextMessage, Content: content}
		_, err := ctx.ReplyGroup(msg)
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendGroupAt("group1", "user1", "/echo hello")
	tb.AssertReplied(t, "group1", "/echo hello")
}
func TestBot_SendC2C(t *testing.T) {
	tb := testbot.New()
	tb.Engine().OnC2C(context.OnCommand("/ping")).Handle(func(ctx *context.Context) error {
		msg := &dto.Message{Type: dto.TextMessage, Content: "pong"}
		var ev dto.C2CMessageCreateEvent
		_ = ctx.DecodeEvent(&ev)
		_, err := ctx.SendSingleMessage(ev.Author.UserOpenID, msg)
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendC2C("user42", "/ping")
	tb.AssertReplied(t, "user42", "pong")
}
func TestBot_AssertNotReplied(t *testing.T) {
	tb := testbot.New()
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendC2C("user1", "no handler for this")
	tb.AssertNotReplied(t, "user1")
}
func TestBot_AssertSentCount(t *testing.T) {
	tb := testbot.New()
	tb.Engine().OnGroupAt(context.OnKeyword("ping")).Handle(func(ctx *context.Context) error {
		var ev dto.GroupAtMessageCreateEvent
		_ = ctx.DecodeEvent(&ev)
		_, err := ctx.ReplyGroup(&dto.Message{Content: "pong"})
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendGroupAt("g1", "u1", "ping")
	tb.SendGroupAt("g1", "u1", "ping")
	tb.AssertSentCount(t, 2)
}
func TestBot_Inject_ArbitraryPayload(t *testing.T) {
	tb := testbot.New()
	fired := false
	tb.Engine().On(dto.GroupAddRobot).Handle(func(ctx *context.Context) error {
		fired = true
		return nil
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.Inject(&dto.Payload{
		Operation: dto.Dispatch,
		Type:      dto.GroupAddRobot,
	})
	if !fired {
		t.Error("handler was not fired")
	}
}
func TestBot_ClearSent(t *testing.T) {
	tb := testbot.New()
	tb.Engine().OnGroupAt(context.OnCommand("/hi")).Handle(func(ctx *context.Context) error {
		var ev dto.GroupAtMessageCreateEvent
		_ = ctx.DecodeEvent(&ev)
		_, err := ctx.ReplyGroup(&dto.Message{Content: "hi"})
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendGroupAt("g1", "u1", "/hi")
	tb.ClearSent()
	tb.AssertSentCount(t, 0)
}
