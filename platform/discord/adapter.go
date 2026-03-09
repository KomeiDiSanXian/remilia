// Package discord 是 Discord 平台的 platform.PlatformAdapter 骨架实现。
//
// 当前为占位实现，等待社区贡献完整的 Discord Bot API 集成。
//
// 要实现完整的 Discord 适配器，需要：
//  1. 引入 Discord Go SDK（如 github.com/bwmarrin/discordgo）
//  2. 实现 Adapter.Start() 建立 WebSocket 连接并接收事件
//  3. 实现 sender.Send() 调用 Discord REST API 发送消息
//  4. 将 Discord 事件类型映射到 platform.EventKind
package discord

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

const PlatformID = "discord"

// Adapter Discord 平台适配器（骨架）
type Adapter struct{}

// NewAdapter 创建 Discord 适配器（占位）
func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Platform() string            { return PlatformID }
func (a *Adapter) Sender() platform.Sender     { return &noopSender{} }
func (a *Adapter) Stop(_ stdctx.Context) error { return nil }
func (a *Adapter) Start(_ stdctx.Context, _ func(platform.Event)) error {
	return fmt.Errorf("discord adapter: not yet implemented")
}

// noopSender 占位发送器
type noopSender struct{}

func (s *noopSender) Send(_ stdctx.Context, _ string, _ platform.OutboundMessage) error {
	return fmt.Errorf("discord sender: not yet implemented")
}

// discordEvent Discord 事件占位
type discordEvent struct{}

func (e *discordEvent) Platform() string          { return PlatformID }
func (e *discordEvent) Kind() platform.EventKind  { return platform.EventKindUnknown }
func (e *discordEvent) RawType() string           { return "" }
func (e *discordEvent) Sender() platform.UserInfo { return platform.UserInfo{} }
func (e *discordEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{} }
func (e *discordEvent) Content() string           { return "" }
func (e *discordEvent) Timestamp() time.Time      { return time.Time{} }
func (e *discordEvent) RawPayload() any           { return nil }
