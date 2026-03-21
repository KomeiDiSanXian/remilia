// Package telegram is the platform.Adapter skeleton for Telegram.
//
// Currently a placeholder — waiting for community contribution of a full
// Telegram Bot API integration.
//
// Implementers should refer to platform/qq/event.go as a reference for
// wrapping a platform SDK event into platform.Event.
package telegram

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "telegram"

// Capabilities declares Telegram platform feature capabilities.
var Capabilities = platform.Capabilities{
	Markdown:        true,
	Buttons:         true,
	MultiAttachment: true,
	MessageEdit:     true,
	MessageDelete:   true,
	Embeds:          false,
	FileUpload:      true,
	GuildSupport:    false,
}

// Adapter is the Telegram platform adapter skeleton.
type Adapter struct{}

// NewAdapter creates a Telegram adapter (placeholder).
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string                    { return PlatformID }
func (a *Adapter) Sender() platform.Sender             { return &platform.NoopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error         { return nil }
func (a *Adapter) Capabilities() platform.Capabilities { return Capabilities }
func (a *Adapter) IsRunning() bool                     { return false }

// Start implements platform.Adapter (not yet implemented).
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("telegram adapter: not yet implemented")
}
