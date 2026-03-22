package discord

import "github.com/KomeiDiSanXian/remilia/platform"

// ────────────────────────────────────────────────────────────────────────────
// Interaction Token Keys
// ────────────────────────────────────────────────────────────────────────────

// Discord interaction token keys stored in platform.ChatInfo.Tokens.
//
// The sender reads these to dispatch via the interaction response API instead
// of the regular channel message API.
//
// Example usage in a handler (automatic via ctx.Reply):
//
//	ctx.Reply(platform.TextMessage("pong"))
//
// Manual access (advanced):
//
//	if id := event.Chat().Tokens[discord.TokenInteractionID]; id != "" {
//	    // This event is a Discord interaction
//	}
const (
	// TokenInteractionID is the Discord interaction ID.
	//
	// Used to look up the cached *discordgo.Interaction from the sender's
	// store when dispatching a response via the Interactions API.
	TokenInteractionID = "interaction_id"

	// TokenInteractionToken is the Discord interaction token.
	//
	// Valid for 15 minutes after the interaction was created.
	// Used for follow-up messages (FollowupMessageCreate).
	TokenInteractionToken = "interaction_token"
)

// ────────────────────────────────────────────────────────────────────────────
// MessageExtra
// ────────────────────────────────────────────────────────────────────────────

// MessageExtra holds Discord-specific message send options.
//
// Inject with [ApplyExtra]; the Discord Sender retrieves it via [extractExtra].
//
// Example:
//
//	msg := platform.TextMessage("secret").
//	    Then(discord.ApplyExtra(discord.MessageExtra{Ephemeral: true}))
//
//	// Or wrap explicitly:
//	msg = discord.ApplyExtra(msg, discord.MessageExtra{Ephemeral: true})
type MessageExtra struct {
	// TTS enables text-to-speech playback for the message.
	TTS bool

	// Ephemeral makes the message visible only to the user who triggered the
	// interaction. Only effective for interaction responses and follow-ups.
	Ephemeral bool

	// SuppressEmbeds prevents Discord from auto-generating link previews.
	SuppressEmbeds bool

	// AllowedMentions controls which @-mentions are actually notified.
	//
	// Nil = Discord default (all referenced mentions are pinged).
	// Set to an empty value to suppress all mentions.
	AllowedMentions *AllowedMentions
}

// AllowedMentions controls which @-mentions are parsed and notify users.
//
// See https://discord.com/developers/docs/resources/message#allowed-mentions-object
type AllowedMentions struct {
	// Parse is the list of mention types to actively parse.
	//
	// Allowed values: "roles", "users", "everyone".
	// Mutually exclusive with Roles/Users (don't set Parse["roles"] and Roles simultaneously).
	Parse []string

	// Roles is an explicit allow-list of role IDs to mention (up to 100).
	Roles []string

	// Users is an explicit allow-list of user IDs to mention (up to 100).
	Users []string

	// RepliedUser controls whether the replied-to user is mentioned when
	// using a message reference (reply). Default: false.
	RepliedUser bool
}

// discordExtraKey is the private key used to store MessageExtra inside
// platform.OutboundMessage.Extra, namespaced to avoid collisions.
const discordExtraKey = "__discord_message_extra__"

// ApplyExtra injects Discord-specific options into an OutboundMessage.
//
// Returns a new message; the original is not modified.
//
// Example:
//
//	msg = discord.ApplyExtra(msg, discord.MessageExtra{Ephemeral: true})
func ApplyExtra(msg platform.OutboundMessage, extra MessageExtra) platform.OutboundMessage {
	return msg.WithExtra(discordExtraKey, extra)
}

// extractExtra retrieves Discord-specific options from an OutboundMessage.
//
// Returns zero-value MessageExtra if no extra was injected or the type
// does not match.
func extractExtra(msg platform.OutboundMessage) MessageExtra {
	if msg.Extra == nil {
		return MessageExtra{}
	}
	v, ok := msg.Extra[discordExtraKey]
	if !ok {
		return MessageExtra{}
	}
	e, _ := v.(MessageExtra)
	return e
}
