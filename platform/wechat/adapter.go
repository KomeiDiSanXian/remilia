// Package wechat 是微信公众号/企业微信平台的 platform.PlatformAdapter 骨架实现。
//
// 当前为占位实现，等待社区贡献完整的微信消息处理集成。
//
// 要实现完整的微信适配器，需要：
//  1. 实现微信 XML 消息解析（公众号 Webhook 消息格式）
//  2. 实现消息签名验证（Token + Timestamp + Nonce）
//  3. 实现 sender.Send() 调用微信客服消息 API 或模板消息 API
//  4. 将微信消息类型映射到 platform.EventKind
package wechat

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "wechat"

// Adapter 微信平台适配器（骨架）
type Adapter struct{}

// NewAdapter 创建微信适配器（占位）
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("wechat adapter: not yet implemented")
}

// noopSender 占位发送器
type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("wechat sender: not yet implemented")
}

// wechatEvent 微信事件占位
type wechatEvent struct{}

func (e *wechatEvent) Platform() string          { return PlatformID }
func (e *wechatEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *wechatEvent) RawType() string           { return "" }
func (e *wechatEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *wechatEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *wechatEvent) Content() string           { return "" }
func (e *wechatEvent) Timestamp() time.Time      { return time.Time{} }
func (e *wechatEvent) RawPayload() any           { return nil }
