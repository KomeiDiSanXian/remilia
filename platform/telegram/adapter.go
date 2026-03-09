// Package telegram 是 Telegram Bot 平台的 platform.PlatformAdapter 骨架实现。
//
// 当前为占位实现，等待社区贡献完整的 Telegram Bot API 集成。
//
// 要实现完整的 Telegram 适配器，需要：
//  1. 引入 Telegram Bot SDK（如 github.com/go-telegram-bot-api/telegram-bot-api/v5）
//  2. 实现 Adapter.Start() 使用 Long Polling 或 Webhook 接收更新
//  3. 实现 sender.Send() 调用 sendMessage API
//  4. 将 Telegram Update 类型映射到 platform.EventKind
package telegram

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "telegram"

// Adapter Telegram 平台适配器（骨架）
type Adapter struct{}

// NewAdapter 创建 Telegram 适配器（占位）
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("telegram adapter: not yet implemented")
}

// noopSender 占位发送器
type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("telegram sender: not yet implemented")
}

// telegramEvent Telegram 事件占位
type telegramEvent struct{}

func (e *telegramEvent) Platform() string          { return PlatformID }
func (e *telegramEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *telegramEvent) RawType() string           { return "" }
func (e *telegramEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *telegramEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *telegramEvent) Content() string           { return "" }
func (e *telegramEvent) Timestamp() time.Time      { return time.Time{} }
func (e *telegramEvent) RawPayload() any           { return nil }
