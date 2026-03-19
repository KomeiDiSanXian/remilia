// Package wechat is the platform.PlatformAdapter skeleton for WeChat.
package wechat

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "wechat"

type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }

func (a *Adapter) StartPlatform(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("wechat adapter: not yet implemented")
}

type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("wechat sender: not yet implemented")
}

type wechatEvent struct{}

func (e *wechatEvent) Platform() string          { return PlatformID }
func (e *wechatEvent) ID() string                { return "" }
func (e *wechatEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *wechatEvent) RawType() string           { return "" }
func (e *wechatEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *wechatEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *wechatEvent) Content() string           { return "" }
func (e *wechatEvent) Timestamp() time.Time      { return time.Time{} }
func (e *wechatEvent) RawPayload() any           { return nil }
