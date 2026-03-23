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

// Adapter is the WeChat platform adapter skeleton.
type Adapter struct{}

// NewAdapter creates a WeChat adapter (placeholder).
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string { return PlatformID }
func (a *Adapter) Sender() platform.Sender {
	return &platform.NoopSender{}
}
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }

// Capabilities returns WeChat platform feature capabilities.
//
// 使用方法而非包级变量，保留运行时动态更新的能力。
// 量化限制目前均为 0（未公开或因产品形态差异难以统一）。
func (a *Adapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        false,
		Buttons:         true, // 模板消息/卡片消息支持
		MultiAttachment: false,
		MessageEdit:     false,
		MessageDelete:   false,
		Embeds:          false,
		FileUpload:      true,
		GuildSupport:    false,
		Reactions:       false,
		ThreadReply:     false,
		TypingIndicator: false,
		MentionAll:      false,
		VoiceChannel:    false,
	}
}

func (a *Adapter) IsRunning() bool { return false }

// Start implements platform.Adapter (not yet implemented).
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("wechat adapter: not yet implemented")
}
