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

// WebhookService 管理 QQ Webhook HTTP 服务器与 Token 生命周期，并向 qq.Adapter 提供事件流。
//
// 将原来混合在 WebhookServerAdapter 中的两个职责拆分：
//   - WebhookService（本类型）：HTTP 服务器 + Token 管理 + 事件流 → 实现 qq.EventSource 接口
//   - qq.Adapter（现有类型）：事件分发 worker pool → 实现 platform.Adapter 事件路由
//
// WebhookService 通过 qq.EventSource 接口（EventStream()）接入 qq.Adapter，
// 两者的组合由 WebhookServerAdapter 负责（见 webhook_server.go）。
//
// 与 openapi/protocol/webhook.Conn 的区别：
//   - webhook.Conn：协议层，处理 HTTP 请求的验签、解析和操作分发
//   - WebhookService：服务层，管理 HTTP 服务器生命周期、Token 刷新和事件流
type WebhookService struct {
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

// NewWebhookService 使用默认配置创建 WebhookService。
func NewWebhookService(addr string, botInfo *dto.BotInfo) *WebhookService {
	return NewWebhookServiceWithConfig(addr, botInfo, config.WebhookConfig{
		EventBuffer: 100,
	})
}

// NewWebhookServiceWithConfig 使用指定配置创建 WebhookService。
func NewWebhookServiceWithConfig(addr string, botInfo *dto.BotInfo, cfg config.WebhookConfig) *WebhookService {
	bufferSize := cfg.EventBuffer
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &WebhookService{
		addr:       addr,
		botInfo:    botInfo,
		bufferSize: bufferSize,
	}
}

// WithAPI 注入外部 QQ OpenAPI client。
//
// 调用后不再自动创建 token.Manager，由调用方管理 API 客户端生命周期。
// 支持链式调用：s.WithAPI(api)
func (s *WebhookService) WithAPI(api openapi.OpenAPI) *WebhookService {
	s.api = api
	s.apiExternal = true
	return s
}

// Sender 返回该连接对应的消息发送器。
//
// start() 之前返回 NoopSender；start() 之后返回缓存的真实发送器，
// 避免每次调用都分配新的 qqSender 实例。
func (s *WebhookService) Sender() platform.Sender {
	s.mu.Lock()
	sv := s.cachedSender
	s.mu.Unlock()
	if sv != nil {
		return sv
	}
	return &platform.NoopSender{}
}

// API 返回当前绑定的 OpenAPI 客户端。start() 之前可能为 nil。
func (s *WebhookService) API() openapi.OpenAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.api
}

// TokenReady 返回是否已成功获取到 QQ 平台 access token。start() 之前或 tokenMgr 未创建时返回 false。
func (s *WebhookService) TokenReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenMgr == nil {
		return false
	}
	return s.tokenMgr.Ready()
}

// TokenExpiresAt 返回当前 access token 的过期时间。未就绪时返回零值。
func (s *WebhookService) TokenExpiresAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenMgr == nil {
		return time.Time{}
	}
	return s.tokenMgr.TokenExpiresAt()
}

// EventStream 实现 qq.EventSource 接口，返回事件 channel（start() 后有效）。
//
// 若 start() 尚未被调用，返回 nil；qq.Adapter 会将 nil channel 视为"无事件来源"并立即退出。
func (s *WebhookService) EventStream() <-chan *dto.Payload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.webhookImpl == nil {
		return nil
	}
	return s.webhookImpl.EventStream()
}

// start 初始化并启动 HTTP 服务器（非阻塞，HTTP server 在后台 goroutine 运行）。
//
// 若已运行，直接返回 nil。ctx 的生命周期与本次 start-stop 周期绑定。
func (s *WebhookService) start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		logger.Debug("[WebhookService] Already running")
		return nil
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	// 若提供了 BotInfo 且未通过 WithAPI 注入外部 API，则自动创建 token.Manager。
	if s.botInfo != nil && !s.apiExternal {
		mgr := token.NewManagerWithContext(s.ctx, s.botInfo)
		s.api = openapi.New(mgr)
		s.cachedSender = NewSender(s.api)
		s.tokenMgr = mgr
		logger.Info("[WebhookService] Token manager created from BotInfo")
	} else if s.apiExternal && s.api != nil {
		s.cachedSender = NewSender(s.api)
	}

	s.webhookImpl = webhook.NewWithBuffer(s.botInfo, s.bufferSize)
	if s.webhookImpl == nil {
		s.cancel()
		s.mu.Unlock()
		return errutil.ErrWebhookCreateFailed
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.webhookImpl.Handle)
	mux.HandleFunc("/", s.webhookImpl.Handle)

	// 该端口按设计对公网可达（QQ 回调），必须设置读超时，
	// 否则慢速请求可长期占用连接与 goroutine（Slowloris）。
	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.running = true
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.cancel()
		s.mu.Lock()
		s.running = false
		s.webhookImpl = nil
		s.mu.Unlock()
		return errutil.Wrapf(err, "failed to bind address %s", s.addr)
	}

	s.wg.Go(func() {
		logger.Infof("[WebhookService] HTTP server listening on %s", s.addr)
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[WebhookService] HTTP server error")
			s.cancel()
		}
	})

	logger.Info("[WebhookService] Started")
	return nil
}

// stop 优雅关闭 HTTP 服务器并等待所有 goroutine 退出。
func (s *WebhookService) stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		logger.Debug("[WebhookService] Not running, nothing to stop")
		return nil
	}
	s.running = false
	tokenMgr := s.tokenMgr
	s.mu.Unlock()

	logger.Info("[WebhookService] Stopping...")

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			logger.WithError(err).Warn("[WebhookService] HTTP server shutdown error")
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Warn("[WebhookService] Stop timeout waiting for HTTP server goroutine")
		return ctx.Err()
	}

	// 同步等待 auto-created token manager 完全退出
	if tokenMgr != nil {
		tokenMgr.Stop()
	}
	// 无论是否使用外部注入的 API，都必须清空相关字段，
	// 避免热重启时 EventStream() 返回已关闭的旧 channel。
	s.mu.Lock()
	s.tokenMgr = nil
	s.api = nil
	s.cachedSender = nil
	s.webhookImpl = nil
	s.mu.Unlock()

	logger.Info("[WebhookService] Stopped")
	return nil
}
