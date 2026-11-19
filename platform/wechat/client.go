package wechat

// client.go — 企业微信智能机器人长连接的传输层。
//
// 协议形态：一条 WebSocket 上跑 JSON 帧，{cmd, headers:{req_id}, body}。
// 服务端推送两类帧（aibot_msg_callback / aibot_event_callback），开发者用
// 同一套结构发请求并用 headers.req_id 关联回执——注意回执帧的 cmd 可能为空，
// 因此**只能按 req_id 匹配**，不能按 cmd 匹配。
//
// 另外两条必须遵守的约束（踩过坑的地方）：
//  1. 同一个机器人同一时间只允许一条长连接。新连接订阅成功会踢掉旧连接，
//     旧连接收到 aibot_event_callback(disconnected_event) 后被服务端断开。
//     多实例部署时不要都去连，否则两个进程会互相踢，表现为"消息随机丢"。
//  2. 30 秒一次 ping。服务端长时间收不到心跳会主动断开，而 TCP 层看起来
//     完全正常，只有读到关闭帧才会发现——所以心跳失败必须**关闭连接**，
//     否则读循环会一直阻塞在 ReadMessage 上，重连逻辑永远不触发。

import (
	stdctx "context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

const (
	// pingInterval 是心跳间隔（官方建议 30 秒）。
	pingInterval = 30 * time.Second
	// heartbeatTimeout 是在没有下行帧的情况下，判定连接已死的时长。
	// 取心跳的两倍多一点：一次 ping 丢失不应误判断线。
	heartbeatTimeout = 75 * time.Second
	// maxFrameBytes 限制单帧体积，防止对端声明超大帧把进程打爆。
	maxFrameBytes = 16 << 20
	// subscribeTimeout 是订阅（aibot_subscribe）等待回执的超时。
	subscribeTimeout = 15 * time.Second
)

// wsClient 是长连接的单连接实现：负责拨号、订阅、心跳、收发帧。
//
// 生命周期由 Adapter 控制：每次重连创建新实例，不跨连接复用。
type wsClient struct {
	cfg  Config
	conn *websocket.Conn

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan *Frame

	onEvent func(reqID string, body jsonx.RawMessage)

	closed chan struct{}
	once   sync.Once
}

// newWSClient 创建长连接客户端（不拨号）。
func newWSClient(cfg Config, onEvent func(reqID string, body jsonx.RawMessage)) *wsClient {
	return &wsClient{
		cfg:     cfg,
		pending: make(map[string]chan *Frame),
		onEvent: onEvent,
		closed:  make(chan struct{}),
	}
}

// dial 建立连接。
func (c *wsClient) dial(ctx stdctx.Context) error {
	dialCtx, cancel := stdctx.WithTimeout(ctx, c.cfg.DialTimeout)
	defer cancel()

	dialer := websocket.Dialer{
		HandshakeTimeout: c.cfg.DialTimeout,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, resp, err := dialer.DialContext(dialCtx, c.cfg.Endpoint, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("wechat: 拨号 %s: HTTP %d: %w", c.cfg.Endpoint, resp.StatusCode, err)
		}
		return fmt.Errorf("wechat: 拨号 %s: %w", c.cfg.Endpoint, err)
	}
	conn.SetReadLimit(maxFrameBytes)
	c.conn = conn
	return nil
}

// subscribe 发送 aibot_subscribe 并等待回执。
//
// 订阅失败必须当成致命错误处理：没有订阅成功，服务端不会推送任何回调，
// 但连接本身是通的——静默降级成"运行中但永远收不到消息"最难排查。
func (c *wsClient) subscribe(ctx stdctx.Context) error {
	subCtx, cancel := stdctx.WithTimeout(ctx, subscribeTimeout)
	defer cancel()

	frame, err := c.request(subCtx, cmdSubscribe, subscribeBody{
		BotID:  c.cfg.BotID,
		Secret: c.cfg.Secret,
	})
	if err != nil {
		return err
	}
	if !frame.ok() {
		return fmt.Errorf("wechat: 订阅被拒绝: errcode=%d errmsg=%s",
			frame.errorCode(), frame.ErrMsg)
	}
	return nil
}

// request 发送一个请求帧并等待同 req_id 的回执。
func (c *wsClient) request(ctx stdctx.Context, cmd string, body any) (*Frame, error) {
	reqID, err := newReqID(cmd)
	if err != nil {
		return nil, err
	}

	ch := make(chan *Frame, 1)
	c.mu.Lock()
	c.pending[reqID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, reqID)
		c.mu.Unlock()
	}()

	if err := c.write(Frame{
		Cmd:     cmd,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    mustRaw(body),
	}); err != nil {
		return nil, err
	}

	select {
	case f := <-ch:
		return f, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wechat: %s 等待回执超时: %w", cmd, ctx.Err())
	case <-c.closed:
		return nil, errors.New("wechat: 连接已关闭")
	}
}

// reply 发送一条"透传 req_id"的回复（aibot_respond_msg 等）并等待回执。
//
// 企业微信要求同一次回调的所有回复都复用回调帧里的 req_id，因此这里
// 不生成新 ID。等待回执是为了让"回复被拒"（权限、长度、频率限制）能作为
// 错误返回给调用方——不读回执的话失败只会表现为"消息没出现"。
func (c *wsClient) reply(ctx stdctx.Context, reqID, cmd string, body any) error {
	frame, err := c.replyWait(ctx, reqID, cmd, body)
	if err != nil {
		return err
	}
	if !frame.ok() {
		return &frameError{cmd: cmd, code: frame.errorCode(), msg: frame.ErrMsg}
	}
	return nil
}

// frameError 表示企业微信以 errcode 拒绝了某个上行帧。
//
// 携带原始错误码，供发送侧映射成 [platform.SendErrorCode]（频率限制可重试、
// 凭证错误要改配置，上层需要区分）。
type frameError struct {
	cmd  string
	code int
	msg  string
}

// Error 实现 error 接口。
func (e *frameError) Error() string {
	return fmt.Sprintf("wechat: %s 被拒绝: errcode=%d errmsg=%s", e.cmd, e.code, e.msg)
}

// frameCode 从任意错误链中提取企业微信错误码，非帧错误返回 0。
func frameCode(err error) int {
	if fe, ok := errors.AsType[*frameError](err); ok {
		return fe.code
	}
	return 0
}

// replyWait 发送回复帧并等待同 req_id 的回执。
func (c *wsClient) replyWait(ctx stdctx.Context, reqID, cmd string, body any) (*Frame, error) {
	if reqID == "" {
		return nil, errors.New("wechat: 缺少 req_id，无法回复（该事件不是消息回调）")
	}

	ch := make(chan *Frame, 1)
	c.mu.Lock()
	c.pending[reqID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if cur, ok := c.pending[reqID]; ok && cur == ch {
			delete(c.pending, reqID)
		}
		c.mu.Unlock()
	}()

	if err := c.write(Frame{
		Cmd:     cmd,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    mustRaw(body),
	}); err != nil {
		return nil, err
	}

	select {
	case f := <-ch:
		return f, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wechat: %s 等待回执超时: %w", cmd, ctx.Err())
	case <-c.closed:
		return nil, errors.New("wechat: 连接已关闭")
	}
}

// replyNoWait 发送回复帧但不读回执（流式刷新的中间帧用：
// 逐帧等 RTT 会把流式输出拖成挤牙膏）。
func (c *wsClient) replyNoWait(reqID, cmd string, body any) error {
	if reqID == "" {
		return errors.New("wechat: 缺少 req_id，无法回复（该事件不是消息回调）")
	}
	return c.write(Frame{
		Cmd:     cmd,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    mustRaw(body),
	})
}

// write 序列化并写出一帧。
func (c *wsClient) write(frame Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return errors.New("wechat: 连接未建立")
	}
	data, err := jsonx.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wechat: 序列化帧失败: %w", err)
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("wechat: 设置写截止时间失败: %w", err)
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("wechat: 写帧失败: %w", err)
	}
	return nil
}

// readLoop 读取并分发帧，直到连接关闭或 ctx 取消。
func (c *wsClient) readLoop(ctx stdctx.Context) error {
	if err := c.conn.SetReadDeadline(time.Now().Add(heartbeatTimeout)); err != nil {
		return fmt.Errorf("wechat: 设置读截止时间失败: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if errors.Is(err, stdctx.Canceled) || errors.Is(err, stdctx.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("wechat: 读帧失败: %w", err)
		}
		// 每次成功读取都顺延截止时间：心跳帧与业务帧都算"连接活着"。
		if err := c.conn.SetReadDeadline(time.Now().Add(heartbeatTimeout)); err != nil {
			return fmt.Errorf("wechat: 刷新读截止时间失败: %w", err)
		}

		var frame Frame
		if err := jsonx.Unmarshal(data, &frame); err != nil {
			logger.WithError(err).Warn("[wechat.wsClient] 解析帧失败，跳过")
			continue
		}
		c.dispatch(frame)
	}
}

// dispatch 按 req_id / cmd 分发一帧。
func (c *wsClient) dispatch(frame Frame) {
	// 回执优先：回执帧的 cmd 可能为空，只能按 req_id 匹配。
	if frame.Headers.ReqID != "" {
		c.mu.Lock()
		ch, ok := c.pending[frame.Headers.ReqID]
		c.mu.Unlock()
		if ok {
			select {
			case ch <- &frame:
			default:
			}
			return
		}
	}

	switch frame.Cmd {
	case cmdCallback, cmdEventCallback:
		if c.onEvent != nil && len(frame.Body) > 0 {
			// req_id 必须一起下发：它是长连接模式下唯一的回复授权。
			c.onEvent(frame.Headers.ReqID, frame.Body)
		}
	case cmdPing:
		// 服务端主动 ping：回一个 pong 保持连接。
		if err := c.write(Frame{Cmd: cmdPing, Headers: frame.Headers}); err != nil {
			logger.WithError(err).Debug("[wechat.wsClient] 回复 ping 失败")
		}
	case "":
		// 既不是回执也不是推送：忽略。
	default:
		logger.WithFields(logger.Fields{
			"platform": PlatformID,
			"cmd":      frame.Cmd,
		}).Debug("[wechat.wsClient] 忽略未知帧")
	}
}

// pingLoop 每 30 秒发一次心跳。
//
// 心跳失败必须主动关闭连接：读循环阻塞在 ReadMessage 上时，
// 只有关闭连接才能让它返回、进而触发重连。
func (c *wsClient) pingLoop(ctx stdctx.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-ticker.C:
			frame, err := c.request(ctx, cmdPing, nil)
			if err != nil {
				logger.WithError(err).Warn("[wechat.wsClient] 心跳失败，关闭连接以触发重连")
				_ = c.Close()
				return
			}
			if !frame.ok() {
				logger.WithFields(logger.Fields{
					"errcode": frame.errorCode(),
					"errmsg":  frame.ErrMsg,
				}).Warn("[wechat.wsClient] 心跳被拒绝，关闭连接以触发重连")
				_ = c.Close()
				return
			}
		}
	}
}

// Close 关闭连接（幂等）。
func (c *wsClient) Close() error {
	var err error
	c.once.Do(func() {
		close(c.closed)
		if c.conn != nil {
			err = c.conn.Close()
		}
	})
	return err
}

// mustRaw 把请求体序列化成 RawMessage。
//
// 序列化失败在实践中只可能是程序错误（body 都是本地结构体），这里返回
// JSON null 并由服务端拒绝，避免为每个调用点引入一个永远不会触发的错误分支。
func mustRaw(v any) jsonx.RawMessage {
	if v == nil {
		return nil
	}
	data, err := jsonx.Marshal(v)
	if err != nil {
		logger.WithError(err).Error("[wechat.wsClient] 序列化请求体失败，发送 null")
		return jsonx.RawMessage("null")
	}
	return data
}
