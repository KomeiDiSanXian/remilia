package onebot

import (
	stdctx "context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/gorilla/websocket"
)

// ────────────────────────────────────────────────────────────────────────────
// ReverseWSAdapter
// ────────────────────────────────────────────────────────────────────────────

// ReverseWSAdapter 是一个 platform.Adapter，它监听来自 OneBot V11 实现的
// 反向 WebSocket 连接。
//
// 通信流程：
//
//	OneBot 实现 ──WS 连接──▶ adapter（本适配器）
//	OneBot 实现 ──── 事件 ──▶ adapter
//	OneBot 实现 ◀─ API 调用 ── adapter（每个连接独立的 wsAPIClient）
//
// OneBot 实现须配置 ws_reverse.enable = true，并指向本适配器的 ListenAddr。
//
// 支持多个同时连接；所有连接的事件均分发给同一个 handler。
type ReverseWSAdapter struct {
	platform.DisconnectNotifier

	config Config

	mu      sync.RWMutex
	running bool
	cancel  stdctx.CancelFunc
	server  *http.Server

	starting atomic.Bool
	wg       sync.WaitGroup

	// 活跃连接列表
	connMu      sync.Mutex
	conns       map[*wsConn]struct{}
	primarySndr platform.Sender // 第一个连接的 sender，避免遍历 map
	handler     func(platform.Event)

	// 机器人身份（由第一个连接的客户端填充）
	botID   string
	botName string
}

// wsConn 将单个 WS 连接与其独立的 API 客户端封装在一起。
type wsConn struct {
	conn      *websocket.Conn
	apiClient *wsAPIClient
	sender    *Sender
}

const (
	// bearerPrefix 是 Authorization 头中 Bearer 方案的前缀。
	bearerPrefix = "Bearer "

	// maxWSMessageBytes 是单条 WebSocket 消息允许的最大字节数。
	// gorilla/websocket 默认不限制读取大小，恶意或异常对端可以声明
	// 超大帧使进程 OOM。OneBot 事件远小于该阈值。
	maxWSMessageBytes = 16 << 20 // 16 MiB
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// NewReverseWSAdapter 使用给定的 Config 创建 ReverseWSAdapter。
func NewReverseWSAdapter(cfg Config) *ReverseWSAdapter {
	cfg.setDefaults()
	return &ReverseWSAdapter{
		config: cfg,
		conns:  make(map[*wsConn]struct{}),
	}
}

// ── platform.Adapter ────────────────────────────────────────────────────────

// Platform 返回 "onebot"。
func (a *ReverseWSAdapter) Platform() string { return PlatformID }

// Sender 返回第一个活跃连接的发送器，若无连接则返回空操作发送器。
func (a *ReverseWSAdapter) Sender() platform.Sender {
	a.connMu.Lock()
	defer a.connMu.Unlock()
	if a.primarySndr != nil {
		return a.primarySndr
	}
	return &platform.NoopSender{}
}

// Capabilities 返回 OneBot V11 平台的功能集。
func (a *ReverseWSAdapter) Capabilities() platform.Capabilities { return onebotCapabilities() }

// IsRunning 当 WS 服务器正在接受连接时返回 true。
func (a *ReverseWSAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 开始监听传入的 WebSocket 连接。
// 阻塞直到 ctx 被取消。
func (a *ReverseWSAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
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
	a.handler = handler
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		cancel()
	}()

	listenAddr := a.config.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleWS)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("onebot reverse ws: listen %s: %w", listenAddr, err)
	}

	logger.Infof("[onebot.ReverseWSAdapter] Listening on %s", listenAddr)

	// 在后台运行 HTTP 服务器
	a.wg.Go(func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[onebot.ReverseWSAdapter] Server error")
		}
	})

	// 等待 ctx 取消
	<-cancelCtx.Done()

	shutCtx, shutCancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)

	// 关闭所有活跃连接
	a.connMu.Lock()
	for c := range a.conns {
		_ = c.conn.Close()
	}
	a.connMu.Unlock()

	a.wg.Wait()
	return nil
}

// Stop 关闭监听服务器。
func (a *ReverseWSAdapter) Stop(ctx stdctx.Context) error {
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

// ── platform.BotIdentity ────────────────────────────────────────────────────

// BotID 返回机器人的 QQ 号（来自第一个已连接的会话）。
func (a *ReverseWSAdapter) BotID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botID
}

// BotName 返回机器人的昵称（来自第一个已连接的会话）。
func (a *ReverseWSAdapter) BotName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botName
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *ReverseWSAdapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": "reverse_websocket",
	}
	a.connMu.Lock()
	detail["active_connections"] = len(a.conns)
	a.connMu.Unlock()
	return detail
}

// ────────────────────────────────────────────────────────────────────────────
// HTTP / WebSocket 处理
// ────────────────────────────────────────────────────────────────────────────

// handleWS 将 HTTP 连接升级为 WebSocket 并处理事件。
func (a *ReverseWSAdapter) handleWS(w http.ResponseWriter, r *http.Request) {
	// Token 验证。
	// OneBot V11 允许两种携带方式：Authorization: Bearer <token>，
	// 或 URL 查询参数 ?access_token=<token>。使用常量时间比较避免时序侧信道。
	if a.config.Token != "" {
		token := r.URL.Query().Get("access_token")
		if token == "" {
			auth := r.Header.Get("Authorization")
			if len(auth) >= len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
				token = auth[len(bearerPrefix):]
			} else {
				token = auth
			}
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.config.Token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.WithError(err).Warn("[onebot.ReverseWSAdapter] Upgrade failed")
		return
	}
	// 限制单帧大小，避免恶意/异常对端声明超大帧导致 OOM。
	conn.SetReadLimit(maxWSMessageBytes)

	apiClient := newWSAPIClient(conn, a.config.APITimeout)
	sender := newSender(apiClient)
	c := &wsConn{conn: conn, apiClient: apiClient, sender: sender}

	a.connMu.Lock()
	a.conns[c] = struct{}{}
	if a.primarySndr == nil {
		a.primarySndr = sender
	}
	a.connMu.Unlock()

	logger.Infof("[onebot.ReverseWSAdapter] New connection from %s", r.RemoteAddr)

	defer func() {
		a.connMu.Lock()
		delete(a.conns, c)
		if a.primarySndr == sender {
			// 重新查找下一个连接的 sender
			a.primarySndr = nil
			for cc := range a.conns {
				a.primarySndr = cc.sender
				break
			}
		}
		a.connMu.Unlock()
		_ = conn.Close()
		logger.Infof("[onebot.ReverseWSAdapter] Connection closed: %s", r.RemoteAddr)
	}()

	// 处理来自此连接的消息
	eventCh := make(chan platform.Event, a.config.EventBufferSize)
	a.wg.Go(func() {
		defer close(eventCh)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					logger.WithError(err).Debug("[onebot.ReverseWSAdapter] Read error")
				}
				return
			}
			if isAPIResponse(msg) {
				apiClient.routeResponse(msg)
				continue
			}
			ev, err := parseEvent(msg)
			if err != nil {
				logger.WithError(err).Warn("[onebot.ReverseWSAdapter] Failed to parse event")
				continue
			}
			if ev.Kind() != platform.EventKindUnknown {
				select {
				case eventCh <- ev:
				default:
					logger.Warn("[onebot.ReverseWSAdapter] Event channel full, dropping event")
				}
			}
		}
	})

	// 从第一个客户端获取机器人身份。
	//
	// 必须放在读取循环启动之后：get_login_info 的响应由上面的读取循环
	// 通过 apiClient.routeResponse 投递，若在读取循环之前调用，响应永远
	// 无法送达，只会在超时后失败，且每条新连接都要白白阻塞一次超时。
	a.mu.RLock()
	hasBotID := a.botID != ""
	a.mu.RUnlock()
	if !hasBotID {
		a.wg.Go(func() {
			fetchCtx, cancel := stdctx.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			info, err := sender.GetLoginInfo(fetchCtx)
			if err != nil {
				logger.WithError(err).Warn("[onebot.ReverseWSAdapter] Failed to fetch bot identity")
				return
			}
			a.mu.Lock()
			a.botID = strconv.FormatInt(info.UserID, 10)
			a.botName = info.Nickname
			botID := a.botID
			a.mu.Unlock()
			logger.Infof("[onebot.ReverseWSAdapter] Bot: %s (%s)", info.Nickname, botID)
		})
	}

	handler := a.handler
	for ev := range eventCh {
		if handler != nil {
			// 引用消息回查（get_msg）须在分发侧 goroutine 执行：
			// 读循环内同步调用会与 routeResponse 互相等待（见 quote.go）
			enrichQuotedAttachments(stdctx.Background(), sender, ev)
			platform.SafeDispatch(handler, ev)
		}
	}
}

// 编译期接口断言
var (
	_ platform.Adapter            = (*ReverseWSAdapter)(nil)
	_ platform.BotIdentity        = (*ReverseWSAdapter)(nil)
	_ platform.RecoverableAdapter = (*ReverseWSAdapter)(nil)
	_ platform.HealthDetailer     = (*ReverseWSAdapter)(nil)
)
