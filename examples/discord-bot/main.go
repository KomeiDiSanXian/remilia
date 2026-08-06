// Discord bot example demonstrating both connection methods.
//
// # Method 1 — Gateway (WebSocket, default)
//
//	go run main.go -method gateway -token YOUR_BOT_TOKEN
//
// # Method 2 — HTTP Interactions Endpoint
//
//	go run main.go -method interactions -token YOUR_BOT_TOKEN \
//	    -pubkey YOUR_APP_PUBLIC_KEY -addr :8080
//
// # Required Bot Permissions / Intents
//
// In the Discord Developer Portal:
//   - Enable "Message Content Intent" (for reading message content)
//   - Enable "Server Members Intent" (for member events)
//   - Invite the bot with Scopes: bot, applications.commands
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	remilia "github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/bwmarrin/discordgo"
)

func main() {
	method := flag.String("method", "gateway", "Connection method: 'gateway' or 'interactions'")
	token := flag.String("token", os.Getenv("DISCORD_BOT_TOKEN"), "Discord bot token")
	pubkey := flag.String("pubkey", os.Getenv("DISCORD_PUBLIC_KEY"), "Discord app public key (interactions mode)")
	addr := flag.String("addr", ":8080", "HTTP listen address (interactions mode)")
	flag.Parse()

	if *token == "" {
		log.Fatal("Bot token is required. Set DISCORD_BOT_TOKEN or use -token flag.")
	}

	var adapter platform.Adapter

	switch *method {
	case "gateway":
		adapter = mustGatewayAdapter(*token)
	case "interactions":
		if *pubkey == "" {
			log.Fatal("Public key required for interactions mode. Set DISCORD_PUBLIC_KEY or -pubkey.")
		}
		adapter = mustInteractionsAdapter(*token, *pubkey, *addr)
	default:
		log.Fatalf("Unknown method %q. Use 'gateway' or 'interactions'.", *method)
	}

	// Build the bot.
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("discord-example-bot").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// Register event handlers via the engine.
	registerHandlers(bot.Engine())

	log.Printf("Starting Discord bot (method=%s)...", *method)
	if err := bot.Start(); err != nil {
		log.Fatalf("Bot start error: %v", err)
	}

	log.Println("Bot is running. Press Ctrl+C to stop.")
	bot.WaitForShutdown()
	log.Println("Bot stopped.")
}

// ─── Connection helpers ──────────────────────────────────────────────────────

// mustGatewayAdapter creates a Gateway (WebSocket) adapter.
//
// The Gateway is the primary method for most bots. It maintains a persistent
// WebSocket connection to Discord and delivers all subscribed events in real time.
//
//   - Supports: messages, reactions, guild events, member events, interactions
//   - Intents: must match what you enabled in the Developer Portal
func mustGatewayAdapter(token string) platform.Adapter {
	cfg := discord.GatewayConfig{
		Token: token,
		// DefaultIntents includes Guilds, GuildMessages, DirectMessages, Reactions, etc.
		// Add MessageContent and GuildMembers (both are privileged, require Developer Portal toggle).
		Intents: discord.DefaultIntents |
			discordgo.IntentsMessageContent | // read message text
			discordgo.IntentsGuildMembers, // guild member join/leave
		ShouldReconnect: true,
		WorkerCount:     4,
	}

	adapter, err := discord.NewGatewayAdapter(cfg)
	if err != nil {
		log.Fatalf("Failed to create Gateway adapter: %v", err)
	}
	return adapter
}

// mustInteractionsAdapter creates an HTTP Interactions adapter.
//
// Use this for serverless deployments, or when your bot only handles slash
// commands and component interactions (no background event stream needed).
//
// Setup:
//  1. Your server must have a publicly accessible HTTPS URL.
//  2. Set "Interactions Endpoint URL" in the Discord Developer Portal.
//  3. Provide your app's public key (from General Information page).
func mustInteractionsAdapter(token, pubkey, addr string) platform.Adapter {
	cfg := discord.InteractionsConfig{
		Addr:        addr,
		PublicKey:   pubkey,
		Token:       token, // used for follow-up messages
		Path:        "/interactions",
		AutoDefer:   true, // acknowledge immediately; respond asynchronously
		WorkerCount: 4,
	}

	adapter, err := discord.NewInteractionsAdapter(cfg)
	if err != nil {
		log.Fatalf("Failed to create Interactions adapter: %v", err)
	}
	return adapter
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func registerHandlers(eng *engine.Engine) {
	// Guild message — !ping command
	eng.OnCommand(string(platform.EventKindGuildMessage), "!ping").
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event != nil && event.Sender().IsBot {
				return nil // ignore other bots
			}
			ctx.Reply(platform.TextMessage("Pong! 🏓"))
			return nil
		})

	// Guild message — !embed command
	eng.OnCommand(string(platform.EventKindGuildMessage), "!embed").
		Handle(func(ctx *eventctx.Context) error {
			ctx.Reply(platform.TextMessage("").WithEmbeds(platform.Embed{
				Title:       "Example Embed",
				Description: "Sent from **remilia** on Discord.",
				Color:       0x5865F2, // Discord blurple
				Fields: []platform.EmbedField{
					{Name: "Library", Value: "remilia", Inline: true},
					{Name: "Platform", Value: "Discord", Inline: true},
				},
				FooterText: "Powered by remilia",
			}))
			return nil
		})

	// Guild message — !buttons command
	eng.OnCommand(string(platform.EventKindGuildMessage), "!buttons").
		Handle(func(ctx *eventctx.Context) error {
			ctx.Reply(platform.TextMessage("Choose an option:").WithButtons(
				platform.Button{
					ID: "btn_yes", Label: "Yes ✅",
					Style: platform.ButtonStylePrimary, Row: 1,
				},
				platform.Button{
					ID: "btn_no", Label: "No ❌",
					Style: platform.ButtonStyleDanger, Row: 1,
				},
				platform.Button{
					Label: "Discord Docs", Style: platform.ButtonStyleLink,
					URL: "https://discord.com/developers/docs",
				},
			))
			return nil
		})

	// Guild message — !reply command
	eng.OnCommand(string(platform.EventKindGuildMessage), "!reply").
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event == nil {
				return nil
			}
			ctx.Reply(platform.TextMessage("Replying to your message!").WithReply(event.ID()))
			return nil
		})

	// DM handler — echo back any DM.
	eng.OnEventKind(platform.EventKindPrivateMessage).
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event != nil {
				fmt.Printf("[DM] %s: %s\n", event.Sender().DisplayName, platform.Content(event))
			}
			ctx.Reply(platform.TextMessage("Hi! Got your DM."))
			return nil
		})

	// Interaction handler (button clicks, slash commands, modals).
	eng.OnEventKind(platform.EventKindInteraction).
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event == nil {
				return nil
			}
			content := platform.Content(event)
			fmt.Printf("[Interaction] %s: %s\n", event.Sender().DisplayName, content)

			var msg platform.OutboundMessage
			switch content {
			case "btn_yes":
				msg = discord.ApplyExtra(
					platform.TextMessage("You clicked Yes! ✅"),
					discord.MessageExtra{Ephemeral: true},
				)
			case "btn_no":
				msg = discord.ApplyExtra(
					platform.TextMessage("You clicked No! ❌"),
					discord.MessageExtra{Ephemeral: true},
				)
			default:
				msg = platform.TextMessage(fmt.Sprintf("Interaction: `%s`", content))
			}
			ctx.Reply(msg)
			return nil
		})

	// System events (READY, RESUMED).
	eng.OnEventKind(platform.EventKindSystem).
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event != nil {
				fmt.Printf("[System] %s\n", event.Kind())
			}
			return nil
		})

	// Bot added to a guild.
	eng.OnEventKind(platform.EventKindBotAdded).
		Handle(func(ctx *eventctx.Context) error {
			event := ctx.GetPlatformEvent()
			if event != nil {
				g := event.Chat()
				fmt.Printf("[System] Bot added to guild: %s (%s)\n", g.Name, g.ID)
			}
			return nil
		})
}
