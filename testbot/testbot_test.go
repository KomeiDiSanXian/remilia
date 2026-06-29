package testbot_test

import (
	stdctx "context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

func TestMockAPI_SingleChat(t *testing.T) {
	api := testbot.NewMockAPI()
	msg := &dto.Message{Type: dto.TextMessage, Content: "hello"}
	api.SingleChat(stdctx.Background(), "user1", msg)
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
	api.GroupChat(stdctx.Background(), "group1", msg)
	last := api.LastSent()
	if last == nil || last.Target != "group1" || !last.IsGroup {
		t.Errorf("unexpected last sent: %+v", last)
	}
}
func TestMockAPI_Clear(t *testing.T) {
	api := testbot.NewMockAPI()
	api.SingleChat(stdctx.Background(), "u", &dto.Message{Content: "x"})
	api.Clear()
	if len(api.Sent()) != 0 {
		t.Error("expected empty after Clear")
	}
}
func TestBot_SendGroupAt_AssertReplied(t *testing.T) {
	tb := testbot.NewQQBot()
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnCommand("/echo")).Handle(func(ctx *context.Context) error {
		content := ctx.GetMessageContent()
		ctx.Reply(platform.TextMessage(content))
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformGroupAt("user1", "group1", "/echo hello")
	tb.AssertPlatformReplied(t, "/echo hello")
}
func TestBot_SendC2C(t *testing.T) {
	tb := testbot.NewQQBot()
	tb.Engine().OnEventKind(platform.EventKindPrivateMessage, context.OnCommand("/ping")).Handle(func(ctx *context.Context) error {
		ctx.Reply(platform.TextMessage("pong"))
		return err
	})
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	tb.SendPlatformC2C("user42", "/ping")
	tb.AssertPlatformReplied(t, "pong")
}
func TestBot_AssertNotReplied(t *testing.T) {
	tb := testbot.NewQQBot()
	if err := tb.Start(); err != nil {
		t.Fatal(err)
	}
	// 没有注册任何 handler，发送消息后不应产生任何回复
	tb.SendPlatformC2C("user1", "no handler for this")
	if got := len(tb.SenderAPI().Sent()); got != 0 {
		t.Errorf("expected no replies for unhandled message, got %d", got)
	}
}
func TestBot_AssertSentCount(t *testing.T) {
	tb := testbot.NewQQBot()
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnKeyword("ping")).Handle(func(ctx *context.Context) error {
		ctx.Reply(platform.TextMessage("pong"))
		return err
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
	tb := testbot.NewQQBot()
	fired := false
	// GroupAddRobot 在 platform/qq/event.go 中映射为 EventKindBotAdded（机器人被加入群组）。
	tb.Engine().OnEventKind(platform.EventKindBotAdded).Handle(func(ctx *context.Context) error {
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
	tb := testbot.NewQQBot()
	tb.Engine().OnEventKind(platform.EventKindGroupMessage, context.OnCommand("/hi")).Handle(func(ctx *context.Context) error {
		ctx.Reply(platform.TextMessage("hi"))
		return err
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
