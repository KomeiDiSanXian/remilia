// Package discord is the Discord platform.Adapter implementation.
//
// # Connection Methods
//
// Discord supports two main ways to connect a bot:
//
//  1. Gateway (WebSocket) — The primary method. The bot maintains a persistent
//     WebSocket connection to Discord's Gateway and receives real-time events.
//     Use [NewGatewayAdapter] or [NewAdapter] for this mode.
//
//  2. HTTP Interactions Endpoint — Discord sends POST requests to a URL you
//     configure in the Developer Portal whenever users invoke slash commands,
//     click buttons, or submit modals. No persistent connection is required.
//     Use [NewInteractionsAdapter] for this mode.
//
// # Quick Start (Gateway)
//
//	adapter, err := discord.NewAdapter("BOT_TOKEN")
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
//
// # Quick Start (HTTP Interactions)
//
//	adapter, err := discord.NewInteractionsAdapter(discord.InteractionsConfig{
//	    Addr:      ":8080",
//	    PublicKey: "YOUR_APP_PUBLIC_KEY",
//	    Token:     "BOT_TOKEN",
//	})
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"
)

// ────────────────────────────────────────────────────────────────────────────
// GatewayConfig
// ────────────────────────────────────────────────────────────────────────────

// GatewayConfig holds configuration for the Discord Gateway (WebSocket) adapter.
//
// The Gateway is the primary connection method for Discord bots, providing
// real-time event delivery over a persistent WebSocket connection.
type GatewayConfig struct {
	// Token is the bot token (without "Bot " prefix; it will be added automatically).
	//
	// Required. Obtain your token from https://discord.com/developers/applications
	Token string

	// Intents specifies which Gateway Intents to subscribe to.
	//
	// Intents control which events Discord delivers to your bot.
	// Some intents are "privileged" and must be enabled in the Developer Portal.
	//
	// Default (zero value): [DefaultIntents] (common non-privileged intents).
	//
	// To receive message content, add discordgo.IntentsMessageContent (privileged).
	// To receive guild member events, add discordgo.IntentsGuildMembers (privileged).
	//
	// See https://discord.com/developers/docs/topics/gateway#gateway-intents
	Intents discordgo.Intent

	// WorkerCount is the number of event-processing goroutines.
	//
	// 0 or negative: defaults to runtime.NumCPU().
	WorkerCount int

	// EventBufferSize is the capacity of the internal event channel.
	//
	// Default (zero value): 100.
	EventBufferSize int

	// ShouldReconnect controls whether the session automatically reconnects
	// on unexpected disconnects (network errors, gateway restarts, etc.).
	//
	// Default (zero value): false — set to true for production bots.
	ShouldReconnect bool

	// LargeThreshold sets the member count threshold at which members are
	// no longer sent in GUILD_CREATE. Must be between 50 and 250.
	//
	// Default (zero value): 250 (Discord default).
	LargeThreshold int

	// ShardID is this shard's ID when using sharding (0-indexed).
	//
	// Only relevant when NumShards > 1. Default: 0.
	ShardID int

	// NumShards is the total number of shards when using sharding.
	//
	// Default (zero value): 1 (no sharding).
	// Required once your bot is in 2500+ guilds.
	//
	// See https://discord.com/developers/docs/topics/gateway#sharding
	NumShards int
}

// DefaultIntents returns a set of commonly used non-privileged Gateway Intents.
//
// Includes: Guilds, GuildMessages, DirectMessages, GuildMessageReactions,
// DirectMessageReactions, GuildVoiceStates, GuildScheduledEvents.
//
// To receive message content you must additionally enable:
//
//	cfg.Intents |= discordgo.IntentsMessageContent
//
// (requires "Message Content Intent" to be enabled in Developer Portal).
const DefaultIntents = discordgo.IntentsGuilds |
	discordgo.IntentsGuildMessages |
	discordgo.IntentsDirectMessages |
	discordgo.IntentsGuildMessageReactions |
	discordgo.IntentsDirectMessageReactions |
	discordgo.IntentsGuildVoiceStates |
	discordgo.IntentsGuildScheduledEvents

// AllIntents is a convenience alias for discordgo.IntentsAll.
//
// Use with caution — includes privileged intents (GuildMembers, MessageContent,
// GuildPresences) that must be approved in the Developer Portal for verified bots.
const AllIntents = discordgo.IntentsAll

// setDefaults fills zero-value fields with sensible defaults.
func (c *GatewayConfig) setDefaults() {
	if c.Intents == 0 {
		c.Intents = DefaultIntents |
			discordgo.IntentsMessageContent |
			discordgo.IntentsGuildMembers
	}
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 100
	}
	if c.LargeThreshold <= 0 {
		c.LargeThreshold = 250
	}
	if c.WorkerCount <= 0 {
		c.WorkerCount = 0 // runtime.NumCPU() at Start time
	}
}

// setDefaults fills zero-value fields with sensible defaults.
func (c *InteractionsConfig) setDefaults() {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 100
	}
	if c.AckTimeout <= 0 {
		c.AckTimeout = 2500 * time.Millisecond
	}
	if c.Path == "" {
		c.Path = "/"
	}
	if c.WorkerCount <= 0 {
		c.WorkerCount = 0 // runtime.NumCPU() at Start time
	}
}

// DefaultGatewayConfig returns a GatewayConfig with production-ready defaults.
//
// Enables message content and guild member intents; both are privileged and
// must be toggled on in the Discord Developer Portal for your application.
func DefaultGatewayConfig(token string) GatewayConfig {
	cfg := GatewayConfig{
		Token: token,
		Intents: DefaultIntents |
			discordgo.IntentsMessageContent |
			discordgo.IntentsGuildMembers,
		EventBufferSize: 100,
		ShouldReconnect: true,
		NumShards:       1,
	}
	return cfg
}

// ────────────────────────────────────────────────────────────────────────────
// InteractionsConfig
// ────────────────────────────────────────────────────────────────────────────

// InteractionsConfig holds configuration for the Discord HTTP Interactions adapter.
//
// The Interactions endpoint model allows Discord to call your server over HTTPS
// whenever a user runs a slash command, clicks a button, etc., without
// requiring a persistent WebSocket connection.
//
// To use this mode:
//  1. Host an HTTPS endpoint accessible by Discord.
//  2. Set the "Interactions Endpoint URL" in your app's Developer Portal.
//  3. Provide your app's public key (from the Developer Portal) for signature
//     verification.
//
// See https://discord.com/developers/docs/interactions/receiving-and-responding
type InteractionsConfig struct {
	// Addr is the HTTP listen address, e.g. ":8080" or "0.0.0.0:8443".
	//
	// Required.
	Addr string

	// PublicKey is your Discord application's Ed25519 public key for
	// signature verification, as a hex-encoded string.
	//
	// Required. Found in your application's General Information page
	// at https://discord.com/developers/applications
	PublicKey string

	// Token is the bot token used for sending follow-up messages via the REST API.
	//
	// Optional if the bot only needs to respond to interactions and not send
	// follow-up messages. Omit "Bot " prefix; it will be added automatically.
	Token string

	// Path is the URL path to listen on. Default: "/" (accepts all paths).
	//
	// Set to "/interactions" if you want to mount under a specific path.
	Path string

	// WorkerCount is the number of event-processing goroutines.
	// 0 or negative: defaults to runtime.NumCPU().
	WorkerCount int

	// EventBufferSize is the capacity of the internal event channel.
	// Default (zero value): 100.
	EventBufferSize int

	// AutoDefer controls whether the adapter automatically acknowledges
	// APPLICATION_COMMAND interactions with a deferred response before
	// passing the event to your handler.
	//
	// When true (recommended): Discord receives an immediate acknowledgment
	// ("Bot is thinking...") and your handler has up to 15 minutes to send
	// a follow-up. Use ctx.Reply() as usual; the sender uses the follow-up API.
	//
	// When false: Your handler must respond within 3 seconds or Discord will
	// show "This interaction failed." Suitable for fast handlers only.
	AutoDefer bool

	// AckTimeout is used internally for deferred-response timing (not user-facing).
	// Defaults to 2.5 seconds (safely under Discord's 3-second requirement).
	AckTimeout time.Duration
}
