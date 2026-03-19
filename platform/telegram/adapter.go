// Package telegram is the platform.PlatformAdapter skeleton for Telegram.
package telegram

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "telegram"

type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }

func (a *Adapter) StartPlatform(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("telegram adapter: not yet implemented")
}

type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("telegram sender: not yet implemented")
}

type telegramEvent struct{}

func (e *telegramEvent) Platform() string          { return PlatformID }
func (e *telegramEvent) ID() string                { return "" }
func (e *telegramEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *telegramEvent) RawType() string           { return "" }
func (e *telegramEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *telegramEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *telegramEvent) Content() string           { return "" }
func (e *telegramEvent) Timestamp() time.Time      { return time.Time{} }
func (e *telegramEvent) RawPayload() any           { return nil }
