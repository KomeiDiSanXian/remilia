package qq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// WebhookServerAdapter 是面向 QQ Webhook 机器人的 platform.Adapter 实现。
//
// 本类型仅作为协调器（thin coordinator），将两个独立职责分别委托给：
//   - WebhookService：HTTP 服务器 + Token 管理 + 事件流（见 webhook_conn.go）
//   - qq.Adapter：事件分发 worker pool（见 adapter.go）
//
// 这使每个组件的代码量和关注点都显著缩减，便于单独测试和替换。
//
// 典型用法：
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
type WebhookServerAdapter struct {
	svc     *WebhookService
	adapter *Adapter
}

// Platform 实现 platform.Adapter。
func (a *WebhookServerAdapter) Platform() string { return PlatformID }

// Sender 实现 platform.Adapter；委托给 WebhookService（持有 OpenAPI client）。
func (a *WebhookServerAdapter) Sender() platform.Sender { return a.svc.Sender() }

// Capabilities 实现 platform.Adapter。
func (a *WebhookServerAdapter) Capabilities() platform.Capabilities { return qqCapabilities() }

// IsRunning 实现 platform.Adapter；委托给内部 Adapter 的运行状态。
func (a *WebhookServerAdapter) IsRunning() bool { return a.adapter.IsRunning() }

// WithAPI 注入外部 QQ OpenAPI client，委托给底层 WebhookService。
//
// 支持链式调用：adapter.WithAPI(api).Start(ctx, handler)
func (a *WebhookServerAdapter) WithAPI(api openapi.OpenAPI) *WebhookServerAdapter {
	a.svc.WithAPI(api)
	return a
}

// NewWebhookServerAdapter 使用默认配置创建 WebhookServerAdapter。
//
// 参数：
//   - addr: HTTP 监听地址，例如 ":8080"
//   - botInfo: 机器人信息（nil 则不自动创建 Token Manager）
func NewWebhookServerAdapter(addr string, botInfo *dto.BotInfo) *WebhookServerAdapter {
	return NewWebhookServerAdapterWithConfig(addr, botInfo, config.WebhookConfig{
		WorkerCount: 0,   // 0 = runtime.NumCPU()
		EventBuffer: 100, // 默认缓冲区
	})
}

// SimpleWebhookAdapter 在指定端口创建最简 Webhook 适配器（不含 BotInfo）。
//
// 适合快速原型；不支持主动 API 调用。
func SimpleWebhookAdapter(port int) *WebhookServerAdapter {
	return NewWebhookServerAdapter(fmt.Sprintf(":%d", port), nil)
}

// NewWebhookServerAdapterWithConfig 使用指定配置创建 WebhookServerAdapter。
func NewWebhookServerAdapterWithConfig(addr string, botInfo *dto.BotInfo, cfg config.WebhookConfig) *WebhookServerAdapter {
	svc := NewWebhookServiceWithConfig(addr, botInfo, cfg)
	adapter := NewAdapter(svc, nil)
	logger.Infof("[WebhookServerAdapter] Started, buffer=%d", svc.bufferSize)
	return &WebhookServerAdapter{svc: svc, adapter: adapter}
}

// Start 实现 platform.Adapter。
//
// 先启动 HTTP 服务器（WebhookService.start，非阻塞），
// 再运行事件分发循环（qq.Adapter.Start，阻塞直到 ctx 取消或事件流关闭）。
func (a *WebhookServerAdapter) Start(ctx context.Context, handler func(platform.Event)) error {
	// 启动 HTTP 服务器（非阻塞）
	if err := a.svc.start(ctx); err != nil {
		return err
	}
	// svc.start() 可能创建了 API 客户端，同步到内部 adapter
	a.adapter.WithAPI(a.svc.API())
	// 运行事件分发循环（阻塞）
	return a.adapter.Start(ctx, handler)
}

// Stop 实现 platform.Adapter。
//
// 先停止事件分发（Adapter.Stop），再关闭 HTTP 服务器（WebhookService.stop）。
// 两个错误会被合并返回。
func (a *WebhookServerAdapter) Stop(ctx context.Context) error {
	err1 := a.adapter.Stop(ctx)
	err2 := a.svc.stop(ctx)
	return errors.Join(err1, err2)
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

func (a *WebhookServerAdapter) BotID() string   { return a.adapter.BotID() }
func (a *WebhookServerAdapter) BotName() string { return a.adapter.BotName() }

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *WebhookServerAdapter) HealthDetail() map[string]any {
	detail := a.adapter.HealthDetail()
	detail["token_ready"] = a.svc.TokenReady()
	if expiresAt := a.svc.TokenExpiresAt(); !expiresAt.IsZero() {
		detail["token_expires_at"] = expiresAt.Format(time.RFC3339)
	}
	detail["token_server"] = "https://bots.qq.com/app/getAppAccessToken"
	return detail
}

// ── WebSocket 适配器工厂 ──────────────────────────────────────────────────────

// WSAdapter 是基于 WebSocket 的 QQ 适配器。
// 它管理 WebSocket 连接、Token 生命周期和事件分发。
//
// 典型用法：
//
//	adapter := qq.NewWSAdapter(botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
type WSAdapter struct {
	tokenMgr *token.Manager
	api      openapi.OpenAPI
	ws       *WSConn
	adapter  *Adapter
}

// NewWSAdapter 创建基于 WebSocket 的 QQ 适配器（订阅所有事件类型）。
//
// 内部自动创建 Token 管理器、OpenAPI 客户端和 WebSocket 连接管理器，
// 是开箱即用的 WebSocket 接入方式。
//
// 典型用法：
//
//	adapter := qq.NewWSAdapter(botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
func NewWSAdapter(botInfo *dto.BotInfo) *WSAdapter {
	tokenMgr := token.NewManager(botInfo)
	api := openapi.New(tokenMgr)
	ws := NewWSConn(api, tokenMgr)
	adapter := NewAdapter(ws, api)
	return &WSAdapter{tokenMgr: tokenMgr, api: api, ws: ws, adapter: adapter}
}

// NewWSAdapterWithIntents 创建指定事件订阅的 WebSocket 适配器。
//
// intents 控制订阅哪些事件类型。仅需部分事件时可使用此构造器减少不必要的事件流量。
// 例如只收群消息：qq.NewWSAdapterWithIntents(botInfo, qq.IntentGroupAndC2C)
func NewWSAdapterWithIntents(botInfo *dto.BotInfo, intents Intents) *WSAdapter {
	tokenMgr := token.NewManager(botInfo)
	api := openapi.New(tokenMgr)
	ws := NewWSConnWithIntents(api, tokenMgr, intents)
	adapter := NewAdapter(ws, api)
	return &WSAdapter{tokenMgr: tokenMgr, api: api, ws: ws, adapter: adapter}
}

// Platform 返回平台标识符 "qq"。
func (a *WSAdapter) Platform() string { return PlatformID }

// Sender 返回 QQ 消息发送器。
func (a *WSAdapter) Sender() platform.Sender {
	if s := a.adapter.Sender(); s != nil {
		return s
	}
	return &platform.NoopSender{}
}

// Capabilities 返回 QQ 平台支持的特性集合。
func (a *WSAdapter) Capabilities() platform.Capabilities { return qqCapabilities() }

// IsRunning 返回适配器当前是否处于运行状态。
func (a *WSAdapter) IsRunning() bool { return a.adapter.IsRunning() }

// BotID 返回机器人在 QQ 平台的唯一标识符。
func (a *WSAdapter) BotID() string { return a.adapter.BotID() }

// BotName 返回机器人的 QQ 昵称。
func (a *WSAdapter) BotName() string { return a.adapter.BotName() }

// Start 启动 WebSocket 连接和事件分发（阻塞）。
//
// 先启动 WebSocket 后台连接，再运行事件分发循环。
// 直到 ctx 被取消或发生致命错误前会一直阻塞。
func (a *WSAdapter) Start(ctx context.Context, handler func(platform.Event)) error {
	if err := a.ws.Start(ctx); err != nil {
		return fmt.Errorf("ws start: %w", err)
	}
	return a.adapter.Start(ctx, handler)
}

// Stop 优雅关闭适配器。
//
// 停止顺序：事件分发 → WebSocket 连接 → Token 管理器。
// 多个错误会合并返回。
func (a *WSAdapter) Stop(ctx context.Context) error {
	err1 := a.adapter.Stop(ctx)
	err2 := a.ws.Stop(ctx)
	a.tokenMgr.Stop()
	return errors.Join(err1, err2)
}

// HealthDetail 返回适配器的详细健康检查信息。
func (a *WSAdapter) HealthDetail() map[string]any {
	detail := a.adapter.HealthDetail()
	detail["token_ready"] = a.tokenMgr.Ready()
	return detail
}

var (
	_ platform.Adapter        = (*WSAdapter)(nil)
	_ platform.BotIdentity    = (*WSAdapter)(nil)
	_ platform.HealthDetailer = (*WSAdapter)(nil)
)

// ── 编译期接口断言 ────────────────────────────────────────────────────────────

var (
	_ platform.Adapter        = (*WebhookServerAdapter)(nil)
	_ platform.BotIdentity    = (*WebhookServerAdapter)(nil)
	_ platform.HealthDetailer = (*WebhookServerAdapter)(nil)
)
