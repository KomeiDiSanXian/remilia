package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

const (
	defaultHeartbeatInterval = 45 * time.Second
	healthyConnDuration      = 60 * time.Second
	reconnectBaseDelay       = 1 * time.Second
	reconnectMaxDelay        = 60 * time.Second
	reconnectMaxRetries      = 0
)

// Intents 是 QQ Bot WebSocket 事件订阅的位掩码类型。
// 每一位代表一类事件，在 Identify 鉴权时通过 OR 组合传入。
//
// 参考: https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/payload.html#事件订阅-intents
type Intents int

const (
	// IntentGuilds 频道与子频道事件：创建/更新/删除。
	IntentGuilds Intents = 1 << 0
	// IntentGuildMembers 频道成员事件：加入/更新/移除。
	IntentGuildMembers Intents = 1 << 1
	// IntentGuildMessages 频道消息事件（仅私域）：MESSAGE_CREATE / MESSAGE_DELETE。
	IntentGuildMessages Intents = 1 << 9
	// IntentGuildMessageReacts 频道消息表情表态事件：添加/移除。
	IntentGuildMessageReacts Intents = 1 << 10
	// IntentDirectMessage 频道私信事件：DIRECT_MESSAGE_CREATE / DIRECT_MESSAGE_DELETE。
	IntentDirectMessage Intents = 1 << 12
	// IntentGroupAndC2C 群聊与单聊事件：C2C_MESSAGE_CREATE、FRIEND_ADD/DEL、GROUP_AT_MESSAGE_CREATE 等 8 个。
	IntentGroupAndC2C Intents = 1 << 25
	// IntentInteraction 互动事件：INTERACTION_CREATE（按钮回调等）。
	IntentInteraction Intents = 1 << 26
	// IntentMessageAudit 消息审核事件：MESSAGE_AUDIT_PASS / MESSAGE_AUDIT_REJECT。
	IntentMessageAudit Intents = 1 << 27
	// IntentForumsEvent 论坛事件（仅私域）：主题/帖子/回复创建删除。
	IntentForumsEvent Intents = 1 << 28
	// IntentAudioAction 音频事件：开始/结束播放、上/下麦。
	IntentAudioAction Intents = 1 << 29
	// IntentPublicGuildMsg 公域频道消息事件：AT_MESSAGE_CREATE / PUBLIC_MESSAGE_DELETE。
	IntentPublicGuildMsg Intents = 1 << 30
)

type identifyProperties struct {
	OS      string `json:"$os"`
	Browser string `json:"$browser"`
	Device  string `json:"$device"`
}

type identifyData struct {
	Token      string             `json:"token"`
	Intents    int                `json:"intents"`
	Shard      [2]int             `json:"shard"`
	Properties identifyProperties `json:"properties"`
}

type resumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       uint64 `json:"seq"`
}

type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type wsPayload struct {
	Op   dto.OperationCode `json:"op"`
	Data json.RawMessage   `json:"d,omitempty"`
	S    uint64            `json:"s,omitempty"`
	T    string            `json:"t,omitempty"`
	ID   string            `json:"id,omitempty"`
}

// WSConn 是 QQ Bot WebSocket 连接管理器。
//
// 它封装了 QQ Bot WebSocket 协议的完整生命周期：
//   - 连接建立（Hello → Identify/Resume → Ready）
//   - 心跳维护（定期 Heartbeat → Heartbeat ACK）
//   - 会话恢复（断线后 Resume）
//   - 事件接收（Dispatch → EventStream channel）
//   - 指数退避自动重连
//
// WSConn 实现了 qq.EventSource 接口（EventStream），
// 可以作为事件源传递给 qq.NewAdapter() 复用现有的事件分发和消息发送逻辑。
//
// 典型用法：
//
//	wsConn := qq.NewWSConn(api, tokenMgr)
//	wsConn.Start(ctx)
//	adapter := qq.NewAdapter(wsConn, api)
type WSConn struct {
	api      openapi.OpenAPI
	tokenMgr *token.Manager

	intents Intents
	shard   [2]int

	eventChan chan *dto.Payload

	mu           sync.Mutex
	conn         *websocket.Conn
	sessionID    string
	lastSeq      atomic.Uint64
	running      bool
	heartbeatInt time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWSConn 创建默认的 QQ Bot WebSocket 连接管理器（订阅所有事件类型）。
//
// 参数：
//   - api: OpenAPI 客户端（用于获取 Gateway 地址）
//   - tokenMgr: Token 管理器（用于获取 Identify/Resume 所需的 access_token）
func NewWSConn(api openapi.OpenAPI, tokenMgr *token.Manager) *WSConn {
	return NewWSConnWithIntents(api, tokenMgr, AllIntents)
}

// NewWSConnWithIntents 创建指定事件订阅的 WebSocket 连接管理器。
//
// intents 控制订阅哪些事件类型，使用按位 OR 组合（如 IntentGroupAndC2C | IntentGuilds）。
// 不支持的 intent 会在 Identify 时被服务端拒绝连接。
func NewWSConnWithIntents(api openapi.OpenAPI, tokenMgr *token.Manager, intents Intents) *WSConn {
	return &WSConn{
		api:       api,
		tokenMgr:  tokenMgr,
		intents:   intents,
		shard:     [2]int{0, 1},
		eventChan: make(chan *dto.Payload, 200),
	}
}

// WithShard 设置分片参数，支持 WebSocket 连接的负载均衡。
//
// current 当前分片索引（从 0 开始），total 总分片数。
// 若 current=0 且 total=1（默认），则使用单连接模式（GET /gateway）。
// 否则使用分片模式（GET /gateway/bot），并按 guild_id 哈希路由事件。
//
// 参考: https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/websocket.html#分片连接-loadbalance
func (c *WSConn) WithShard(current, total int) *WSConn {
	c.shard = [2]int{current, total}
	return c
}

// EventStream 返回事件 channel，实现 qq.EventSource 接口。
//
// Start() 调用前返回 nil；运行期间返回内部事件 channel。
// 消费者（qq.Adapter.Start）从中读取 *dto.Payload 并转换为 platform.Event。
func (c *WSConn) EventStream() <-chan *dto.Payload {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return nil
	}
	return c.eventChan
}

// Start 启动 WebSocket 后台连接 goroutine（非阻塞）。
//
// ctx 用于生命周期管理：ctx 取消时连接自动关闭并退出。
// 返回后可通过 EventStream() 获取事件 channel。
func (c *WSConn) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.running = true
	c.mu.Unlock()

	c.wg.Add(1)
	go c.runLoop()

	return nil
}

// Stop 优雅关闭 WebSocket 连接并等待后台 goroutine 退出。
//
// ctx 控制等待超时；超时后强制返回（不等待 goroutine）。
func (c *WSConn) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	c.running = false
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (c *WSConn) runLoop() {
	defer c.wg.Done()

	delay := reconnectBaseDelay
	attempt := 0

	for {
		if err := c.ctx.Err(); err != nil {
			logger.Info("[qq.WSConn] Context done, exiting runLoop")
			return
		}

		startedAt := time.Now()
		err := c.connect()

		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		logger.WithError(err).Warn("[qq.WSConn] Connection lost")

		if time.Since(startedAt) >= healthyConnDuration {
			attempt = 0
			delay = reconnectBaseDelay
		}

		attempt++

		logger.WithFields(logger.Fields{
			"attempt": attempt,
			"delay":   delay,
		}).Info("[qq.WSConn] Reconnecting...")

		select {
		case <-c.ctx.Done():
			return
		case <-time.After(delay):
		}

		delay = time.Duration(math.Min(
			float64(delay)*2,
			float64(reconnectMaxDelay),
		))
	}
}

func (c *WSConn) gatewayURL() (string, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	if c.shard[0] == 0 && c.shard[1] == 1 {
		result, err := c.api.GetGateway(ctx)
		if err != nil {
			return "", fmt.Errorf("get gateway: %w", err)
		}
		return result.Get("url").String(), nil
	}

	result, err := c.api.GetGatewayBot(ctx)
	if err != nil {
		return "", fmt.Errorf("get gateway bot: %w", err)
	}
	return result.Get("url").String(), nil
}

func (c *WSConn) connect() error {
	if err := c.tokenMgr.WaitReadyWithContext(c.ctx); err != nil {
		return fmt.Errorf("wait token: %w", err)
	}

	url, err := c.gatewayURL()
	if err != nil {
		return fmt.Errorf("resolve gateway url: %w", err)
	}

	logger.WithField("url", url).Info("[qq.WSConn] Dialing gateway")

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(c.ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		_ = conn.Close()
	}()

	if err := c.waitHello(conn); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	sessionID := c.sessionID
	if sessionID == "" {
		if err := c.sendIdentify(conn); err != nil {
			return fmt.Errorf("identify: %w", err)
		}
		if err := c.waitReady(conn); err != nil {
			c.sessionID = ""
			return fmt.Errorf("ready: %w", err)
		}
	} else {
		if err := c.sendResume(conn); err != nil {
			c.sessionID = ""
			logger.Warn("[qq.WSConn] Resume failed, will re-identify")
			return fmt.Errorf("resume: %w", err)
		}
	}

	pingCtx, cancelPing := context.WithCancel(c.ctx)
	defer cancelPing()
	go c.heartbeatLoop(pingCtx, conn)

	return c.readLoop(conn)
}

func (c *WSConn) waitHello(conn *websocket.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}

	var p wsPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("unmarshal hello: %w", err)
	}

	if p.Op != dto.Hello {
		return fmt.Errorf("expected op=10 (Hello), got op=%d", p.Op)
	}

	var h helloData
	if err := json.Unmarshal(p.Data, &h); err != nil {
		return fmt.Errorf("unmarshal hello data: %w", err)
	}

	c.heartbeatInt = time.Duration(h.HeartbeatInterval) * time.Millisecond
	if c.heartbeatInt <= 0 {
		c.heartbeatInt = defaultHeartbeatInterval
	}

	logger.WithField("heartbeat_interval", c.heartbeatInt).Info("[qq.WSConn] Received Hello")

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	return nil
}

func (c *WSConn) sendIdentify(conn *websocket.Conn) error {
	accessToken := c.tokenMgr.GetToken()
	if accessToken == "" {
		return errors.New("access token not ready")
	}
	tk := fmt.Sprintf("QQBot %s", accessToken)
	data := identifyData{
		Token:   tk,
		Intents: int(c.intents),
		Shard:   c.shard,
		Properties: identifyProperties{
			OS:      "linux",
			Browser: "remilia",
			Device:  "remilia",
		},
	}

	payload := wsPayload{
		Op:   dto.Identify,
		Data: mustMarshal(data),
	}

	logger.WithFields(logger.Fields{
		"intents": data.Intents,
		"shard":   data.Shard,
	}).Info("[qq.WSConn] Sending Identify")

	return c.writeJSON(conn, payload)
}

func (c *WSConn) sendResume(conn *websocket.Conn) error {
	accessToken := c.tokenMgr.GetToken()
	if accessToken == "" {
		return errors.New("access token not ready")
	}
	tk := fmt.Sprintf("QQBot %s", accessToken)
	data := resumeData{
		Token:     tk,
		SessionID: c.sessionID,
		Seq:       c.lastSeq.Load(),
	}

	payload := wsPayload{
		Op:   dto.Resume,
		Data: mustMarshal(data),
	}

	logger.WithField("session_id", c.sessionID).Info("[qq.WSConn] Sending Resume")
	return c.writeJSON(conn, payload)
}

func (c *WSConn) waitReady(conn *websocket.Conn) error {
	for {
		var p wsPayload
		if err := c.readJSON(conn, &p); err != nil {
			return fmt.Errorf("read ready: %w", err)
		}

		switch p.Op {
		case dto.InvalidSession:
			return errors.New("invalid session (resume failed)")
		case dto.Dispatch:
			if p.T == "READY" {
				var ready dto.ReadyEvent
				if err := json.Unmarshal(p.Data, &ready); err != nil {
					return fmt.Errorf("unmarshal ready: %w", err)
				}
				c.sessionID = ready.SessionID
				c.lastSeq.Store(p.S)

				logger.WithFields(logger.Fields{
					"session_id": ready.SessionID,
					"version":    ready.Version,
					"user_id":    ready.User.ID,
				}).Info("[qq.WSConn] READY received")
				return nil
			}
			c.lastSeq.Store(p.S)
			c.pushPayload(p)
		}
	}
}

func (c *WSConn) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	if c.heartbeatInt <= 0 {
		c.heartbeatInt = defaultHeartbeatInterval
	}

	ticker := time.NewTicker(c.heartbeatInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			payload := wsPayload{
				Op: dto.Heartbeat,
			}
			if seq := c.lastSeq.Load(); seq > 0 {
				payload.Data = json.RawMessage(fmt.Sprintf("%d", seq))
			}

			if err := c.writeJSON(conn, payload); err != nil {
				logger.WithError(err).Warn("[qq.WSConn] Heartbeat send failed, closing connection")
				_ = conn.Close()
				return
			}
		}
	}
}

func (c *WSConn) readLoop(conn *websocket.Conn) error {
	for {
		if err := c.ctx.Err(); err != nil {
			return err
		}

		var p wsPayload
		if err := c.readJSON(conn, &p); err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		switch p.Op {
		case dto.Heartbeat:
			if err := c.writeJSON(conn, wsPayload{Op: dto.HeartbeatACK}); err != nil {
				logger.WithError(err).Warn("[qq.WSConn] Heartbeat ACK send failed")
			}
		case dto.HeartbeatACK:
		case dto.Reconnect:
			logger.Info("[qq.WSConn] Received Reconnect opcode, reconnecting...")
			return errors.New("server requested reconnect")
		case dto.InvalidSession:
			c.sessionID = ""
			logger.Warn("[qq.WSConn] Invalid session, will re-identify")
			return errors.New("invalid session")
		case dto.Dispatch:
			c.lastSeq.Store(p.S)
			if p.T == "RESUMED" {
				logger.Info("[qq.WSConn] RESUMED - session restored")
				continue
			}
			c.pushPayload(p)
		default:
			logger.WithField("op", p.Op).Debug("[qq.WSConn] Unknown opcode")
		}
	}
}

func (c *WSConn) pushPayload(p wsPayload) {
	dtoPayload := dto.AcquirePayload()
	dtoPayload.ID = dto.EventID(p.ID)
	dtoPayload.Operation = p.Op
	dtoPayload.Detail = p.Data
	dtoPayload.Sequence = p.S
	dtoPayload.Type = p.T

	select {
	case c.eventChan <- dtoPayload:
	default:
		logger.Warn("[qq.WSConn] Event channel full, dropping event")
		dto.ReleasePayload(dtoPayload)
	}
}

func (c *WSConn) readJSON(conn *websocket.Conn, v any) error {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (c *WSConn) writeJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return errors.New("connection replaced")
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// AllIntents 包含所有事件类型的位掩码组合，适合大多数机器人使用。
//
// 包含：GUILDS | GUILD_MEMBERS | GUILD_MESSAGES | GUILD_MESSAGE_REACTIONS |
// DIRECT_MESSAGE | GROUP_AND_C2C_EVENT | INTERACTION | MESSAGE_AUDIT |
// FORUMS_EVENT | AUDIO_ACTION | PUBLIC_GUILD_MESSAGES
//
// 注意：某些 intent（如 FORUMS_EVENT、GUILD_MESSAGES）仅私域机器人可用，
// 公域机器人使用 AllIntents 时 Identify 会被拒绝。
var AllIntents = IntentGuilds | IntentGuildMembers | IntentGuildMessages |
	IntentGuildMessageReacts | IntentDirectMessage | IntentGroupAndC2C |
	IntentInteraction | IntentMessageAudit | IntentForumsEvent |
	IntentAudioAction | IntentPublicGuildMsg
