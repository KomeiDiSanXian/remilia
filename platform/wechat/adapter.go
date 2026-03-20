// Package wechat is the platform.Adapter skeleton for WeChat Work / WeChat Official Account.
//
// Currently, a placeholder — waiting for community contribution of a full
// WeChat Bot API integration.
//
// Implementers should refer to platform/qq/event.go as a reference for
// wrapping a platform SDK event into platform.Event.
package wechat

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "wechat"

// Capabilities declares WeChat platform feature capabilities.
var Capabilities = platform.Capabilities{
	Markdown:        false,
	Buttons:         true, // 模板消息/卡片消息支持
	MultiAttachment: false,
	MessageEdit:     false,
	MessageDelete:   false,
	Embeds:          false,
	FileUpload:      true,
	GuildSupport:    false,
}

// Adapter is the WeChat platform adapter skeleton.
type Adapter struct{}

// NewAdapter creates a WeChat adapter (placeholder).
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string                    { return PlatformID }
func (a *Adapter) Sender() platform.Sender             { return &platform.NoopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error         { return nil }
func (a *Adapter) Capabilities() platform.Capabilities { return Capabilities }

// StartPlatform implements platform.Adapter (not yet implemented).
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("wechat adapter: not yet implemented")
}
