package satori

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─────────────────────────────────────────────────────────────────────────────
// WebhookConfig
// ─────────────────────────────────────────────────────────────────────────────

// WebhookConfig 配置基于 WebHook 的适配器。
//
// 在 WebHook 模式下，Satori SDK 将事件主动推送到应用提供的 URL，
// 而非由应用通过 WebSocket 连接 SDK。
// 参见：https://satori.chat/zh-CN/protocol/events.html#webhook
type WebhookConfig struct {
	// ListenAddr 是 HTTP 服务监听地址。
	// 示例：":8080" 或 "0.0.0.0:9000"。
	ListenAddr string

	// Path 是 SDK 推送事件的 URL 路径。
	// 默认："/satori/webhook"
	Path string

	// Token 是应用期望 SDK 在 Authorization 请求头中携带的 Bearer 令牌。
	// 留空则不进行鉴权校验。
	Token string

	// Platform 是分发事件时使用的平台标识符。
	Platform string

	// UserID 是机器人用户 ID。
	UserID string

	// EventBufferSize 是内部事件通道的缓冲区大小。
	// 默认：256。
	EventBufferSize int

	// HTTPServer 是底层 http.Server，传 nil 则使用默认值。
	HTTPServer *http.Server
}

// DefaultWebhookConfig 返回具有合理默认值的 WebhookConfig。
func DefaultWebhookConfig(listenAddr, platform, userID string) WebhookConfig {
	return WebhookConfig{
		ListenAddr:      listenAddr,
		Path:            "/satori/webhook",
		Platform:        platform,
		UserID:          userID,
		EventBufferSize: 256,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WebhookAdapter
// ─────────────────────────────────────────────────────────────────────────────

// WebhookAdapter 是通过 WebHook（HTTP POST）接收 Satori 事件的 platform.Adapter，
// 而非通过 WebSocket 主动连接 SDK。
//
// 它启动一个 HTTP 服务端监听来自 Satori SDK 的事件推送。
// SDK 必须配置为向本 Adapter 的 URL 推送事件。
type WebhookAdapter struct {
	platform.DisconnectNotifier

	cfg    WebhookConfig
	client *Client       // 可选；通过 WithSendConfig 设置以支持 API 调用
	sender *satoriSender // 若未配置 client 则为 nil

	mu      sync.RWMutex
	server  *http.Server
	running bool
	cancel  stdctx.CancelFunc

	starting atomic.Bool

	// META 信令到达后更新的代理路由 URL 列表（实验性）
	proxyMu   sync.RWMutex
	proxyURLs []string
}

// NewWebhookAdapter 根据给定配置创建一个新的 WebhookAdapter。
//
// 若同时需要发送消息（而不仅是接收事件），
// 请通过 WithSendConfig 提供对应的 Config 以初始化 HTTP API 客户端。
func NewWebhookAdapter(cfg WebhookConfig) *WebhookAdapter {
	if cfg.Path == "" {
		cfg.Path = "/satori/webhook"
	}
	if cfg.EventBufferSize <= 0 {
		cfg.EventBufferSize = 256
	}
	return &WebhookAdapter{cfg: cfg}
}

// WithSendConfig 为 WebhookAdapter 绑定 HTTP API 客户端，
// 使其在接收事件（WebHook）的同时也能发送消息（HTTP API）。
func (a *WebhookAdapter) WithSendConfig(sendCfg Config) *WebhookAdapter {
	sendCfg.Platform = a.cfg.Platform
	sendCfg.UserID = a.cfg.UserID
	_ = sendCfg.Validate() // 填充默认值；忽略错误（调用方应自行校验）
	client := newClient(sendCfg)
	a.client = client
	a.sender = newSender(client)
	return a
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.Adapter 接口
// ─────────────────────────────────────────────────────────────────────────────

// Platform 返回平台标识符。
func (a *WebhookAdapter) Platform() string {
	if a.cfg.Platform != "" {
		return a.cfg.Platform
	}
	return PlatformID
}

// Sender 返回消息发送器；若未配置则返回空操作发送器。
func (a *WebhookAdapter) Sender() platform.Sender {
	if a.sender != nil {
		return a.sender
	}
	return &platform.NoopSender{}
}

// Capabilities 返回 Satori 平台的功能特性集合。
func (a *WebhookAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		MessageEdit:   a.sender != nil,
		MessageDelete: a.sender != nil,
		Reactions:     a.sender != nil,
		GuildSupport:  true,
		MentionAll:    true,
	}
}

// IsRunning 当 HTTP 服务端处于运行状态时返回 true。
func (a *WebhookAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 启动 WebHook HTTP 服务端并将收到的事件分发给 handler。
// 该方法会阻塞，直到 ctx 被取消。
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

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc(a.cfg.Path, a.makeHTTPHandler(handler))

	srv := a.cfg.HTTPServer
	if srv == nil {
		srv = &http.Server{Addr: a.cfg.ListenAddr, Handler: mux}
	} else {
		srv.Handler = mux
	}

	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	logger.WithFields(logger.Fields{
		"platform": a.Platform(),
		"addr":     a.cfg.ListenAddr,
		"path":     a.cfg.Path,
	}).Info("[satori.WebhookAdapter] 正在启动 WebHook HTTP 服务端")

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.NotifyDisconnect(err)
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	select {
	case <-cancelCtx.Done():
		_ = srv.Shutdown(stdctx.Background())
		return cancelCtx.Err()
	case err := <-errCh:
		return err
	}
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

// BotID 返回机器人的用户 ID（来自 cfg.UserID）。
func (a *WebhookAdapter) BotID() string { return a.cfg.UserID }

// BotName 返回机器人显示名称（WebHook 模式下无名称信息）。
func (a *WebhookAdapter) BotName() string { return "" }

// Stop 优雅地关闭 WebHook HTTP 服务端。
func (a *WebhookAdapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	srv := a.server
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP 处理器
// ─────────────────────────────────────────────────────────────────────────────

// makeHTTPHandler 返回处理 Satori WebHook POST 请求的 http.HandlerFunc。
//
// 按照 Satori 协议规范：
//   - 请求头 Satori-Opcode 包含信令类型（Opcode）；
//   - 请求体是符合信令 body 结构的 JSON 对象；
//   - 成功返回 2XX，鉴权失败返回 4XX，处理失败返回 5XX。
func (a *WebhookAdapter) makeHTTPHandler(handler func(platform.Event)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// 若已配置 Token，验证 Authorization 鉴权头。
		if a.cfg.Token != "" {
			auth := r.Header.Get("Authorization")
			expected := "Bearer " + a.cfg.Token
			if !strings.EqualFold(auth, expected) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// 解析 Satori-Opcode 请求头。
		opcodeStr := r.Header.Get("Satori-Opcode")
		opcodeVal, err := strconv.Atoi(opcodeStr)
		if err != nil {
			http.Error(w, "Bad Request: invalid Satori-Opcode", http.StatusBadRequest)
			return
		}
		op := Opcode(opcodeVal)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		switch op {
		case OpcodeEvent:
			var evt Event
			if err := json.Unmarshal(body, &evt); err != nil {
				http.Error(w, "Bad Request: invalid event body", http.StatusBadRequest)
				return
			}
			converted := convertEvent(&evt, a.Platform())
			if handler != nil {
				handler(converted)
			}
			w.WriteHeader(http.StatusOK)

		case OpcodeMeta:
			// 实验性：代理 URL 列表更新，存储到 adapter 供后续使用。
			var meta MetaBody
			if err := json.Unmarshal(body, &meta); err != nil {
				// 解析失败时仍返回 200，不影响 SDK 继续推送。
				logger.WithFields(logger.Fields{
					"platform": a.Platform(),
				}).WithError(err).Warn("[satori.WebhookAdapter] 解析 META body 失败")
				w.WriteHeader(http.StatusOK)
				return
			}
			a.proxyMu.Lock()
			a.proxyURLs = meta.ProxyURLs
			a.proxyMu.Unlock()
			w.WriteHeader(http.StatusOK)

		default:
			// WebHook 仅支持 EVENT 和 META 两种信令类型。
			http.Error(w, "Bad Request: unsupported opcode", http.StatusBadRequest)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 附加辅助方法
// ─────────────────────────────────────────────────────────────────────────────

// Client 返回底层的 Satori HTTP API 客户端（若已通过 WithSendConfig 配置）。
// 调用方可使用此客户端调用进阶 API，如 UploadCreate、ProxyFetch、MetaGet、InternalCall 等。
// 若未配置则返回 nil。
func (a *WebhookAdapter) Client() *Client { return a.client }

// ProxyURLs 返回从 META 信令中获取的代理路由 URL 前缀列表（实验性）。
//
// 在 WebHook 模式下，建议启动时通过 Client().MetaGet() 获取初始 proxy_urls；
// 之后由 SDK 通过 META 信令动态更新。
//
// 参见：https://satori.js.org/zh-CN/advanced/resource.html#proxy-route
func (a *WebhookAdapter) ProxyURLs() []string {
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
	_ platform.Adapter            = (*WebhookAdapter)(nil)
	_ platform.BotIdentity        = (*WebhookAdapter)(nil)
	_ platform.RecoverableAdapter = (*WebhookAdapter)(nil)
)
