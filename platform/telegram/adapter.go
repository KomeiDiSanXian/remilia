// Package telegram is the platform.PlatformAdapter skeleton for Telegram.
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
var Capabilities = platform.PlatformCapabilities{
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

func (a *Adapter) Platform() string                            { return PlatformID }
func (a *Adapter) Sender() platform.Sender                     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error                 { return nil }
func (a *Adapter) Capabilities() platform.PlatformCapabilities { return Capabilities }

// StartPlatform implements platform.PlatformAdapter (not yet implemented).
func (a *Adapter) StartPlatform(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("telegram adapter: not yet implemented")
}

type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ platform.OutboundMessage) error {
	return fmt.Errorf("telegram sender: not yet implemented")
}
