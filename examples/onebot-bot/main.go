// Package main demonstrates how to connect the remilia framework to an
// existing OneBot V11 implementation (e.g. go-cqhttp, NapCat, Lagrange).
//
// Before running:
//  1. Start your OneBot V11 implementation (default WS port: 6700).
//  2. Optionally set a TOKEN environment variable matching the access token.
//
// Usage:
//
//	TOKEN=yourtoken go run main.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/onebot"
)

func main() {
	// ── 1. Configure and create the OneBot V11 adapter ──────────────────────
	// Connect to go-cqhttp (or any OneBot V11 impl) via Forward WebSocket.
	wsURL := getenv("ONEBOT_URL", "ws://127.0.0.1:6700")
	token := getenv("TOKEN", "")

	cfg := onebot.DefaultConfig(wsURL)
	cfg.Token = token

	adapter := onebot.NewForwardWSAdapter(cfg)

	// ── 2. Build the engine ─────────────────────────────────────────────────
	eng := engine.NewEngine()

	// ── 3. Register message handlers ────────────────────────────────────────

	// Echo handler: reply to "ping" with "pong" (private message)
	eng.OnEventKind(platform.EventKindPrivateMessage,
		func(ctx *eventctx.Context) bool {
			ev := ctx.GetPlatformEvent()
			return ev != nil && strings.TrimSpace(ev.Content()) == "ping"
		},
	).Handle(func(ctx *eventctx.Context) error {
		_, err := ctx.Reply(platform.TextMessage("pong"))
		return err
	})

	// Group message handler: reply to messages containing "hello"
	eng.OnEventKind(platform.EventKindGroupMessage,
		func(ctx *eventctx.Context) bool {
			ev := ctx.GetPlatformEvent()
			return ev != nil && strings.Contains(ev.Content(), "hello")
		},
	).Handle(func(ctx *eventctx.Context) error {
		ev := ctx.GetPlatformEvent()
		if ev == nil {
			return nil
		}
		senderID := ev.Sender().ID
		msg := platform.TextMessage("Hi there!").WithMentions(senderID)
		_, err := ctx.Reply(msg)
		return err
	})

	// Delete message: reply nothing and delete the triggering message
	eng.OnEventKind(platform.EventKindGroupMessage,
		func(ctx *eventctx.Context) bool {
			ev := ctx.GetPlatformEvent()
			return ev != nil && strings.TrimSpace(ev.Content()) == "!delete"
		},
	).Handle(func(ctx *eventctx.Context) error {
		ev := ctx.GetPlatformEvent()
		sender := ctx.GetPlatformSender()
		if ev == nil || sender == nil {
			return nil
		}
		if deleter, ok := sender.(platform.MessageDeleter); ok {
			chatID := ev.Chat().ID
			msgID := ev.ID()
			return deleter.Delete(context.Background(), chatID, msgID)
		}
		return nil
	})

	// Group management: ban @mentioned user for 1 minute
	eng.OnEventKind(platform.EventKindGroupMessage,
		func(ctx *eventctx.Context) bool {
			ev := ctx.GetPlatformEvent()
			return ev != nil && strings.HasPrefix(ev.Content(), "!ban ")
		},
	).Handle(func(ctx *eventctx.Context) error {
		ev := ctx.GetPlatformEvent()
		sender := ctx.GetPlatformSender()
		if ev == nil || sender == nil {
			return nil
		}
		gm, ok := sender.(platform.GroupManager)
		if !ok {
			return nil
		}
		groupID := ev.Chat().ID
		for _, u := range platform.GetMentions(ev) {
			if err := gm.BanMember(context.Background(), groupID, u.ID, 60*1_000_000_000); err != nil {
				fmt.Fprintf(os.Stderr, "ban error: %v\n", err)
			}
		}
		return nil
	})

	// Auto-approve friend requests and group invites
	eng.OnEventKind(platform.EventKindRequest).Handle(func(ctx *eventctx.Context) error {
		ev := ctx.GetPlatformEvent()
		sender := ctx.GetPlatformSender()
		if ev == nil || sender == nil {
			return nil
		}

		chat := ev.Chat()
		flag := chat.Tokens[onebot.TokenRequestFlag]
		reqType := chat.Tokens[onebot.TokenRequestType]
		if flag == "" {
			return nil
		}

		ih, ok := sender.(platform.InvitationHandler)
		if !ok {
			return nil
		}

		switch reqType {
		case onebot.RequestTypeFriend:
			if err := ih.AcceptFriendRequest(context.Background(), flag); err != nil {
				return err
			}
			fmt.Printf("[bot] accepted friend request flag=%s\n", flag)
		case onebot.RequestTypeGroup:
			subType := chat.Tokens[onebot.TokenRequestSub]
			if subType == onebot.GroupRequestInvite {
				if err := ih.AcceptGroupInvite(context.Background(), flag); err != nil {
					return err
				}
				fmt.Printf("[bot] accepted group invite flag=%s\n", flag)
			}
		}
		return nil
	})

	// ── 4. Build and start the bot ───────────────────────────────────────────
	bot := remilia.NewBot(adapter, eng, remilia.WithName("onebot-example"))

	fmt.Printf("[bot] Starting, connecting to %s\n", wsURL)
	if err := bot.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[bot] Running as %s (%s)\n",
		platform.GetBotName(adapter), platform.GetBotID(adapter))

	bot.WaitForShutdown()
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
