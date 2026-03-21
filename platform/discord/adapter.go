// Package discord is the platform.Adapter skeleton for Discord.
//
// Currently a placeholder — waiting for community contribution of a full
// Discord Bot API integration.
//
// Implementers should refer to platform/qq/event.go as a reference for
// wrapping a platform SDK event into platform.Event.
package discord

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "discord"

// Capabilities declares Discord platform feature capabilities.
var Capabilities = platform.Capabilities{
	Markdown:        true,
	Buttons:         true,
	MultiAttachment: true,
	MessageEdit:     true,
	MessageDelete:   true,
	Embeds:          true,
	FileUpload:      true,
	GuildSupport:    true,
	Reactions:       true,
	ThreadReply:     true,
	TypingIndicator: true,
	MentionAll:      true,
	VoiceChannel:    true,
}

// Adapter is the Discord platform adapter skeleton.
type Adapter struct{}

// NewAdapter creates a Discord adapter (placeholder).
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string                    { return PlatformID }
func (a *Adapter) Sender() platform.Sender             { return &platform.NoopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error         { return nil }
func (a *Adapter) Capabilities() platform.Capabilities { return Capabilities }
func (a *Adapter) IsRunning() bool                     { return false }

// Start implements platform.Adapter (not yet implemented).
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("discord adapter: not yet implemented")
}
