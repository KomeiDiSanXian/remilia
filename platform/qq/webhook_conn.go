package qq

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/protocol/webhook"
)

// WebhookConn 管理 QQ Webhook HTTP 服务器与 Token 生命周期，并向 qq.Adapter 提供事件流。
//
// D4：将原来混合在 WebhookServerAdapter 中的两个职责拆分：
//   - WebhookConn（本类型）：HTTP 服务器 + Token 管理 + 事件流 → 实现 qq.Webhook 接口
//   - qq.Adapter（现有类型）：事件分发 worker pool → 实现 platform.Adapter 事件路由
//
// WebhookConn 通过 qq.Webhook 接口（EventStream()）接入 qq.Adapter，
// 两者的组合由 WebhookServerAdapter 负责（见 webhook_server.go）。
type WebhookConn struct {
	addr        string
	botInfo     *dto.BotInfo
	api         openapi.OpenAPI
	apiExternal bool // true = 用户通过 WithAPI 注入，不自动创建 token.Manager
	bufferSize  int

	// 以下字段在 start() 中初始化
	tokenMgr     *token.Manager
	webhookImpl  *webhook.Conn
	server       *http.Server
	ctx          context.Context
	cancel       context.CancelFunc
	cachedSender platform.Sender // start() 后缓存，避免每事件分配新 qqSender

	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

// NewWebhookConn 使用默认配置创建 WebhookConn。
func NewWebhookConn(addr string, botInfo *dto.BotInfo) *WebhookConn {
	return NewWebhookConnWithConfig(addr, botInfo, config.WebhookConfig{
		EventBuffer: 100,
	})
}

// NewWebhookConnWithConfig 使用指定配置创建 WebhookConn。
func NewWebhookConnWithConfig(addr string, botInfo *dto.BotInfo, cfg config.WebhookConfig) *WebhookConn {
	bufferSize := cfg.EventBuffer
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &WebhookConn{
		addr:       addr,
		botInfo:    botInfo,
		bufferSize: bufferSize,
	}
}

// WithAPI 注入外部 QQ OpenAPI client。
//
// 调用后不再自动创建 token.Manager，由调用方管理 API 客户端生命周期。
// 支持链式调用：conn.WithAPI(api)
func (c *WebhookConn) WithAPI(api openapi.OpenAPI) *WebhookConn {
	c.api = api
	c.apiExternal = true
	return c
}

// Sender 返回该连接对应的消息发送器。
//
// start() 之前返回 NoopSender；start() 之后返回缓存的真实发送器，
// 避免每次调用都分配新的 qqSender 实例。
func (c *WebhookConn) Sender() platform.Sender {
	c.mu.Lock()
	s := c.cachedSender
	c.mu.Unlock()
	if s != nil {
		return s
	}
	return &platform.NoopSender{}
}

// API 返回当前绑定的 OpenAPI 客户端。start() 之前可能为 nil。
func (c *WebhookConn) API() openapi.OpenAPI {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.api
}

// TokenReady 返回是否已成功获取到 QQ 平台 access token。start() 之前或 tokenMgr 未创建时返回 false。
func (c *WebhookConn) TokenReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokenMgr == nil {
		return false
	}
	return c.tokenMgr.Ready()
}

// TokenExpiresAt 返回当前 access token 的过期时间。未就绪时返回零值。
func (c *WebhookConn) TokenExpiresAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokenMgr == nil {
		return time.Time{}
	}
	return c.tokenMgr.TokenExpiresAt()
}

// EventStream 实现 qq.Webhook 接口，返回事件 channel（start() 后有效）。
//
// 若 start() 尚未被调用，返回 nil；qq.Adapter 会将 nil channel 视为"无事件来源"并立即退出。
func (c *WebhookConn) EventStream() <-chan *dto.Payload {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.webhookImpl == nil {
		return nil
	}
	return c.webhookImpl.EventStream()
}

// start 初始化并启动 HTTP 服务器（非阻塞，HTTP server 在后台 goroutine 运行）。
//
// 若已运行，直接返回 nil。ctx 的生命周期与本次 start-stop 周期绑定。
func (c *WebhookConn) start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		logger.Debug("[WebhookConn] Already running")
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// 若提供了 BotInfo 且未通过 WithAPI 注入外部 API，则自动创建 token.Manager。
	if c.botInfo != nil && !c.apiExternal {
		mgr := token.NewManagerWithContext(c.ctx, c.botInfo)
		c.api = openapi.New(mgr)
		c.cachedSender = NewSender(c.api)
		c.tokenMgr = mgr
		logger.Info("[WebhookConn] Token manager created from BotInfo")
	} else if c.apiExternal && c.api != nil {
		c.cachedSender = NewSender(c.api)
	}

	c.webhookImpl = webhook.NewWithBuffer(c.botInfo, c.bufferSize)
	if c.webhookImpl == nil {
		c.cancel()
		c.mu.Unlock()
		return errutil.ErrWebhookCreateFailed
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", c.webhookImpl.Handle)
	mux.HandleFunc("/", c.webhookImpl.Handle)

	c.server = &http.Server{
		Addr:    c.addr,
		Handler: mux,
	}

	c.running = true
	c.mu.Unlock()

	ln, err := net.Listen("tcp", c.addr)
	if err != nil {
		c.cancel()
		c.mu.Lock()
		c.running = false
		c.webhookImpl = nil
		c.mu.Unlock()
		return errutil.Wrapf(err, "failed to bind address %s", c.addr)
	}

	c.wg.Go(func() {
		logger.Infof("[WebhookConn] HTTP server listening on %s", c.addr)
		if err := c.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[WebhookConn] HTTP server error")
			c.cancel()
		}
	})

	logger.Info("[WebhookConn] Started")
	return nil
}

// stop 优雅关闭 HTTP 服务器并等待所有 goroutine 退出。
func (c *WebhookConn) stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		logger.Debug("[WebhookConn] Not running, nothing to stop")
		return nil
	}
	c.running = false
	tokenMgr := c.tokenMgr
	c.mu.Unlock()

	logger.Info("[WebhookConn] Stopping...")

	if c.server != nil {
		if err := c.server.Shutdown(ctx); err != nil {
			logger.WithError(err).Warn("[WebhookConn] HTTP server shutdown error")
		}
	}

	if c.cancel != nil {
		c.cancel()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Warn("[WebhookConn] Stop timeout waiting for HTTP server goroutine")
		return ctx.Err()
	}

	// 同步等待 auto-created token manager 完全退出
	if tokenMgr != nil {
		tokenMgr.Stop()
	}
	// 无论是否使用外部注入的 API，都必须清空相关字段，
	// 避免热重启时 EventStream() 返回已关闭的旧 channel。
	c.mu.Lock()
	c.tokenMgr = nil
	c.api = nil
	c.cachedSender = nil
	c.webhookImpl = nil
	c.mu.Unlock()

	logger.Info("[WebhookConn] Stopped")
	return nil
}
