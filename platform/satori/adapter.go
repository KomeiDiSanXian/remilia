// Package satori 实现了基于 Satori 协议的 platform.Adapter，
// 可连接任意兼容 Satori 协议的 SDK（如 Chronocat、Lagrange、Koishi 等）。
//
// 快速开始：
//
//	adapter, err := satori.NewAdapter(satori.DefaultConfig(
//	    "http://localhost:5140", // Satori SDK 地址
//	    "chronocat",             // 平台标识符
//	    "1234567890",            // 机器人用户 ID
//	))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
package satori

import (
	stdctx "context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// PlatformID 是 Adapter.Platform() 返回的默认平台标识符。
// 若 Config.Platform 已设置，则使用该值。
const PlatformID = "satori"

// ─────────────────────────────────────────────────────────────────────────────
// Adapter
// ─────────────────────────────────────────────────────────────────────────────

// Adapter 是 Satori 协议的 platform.Adapter 实现。
//
// 它通过 WebSocket（事件订阅）连接到 Satori SDK 服务端，
// 并提供一个 HTTP API 客户端用于发送消息和调用其他 Satori API。
//
// 该 Adapter 自动实现以下可选接口：
//   - platform.RecoverableAdapter（通过 OnDisconnect）
//   - platform.BotIdentity        （通过从 Login 获取 BotID / BotName）
type Adapter struct {
	platform.DisconnectNotifier

	cfg    Config
	client *Client
	sender *satoriSender
	ws     *wsConn

	mu      sync.RWMutex
	running bool
	cancel  stdctx.CancelFunc

	starting atomic.Bool

	// READY 信令到达后填充的登录信息
	loginMu sync.RWMutex
	login   *Login

	// READY / META 信令到达后填充的代理路由 URL 列表（实验性）
	proxyMu   sync.RWMutex
	proxyURLs []string
}

// NewAdapter 根据给定配置创建一个新的 Satori Adapter。
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client := newClient(cfg)
	sender := newSender(client)

	a := &Adapter{
		cfg:    cfg,
		client: client,
		sender: sender,
	}
	return a, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.Adapter 接口
// ─────────────────────────────────────────────────────────────────────────────

// Platform 返回 Satori 平台标识符。
// 若 Config.Platform 已设置则返回该值，否则返回 "satori"。
func (a *Adapter) Platform() string {
	if a.cfg.Platform != "" {
		return a.cfg.Platform
	}
	return PlatformID
}

// Sender 返回 Satori 消息发送器。
func (a *Adapter) Sender() platform.Sender { return a.sender }

// satoriCapabilities 返回 Satori 平台的功能特性集合。
func satoriCapabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         true,
		MultiAttachment: true,
		MessageEdit:     true,
		MessageDelete:   true,
		FileUpload:      true,
		GuildSupport:    true,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: false,
		MentionAll:      true,
	}
}

// Capabilities 返回 Satori 平台的功能特性集合。
func (a *Adapter) Capabilities() platform.Capabilities { return satoriCapabilities() }

// IsRunning 当 Adapter 的 WebSocket 循环处于活动状态时返回 true。
func (a *Adapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 建立到 Satori SDK 的 WebSocket 连接并开始处理事件。
// 该方法会阻塞，直到 ctx 被取消或发生致命错误。
//
// 内部流程：
//  1. 创建 wsConn，管理 IDENTIFY / PING-PONG / 会话恢复；
//  2. 将每个收到的 EVENT 在独立 goroutine 中分发给 handler。
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil // 已在启动中
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	ws := newWSConn(a.cfg, a.Platform(), handler, a.NotifyDisconnect)
	// 设置 READY 回调：解析登录信息，更新 BotID/BotName。
	ws.onReady = func(logins []*Login) {
		a.loginMu.Lock()
		defer a.loginMu.Unlock()
		// 优先匹配与配置一致的用户 ID。
		for _, l := range logins {
			if l.User != nil && l.User.ID == a.cfg.UserID {
				a.login = l
				return
			}
		}
		if len(logins) > 0 {
			a.login = logins[0]
		}
	}
	// 设置 META 回调：更新代理路由列表。
	ws.onMeta = func(proxyURLs []string) {
		a.proxyMu.Lock()
		a.proxyURLs = proxyURLs
		a.proxyMu.Unlock()
	}
	a.mu.Lock()
	a.ws = ws
	a.mu.Unlock()

	logger.WithFields(logger.Fields{
		"platform":  a.Platform(),
		"serverURL": a.cfg.ServerURL,
	}).Info("[satori.Adapter] 正在建立 WebSocket 连接")

	err := ws.Run(cancelCtx)
	if err != nil && !errors.Is(err, stdctx.Canceled) && !errors.Is(err, stdctx.DeadlineExceeded) {
		logger.WithFields(logger.Fields{
			"platform": a.Platform(),
		}).WithError(err).Error("[satori.Adapter] WebSocket 循环因错误退出")
	}
	return err
}

// Stop 优雅地关闭 Adapter。
func (a *Adapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	ws := a.ws
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ws != nil {
		ws.Close()
	}
	logger.WithFields(logger.Fields{
		"platform": a.Platform(),
	}).Info("[satori.Adapter] 已停止")
	return nil
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

// BotID 返回从 Satori SDK 登录信息中获取的机器人用户 ID。
// 若尚未收到登录信息，则回退到 cfg.UserID。
func (a *Adapter) BotID() string {
	a.loginMu.RLock()
	defer a.loginMu.RUnlock()
	if a.login != nil && a.login.User != nil {
		return a.login.User.ID
	}
	return a.cfg.UserID
}

// BotName 返回从 Satori SDK 登录信息中获取的机器人显示名称。
func (a *Adapter) BotName() string {
	a.loginMu.RLock()
	defer a.loginMu.RUnlock()
	if a.login != nil && a.login.User != nil && a.login.User.Name != nil {
		return *a.login.User.Name
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 附加辅助方法：访问底层 HTTP 客户端
// ─────────────────────────────────────────────────────────────────────────────

// Client 返回底层的 Satori HTTP API 客户端。
// 调用方可以使用此客户端直接调用标准 platform.Sender 接口未覆盖的 Satori API，
// 包括 UploadCreate、ProxyFetch、MetaGet、InternalCall 等进阶 API。
func (a *Adapter) Client() *Client { return a.client }

// ProxyURLs 返回从 READY 或 META 信令中获取的代理路由 URL 前缀列表（实验性）。
//
// 应用侧可根据此列表判断某资源链接是否需要通过代理路由访问。
// 在 READY 信令到达前，此列表为空。
//
// 参见：https://satori.js.org/zh-CN/advanced/resource.html#proxy-route
func (a *Adapter) ProxyURLs() []string {
	a.proxyMu.RLock()
	defer a.proxyMu.RUnlock()
	if len(a.proxyURLs) == 0 {
		return nil
	}
	result := make([]string, len(a.proxyURLs))
	copy(result, a.proxyURLs)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// 编译期接口断言
// ─────────────────────────────────────────────────────────────────────────────

var (
	_ platform.Adapter            = (*Adapter)(nil)
	_ platform.RecoverableAdapter = (*Adapter)(nil)
	_ platform.BotIdentity        = (*Adapter)(nil)
	_ platform.HealthDetailer     = (*Adapter)(nil)
)

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *Adapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": "websocket",
	}
	a.loginMu.RLock()
	if a.login != nil {
		detail["logged_in"] = true
	}
	a.loginMu.RUnlock()
	return detail
}
