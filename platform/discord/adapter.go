// Package discord is the platform.PlatformAdapter skeleton for Discord.
//
// Currently, a placeholder — waiting for community contribution of a full
// Discord Bot API integration.
package discord

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "discord"

// Adapter is the Discord platform adapter skeleton.
type Adapter struct{}

// NewAdapter creates a Discord adapter (placeholder).
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }

// StartPlatform implements platform.PlatformAdapter (not yet implemented).
func (a *Adapter) StartPlatform(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("discord adapter: not yet implemented")
}

type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("discord sender: not yet implemented")
}

type discordEvent struct{}

func (e *discordEvent) Platform() string          { return PlatformID }
func (e *discordEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *discordEvent) RawType() string           { return "" }
func (e *discordEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *discordEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *discordEvent) Content() string           { return "" }
func (e *discordEvent) Timestamp() time.Time      { return time.Time{} }
func (e *discordEvent) RawPayload() any           { return nil }
