package telegram

import (
	stdctx "context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// Telegram Webhook 模式的常量与默认值。
const (
	// webhookHeaderSecretToken 是 Telegram 推送更新时携带的鉴权请求头
	// （对应 setWebhook 的 secret_token 参数）。
	webhookHeaderSecretToken = "X-Telegram-Bot-Api-Secret-Token"
	// defaultWebhookPath 是未显式配置路径且无法从 public_url 推导时使用的路径。
	defaultWebhookPath = "/telegram"
	// maxWebhookBodyBytes 限制单次推送的体积（Telegram Update 远小于此值）。
	maxWebhookBodyBytes = 1 << 20
	// defaultWebhookBuffer 是内部事件通道的默认容量。
	defaultWebhookBuffer = 256
	// defaultWebhookWorkers 是事件处理协程的默认数量。
	defaultWebhookWorkers = 4
	// defaultWebhookShutdownTimeout 是优雅关闭的默认等待时长。
	defaultWebhookShutdownTimeout = 10 * time.Second
)

// WebhookConfig 配置基于 Webhook 的 Telegram 适配器。
//
// Webhook 模式下由 Telegram 主动 POST 更新到本端，无需长轮询；适合
// 无法长期持有出站连接、或希望由反向代理统一入口的部署。
type WebhookConfig struct {
	// ListenAddr 是本地 HTTP 服务监听地址，如 ":8443"。
	ListenAddr string

	// PublicURL 是对外可达的 HTTPS 端点，会写入 setWebhook。
	//
	// Telegram 只接受 HTTPS（受信证书），因此通常由反向代理终止 TLS 后
	// 转发到 ListenAddr。路径留空时从该 URL 推导。
	PublicURL string

	// Path 是接收更新的路径，默认从 PublicURL 推导，推导失败用 "/telegram"。
	Path string

	// SecretToken 非空时要求每个请求都带匹配的
	// X-Telegram-Bot-Api-Secret-Token 头，防止他人伪造更新。
	SecretToken string

	// EventBuffer 是内部事件通道容量，默认 256。
	EventBuffer int

	// WorkerCount 是事件处理协程数，默认 4。
	WorkerCount int

	// ShutdownTimeout 是优雅关闭的等待时长，默认 10s。
	ShutdownTimeout time.Duration
}

// setDefaults 填充零值字段。
func (c *WebhookConfig) setDefaults() {
	if c.Path == "" {
		c.Path = webhookPathFromURL(c.PublicURL)
	}
	if c.Path == "" {
		c.Path = defaultWebhookPath
	}
	if !strings.HasPrefix(c.Path, "/") {
		c.Path = "/" + c.Path
	}
	if c.EventBuffer <= 0 {
		c.EventBuffer = defaultWebhookBuffer
	}
	if c.WorkerCount <= 0 {
		c.WorkerCount = defaultWebhookWorkers
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = defaultWebhookShutdownTimeout
	}
}

// webhookPathFromURL 从回调 URL 推导接收路径；无路径时返回空串。
func webhookPathFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// WebhookAdapter 是通过 Webhook 接收更新的 Telegram platform.Adapter 实现。
//
// HTTP 处理函数只做鉴权、解码与入队，然后立即返回 200——附件换链和 handler
// 执行都在 worker 中完成，避免 Telegram 因响应超时而重投造成重复消息。
//
// WebhookAdapter 实现：
//   - platform.Adapter
//   - platform.BotIdentity
//   - platform.RecoverableAdapter（断线通知）
//   - platform.HealthDetailer
type WebhookAdapter struct {
	platform.DisconnectNotifier

	cfg     WebhookConfig
	client  *Client
	sender  *telegramSender
	botUser *User

	cancel   stdctx.CancelFunc
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool
	server   *http.Server
	eventCh  chan platform.Event
	workers  sync.WaitGroup
}

// NewWebhookAdapter 创建 Webhook 模式适配器。
//
// 与 [NewPollingAdapter] 一致，构造时调用 getMe 校验 token 并获取机器人身份；
// cfg.Webhook 必须非空（配置层负责模式选择）。
func NewWebhookAdapter(cfg Config) (*WebhookAdapter, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram adapter: token is required")
	}
	if cfg.Webhook == nil {
		return nil, fmt.Errorf("telegram adapter: webhook config is required")
	}
	hook := *cfg.Webhook
	hook.setDefaults()
	if hook.ListenAddr == "" {
		return nil, fmt.Errorf("telegram adapter: webhook.listen_addr is required")
	}
	if hook.PublicURL == "" {
		return nil, fmt.Errorf("telegram adapter: webhook.public_url is required")
	}
	if !strings.HasPrefix(strings.ToLower(hook.PublicURL), "https://") {
		// Telegram 只投递到 HTTPS，明文地址不会有任何回调；提前告警而不是等 24h 的静默失败。
		logger.Warnf("[telegram.WebhookAdapter] public_url 不是 HTTPS（%s），Telegram 不会投递更新", hook.PublicURL)
	}

	client := NewClient(cfg.Token)
	botUser, err := client.GetMe(stdctx.Background())
	if err != nil {
		return nil, fmt.Errorf("telegram adapter: getMe failed: %w", err)
	}

	return &WebhookAdapter{
		cfg:     hook,
		client:  client,
		sender:  newSender(client, botUser.UserName()),
		botUser: botUser,
		eventCh: make(chan platform.Event, hook.EventBuffer),
	}, nil
}

// Platform 返回 "telegram"。
func (a *WebhookAdapter) Platform() string { return PlatformID }

// Sender 返回 Telegram 消息发送器。
func (a *WebhookAdapter) Sender() platform.Sender { return a.sender }

// Capabilities 返回 Telegram 能力集（与长轮询模式一致）。
func (a *WebhookAdapter) Capabilities() platform.Capabilities {
	return telegramCapabilities()
}

// IsRunning 报告 HTTP 服务是否正在接收更新。
func (a *WebhookAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// BotID 返回机器人的数字 ID。
func (a *WebhookAdapter) BotID() string { return fmt.Sprintf("%d", a.botUser.ID) }

// BotName 返回机器人的用户名。
func (a *WebhookAdapter) BotName() string { return a.botUser.UserName() }

// HealthDetail 返回运行时健康信息。
func (a *WebhookAdapter) HealthDetail() map[string]any {
	return map[string]any{
		"mode":        "webhook",
		"listen_addr": a.cfg.ListenAddr,
		"path":        a.cfg.Path,
		"running":     a.IsRunning(),
		"bot_id":      a.BotID(),
		"bot_name":    a.BotName(),
	}
}

// Start 注册 Webhook、启动 HTTP 服务与事件 worker，并阻塞到 ctx 取消。
//
// setWebhook 失败时直接返回错误：没有回调地址就收不到任何事件，
// 静默降级为"运行中但永远没有消息"比启动失败更难排查。
func (a *WebhookAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
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

	if err := a.client.SetWebhook(cancelCtx, SetWebhookPayload{
		URL:            a.cfg.PublicURL,
		SecretToken:    a.cfg.SecretToken,
		MaxConnections: a.cfg.WorkerCount * 4,
	}); err != nil {
		a.markStopped()
		return fmt.Errorf("telegram webhook: setWebhook failed: %w", err)
	}
	logger.Infof("[telegram.WebhookAdapter] Webhook registered: %s -> %s%s (bot: %s)",
		a.cfg.PublicURL, a.cfg.ListenAddr, a.cfg.Path, a.BotName())

	mux := http.NewServeMux()
	mux.HandleFunc(a.cfg.Path, a.handleUpdate)
	srv := &http.Server{
		Addr:              a.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	for range a.cfg.WorkerCount {
		a.workers.Add(1)
		go a.worker(cancelCtx, handler)
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	var runErr error
	select {
	case <-cancelCtx.Done():
	case err := <-serveErr:
		runErr = fmt.Errorf("telegram webhook: serve failed: %w", err)
		logger.WithError(err).Error("[telegram.WebhookAdapter] HTTP server stopped")
	}

	// 优雅关闭：先停接收，再等 worker 处理完已入队的事件。
	shutdownCtx, shutdownCancel := stdctx.WithTimeout(stdctx.Background(), a.cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, stdctx.DeadlineExceeded) {
		logger.WithError(err).Warn("[telegram.WebhookAdapter] shutdown error")
	}
	a.markStopped()
	a.workers.Wait()
	logger.Debug("[telegram.WebhookAdapter] Stopped")
	return runErr
}

// Stop 优雅关闭 HTTP 服务并取消 worker。
//
// 不调用 deleteWebhook：保留注册可让 Telegram 在停机期间缓存更新并在恢复后
// 补投；主动注销会丢掉这段时间的消息。
func (a *WebhookAdapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	srv := a.server
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		shutdownCtx, shutdownCancel := stdctx.WithTimeout(ctx, a.cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}

// markStopped 复位运行标志（Start/Stop 共用）。
func (a *WebhookAdapter) markStopped() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// handleUpdate 校验并解码单条更新，入队后立即返回。
func (a *WebhookAdapter) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.cfg.SecretToken != "" {
		got := r.Header.Get(webhookHeaderSecretToken)
		// 常数时间比较：避免按字节泄露 secret 前缀。
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.cfg.SecretToken)) != 1 {
			logger.Warn("[telegram.WebhookAdapter] 收到 secret_token 不匹配的推送，已拒绝")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	var upd Update
	if err := json.Unmarshal(body, &upd); err != nil {
		logger.WithError(err).Warn("[telegram.WebhookAdapter] 无法解析更新")
		http.Error(w, "invalid update", http.StatusBadRequest)
		return
	}

	event := newEventWithBot(&upd, a.BotID())
	if event == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	select {
	case a.eventCh <- event:
		w.WriteHeader(http.StatusOK)
	default:
		// 返回 503 让 Telegram 稍后重投：比"回 200 但丢消息"更安全。
		logger.Warn("[telegram.WebhookAdapter] 事件队列已满，通知 Telegram 重投")
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}
}

// worker 从队列取事件，换附件直链后交给 handler。
func (a *WebhookAdapter) worker(ctx stdctx.Context, handler func(platform.Event)) {
	defer a.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-a.eventCh:
			resolveAttachmentURLs(ctx, a.client, event, attachmentResolveBudget)
			platform.SafeDispatch(handler, event)
		}
	}
}

var (
	_ platform.Adapter        = (*WebhookAdapter)(nil)
	_ platform.BotIdentity    = (*WebhookAdapter)(nil)
	_ platform.HealthDetailer = (*WebhookAdapter)(nil)
)
