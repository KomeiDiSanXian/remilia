package testbot_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnCommand("/echo")).Handle(func(ctx *context.Context) error {
		content := ctx.GetMessageContent()
		return ctx.Reply(platform.TextMessage(content))
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformGroupAt("user1", "group1", "/echo hello")
	tb.AssertPlatformReplied(t, "/echo hello")
}
func TestBot_SendC2C(t *testing.T) {
	tb := testbot.New()
	tb.Engine().OnEventKind(platform.EventKindPrivateMessage, context.OnCommand("/ping")).Handle(func(ctx *context.Context) error {
		return ctx.Reply(platform.TextMessage("pong"))
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformC2C("user42", "/ping")
	tb.AssertPlatformReplied(t, "pong")
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
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnKeyword("ping")).Handle(func(ctx *context.Context) error {
		return ctx.Reply(platform.TextMessage("pong"))
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformGroupAt("u1", "g1", "ping")
	tb.SendPlatformGroupAt("u1", "g1", "ping")
	if got := len(tb.SenderAPI().Sent()); got != 2 {
		t.Errorf("expected 2 sent messages, got %d", got)
	}
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
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnCommand("/hi")).Handle(func(ctx *context.Context) error {
		return ctx.Reply(platform.TextMessage("hi"))
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformGroupAt("u1", "g1", "/hi")
	tb.SenderAPI().Clear()
	if got := len(tb.SenderAPI().Sent()); got != 0 {
		t.Errorf("expected 0 sent messages after clear, got %d", got)
	}
}
