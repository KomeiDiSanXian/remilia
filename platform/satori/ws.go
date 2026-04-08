package satori

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─────────────────────────────────────────────────────────────────────────────
// wsConn – 内部 WebSocket 连接管理器
// ─────────────────────────────────────────────────────────────────────────────

// wsConn 管理到 Satori SDK 的单个 WebSocket 连接。
// 负责处理：
//   - 初始 IDENTIFY 握手（支持可选的鉴权令牌与会话恢复序列号）
//   - 心跳循环（每隔 cfg.PingInterval 发送 PING）
//   - 接收 EVENT 信令并分发给处理器
//   - 指数退避自动重连
type wsConn struct {
	cfg          Config
	platformID   string // 平台标识符，用于事件标记
	handler      func(platform.Event)
	onDisconnect func(error)

	// onReady 在收到 READY 信令时调用，参数为登录信息列表。
	// 由外部（Adapter）设置，用于同步 BotID/BotName 等信息。
	onReady func([]*Login)

	// onMeta 在收到 READY 或 META 信令时调用，参数为代理路由 URL 列表。
	// 由外部（Adapter）设置，用于同步 proxy_urls。
	onMeta func([]string)

	// lastSN 是最后收到的 EVENT 信令序列号，用于会话恢复。
	// 存储为 int64；-1 表示"无历史会话"。
	lastSN atomic.Int64

	mu      sync.Mutex
	conn    *websocket.Conn
	running bool
}

// newWSConn 创建一个新的 wsConn。
func newWSConn(cfg Config, platformID string, handler func(platform.Event), onDisconnect func(error)) *wsConn {
	c := &wsConn{
		cfg:          cfg,
		platformID:   platformID,
		handler:      handler,
		onDisconnect: onDisconnect,
	}
	c.lastSN.Store(-1) // 尚无历史会话
	return c
}

// wsURL 根据已配置的 HTTP 服务端 URL 推导 WebSocket URL。
//
// 示例：
//
//	"http://localhost:5140"  → "ws://localhost:5140/v1/events"
//	"https://example.com"   → "wss://example.com/v1/events"
func (c *wsConn) wsURL() string {
	base := strings.TrimRight(c.cfg.ServerURL, "/")
	base = strings.Replace(base, "http://", "ws://", 1)
	base = strings.Replace(base, "https://", "wss://", 1)
	return fmt.Sprintf("%s/%s/events", base, c.cfg.Version)
}

// Run 启动 WebSocket 事件循环，阻塞直到 ctx 被取消。
// 在发生可恢复错误时，会以指数退避策略自动重连。
func (c *wsConn) Run(ctx stdctx.Context) error {
	delay := c.cfg.ReconnectDelay
	maxDelay := c.cfg.MaxReconnectDelay
	maxRetries := c.cfg.MaxReconnects
	attempt := 0

	for {
		if err := ctx.Err(); err != nil {
			return err // ctx 已取消，正常退出
		}

		err := c.runOnce(ctx)
		if err == nil || errors.Is(err, stdctx.Canceled) || errors.Is(err, stdctx.DeadlineExceeded) {
			return err
		}

		// 触发断线回调
		if c.onDisconnect != nil {
			c.onDisconnect(err)
		}

		attempt++
		if maxRetries > 0 && attempt >= maxRetries {
			return fmt.Errorf("satori ws: 已达最大重连次数 (%d)，最后错误: %w", maxRetries, err)
		}

		logger.WithFields(logger.Fields{
			"platform": c.platformID,
			"attempt":  attempt,
			"delay":    delay,
		}).WithError(err).Warn("[satori.wsConn] 连接断开，正在重连…")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		// 指数退避，上限为 maxDelay。
		delay = time.Duration(math.Min(
			float64(delay)*2,
			float64(maxDelay),
		))
	}
}

// runOnce 建立一次 WebSocket 连接并持续处理消息，直到连接关闭或发生错误。
func (c *wsConn) runOnce(ctx stdctx.Context) error {
	url := c.wsURL()
	logger.WithFields(logger.Fields{
		"platform": c.platformID,
		"url":      url,
	}).Debug("[satori.wsConn] 正在连接")

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("satori ws: 拨号 %s: %w", url, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.running = false
		c.mu.Unlock()
		conn.Close()
	}()

	// 连接建立后 10s 内发送 IDENTIFY（协议要求）。
	if err := c.sendIdentify(conn); err != nil {
		return fmt.Errorf("satori ws: identify: %w", err)
	}

	// 启动心跳 goroutine。
	pingCtx, cancelPing := stdctx.WithCancel(ctx)
	defer cancelPing()
	go c.pingLoop(pingCtx, conn)

	// 消息读取循环（阻塞直到错误或 ctx 取消）。
	return c.readLoop(ctx, conn)
}

// sendIdentify 向 WebSocket 发送 IDENTIFY 信令。
func (c *wsConn) sendIdentify(conn *websocket.Conn) error {
	body := IdentifyBody{}
	if c.cfg.Token != "" {
		body.Token = c.cfg.Token
	}
	if sn := c.lastSN.Load(); sn >= 0 {
		body.SN = &sn
	}

	return c.writeSignal(conn, OpcodeIdentify, body)
}

// pingLoop 每隔 cfg.PingInterval 发送一次 PING 信令。
func (c *wsConn) pingLoop(ctx stdctx.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.writeSignal(conn, OpcodePing, nil); err != nil {
				logger.WithFields(logger.Fields{
					"platform": c.platformID,
				}).WithError(err).Debug("[satori.wsConn] PING 发送失败")
				return
			}
		}
	}
}

// readLoop 持续读取 WebSocket 消息并分发处理。
func (c *wsConn) readLoop(ctx stdctx.Context, conn *websocket.Conn) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if errors.Is(err, stdctx.Canceled) || errors.Is(err, stdctx.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("satori ws: 读取消息: %w", err)
		}

		var sig Signal
		if err := json.Unmarshal(data, &sig); err != nil {
			logger.WithFields(logger.Fields{
				"platform": c.platformID,
			}).WithError(err).Warn("[satori.wsConn] 解析信令失败")
			continue
		}

		c.handleSignal(sig)
	}
}

// handleSignal 分发已收到的 WebSocket 信令。
func (c *wsConn) handleSignal(sig Signal) {
	switch sig.Op {
	case OpcodeEvent:
		var evt Event
		if err := json.Unmarshal(sig.Body, &evt); err != nil {
			logger.WithFields(logger.Fields{
				"platform": c.platformID,
			}).WithError(err).Warn("[satori.wsConn] 解析事件 body 失败")
			return
		}
		// 更新最后收到的序列号，用于会话恢复。
		c.lastSN.Store(evt.SN)
		// 转换并分发事件。
		converted := convertEvent(&evt, c.platformID)
		if c.handler != nil {
			c.handler(converted)
		}

	case OpcodeReady:
		var ready ReadyBody
		if err := json.Unmarshal(sig.Body, &ready); err != nil {
			logger.WithFields(logger.Fields{
				"platform": c.platformID,
			}).WithError(err).Warn("[satori.wsConn] 解析 READY body 失败")
			return
		}
		logger.WithFields(logger.Fields{
			"platform": c.platformID,
			"logins":   len(ready.Logins),
		}).Info("[satori.wsConn] READY – 连接已建立")

		// 通知 Adapter 更新登录信息和代理路由列表。
		if c.onReady != nil && len(ready.Logins) > 0 {
			c.onReady(ready.Logins)
		}
		if c.onMeta != nil {
			c.onMeta(ready.ProxyURLs)
		}

	case OpcodePong:
		// 心跳已确认，无需操作。

	case OpcodeMeta:
		// 实验性：代理 URL 列表更新。
		var meta MetaBody
		if err := json.Unmarshal(sig.Body, &meta); err != nil {
			logger.WithFields(logger.Fields{
				"platform": c.platformID,
			}).WithError(err).Warn("[satori.wsConn] 解析 META body 失败")
			return
		}
		if c.onMeta != nil {
			c.onMeta(meta.ProxyURLs)
		}

	default:
		logger.WithFields(logger.Fields{
			"platform": c.platformID,
			"op":       sig.Op,
		}).Debug("[satori.wsConn] 收到未知 opcode")
	}
}

// writeSignal 序列化并通过 WebSocket 发送信令。
func (c *wsConn) writeSignal(conn *websocket.Conn, op Opcode, body any) error {
	sig := struct {
		Op   Opcode `json:"op"`
		Body any    `json:"body,omitempty"`
	}{Op: op, Body: body}

	data, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("序列化信令 op=%d: %w", op, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, data)
}

// Close 优雅地关闭当前 WebSocket 连接。
func (c *wsConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
	}
}
