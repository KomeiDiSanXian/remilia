package qq

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// WebhookServerAdapter 是面向 QQ Webhook 机器人的 platform.Adapter 实现。
//
// D4：本类型现在仅作为协调器（thin coordinator），将两个独立职责分别委托给：
//   - WebhookConn：HTTP 服务器 + Token 管理 + 事件流（见 webhook_conn.go）
//   - qq.Adapter：事件分发 worker pool（见 adapter.go）
//
// 这使每个组件的代码量和关注点都显著缩减，便于单独测试和替换。
//
// 典型用法：
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
type WebhookServerAdapter struct {
	conn    *WebhookConn
	adapter *Adapter
}

// Platform 实现 platform.Adapter。
func (a *WebhookServerAdapter) Platform() string { return PlatformID }

// Sender 实现 platform.Adapter；委托给 WebhookConn（持有 OpenAPI client）。
func (a *WebhookServerAdapter) Sender() platform.Sender { return a.conn.Sender() }

// Capabilities 实现 platform.Adapter。
func (a *WebhookServerAdapter) Capabilities() platform.Capabilities { return QQCapabilities }

// IsRunning 实现 platform.Adapter；委托给内部 Adapter 的运行状态。
func (a *WebhookServerAdapter) IsRunning() bool { return a.adapter.IsRunning() }

// WithAPI 注入外部 QQ OpenAPI client，委托给底层 WebhookConn。
//
// 支持链式调用：adapter.WithAPI(api).Start(ctx, handler)
func (a *WebhookServerAdapter) WithAPI(api openapi.OpenAPI) *WebhookServerAdapter {
	a.conn.WithAPI(api)
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
	workers := cfg.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	conn := NewWebhookConnWithConfig(addr, botInfo, cfg)
	adapter := NewAdapter(conn, nil).WithWorkers(workers)
	logger.Infof("[WebhookServerAdapter] Config: workers=%d, buffer=%d", workers, conn.bufferSize)
	return &WebhookServerAdapter{conn: conn, adapter: adapter}
}

// Start 实现 platform.Adapter。
//
// 先启动 HTTP 服务器（WebhookConn.start，非阻塞），
// 再运行事件分发循环（qq.Adapter.Start，阻塞直到 ctx 取消或事件流关闭）。
func (a *WebhookServerAdapter) Start(ctx context.Context, handler func(platform.Event)) error {
	// 启动 HTTP 服务器（非阻塞）
	if err := a.conn.start(ctx); err != nil {
		return err
	}
	// 运行事件分发循环（阻塞）
	return a.adapter.Start(ctx, handler)
}

// Stop 实现 platform.Adapter。
//
// 先停止事件分发（Adapter.Stop），再关闭 HTTP 服务器（WebhookConn.stop）。
// 两个错误会被合并返回。
func (a *WebhookServerAdapter) Stop(ctx context.Context) error {
	err1 := a.adapter.Stop(ctx)
	err2 := a.conn.stop(ctx)
	return errors.Join(err1, err2)
}
