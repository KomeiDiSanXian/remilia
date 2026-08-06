package onebot

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ────────────────────────────────────────────────────────────────────────────
// APIClient 接口
// ────────────────────────────────────────────────────────────────────────────

// APIClient 是向 OneBot 实现发送 API 调用的接口。
//
// 具体实现：
//   - wsAPIClient  — 通过已有的 WebSocket 连接发送请求
//   - httpAPIClient — 向 OneBot HTTP API 服务器发送请求
type APIClient interface {
	// Call 使用给定的参数发送 API 动作并返回响应。
	//
	// 若 OneBot 实现返回非 ok 状态，则返回 error。
	Call(ctx stdctx.Context, action string, params any) (*APIResponse, error)
}

// ────────────────────────────────────────────────────────────────────────────
// wsAPIClient — 基于 WebSocket 的 API 客户端
// ────────────────────────────────────────────────────────────────────────────

// pending 持有一个等待 echo 关联 API 响应的通道。
type pending struct {
	ch chan *APIResponse
}

// wsAPIClient 通过 WebSocket 连接发送 API 请求，并按 echo ID 匹配响应。
type wsAPIClient struct {
	conn      *websocket.Conn
	mu        sync.Mutex          // 保护 conn 和发送操作
	pending   map[string]*pending // echo → 等待中的调用
	pendingMu sync.Mutex
	counter   atomic.Uint64
	timeout   time.Duration
}

// newWSAPIClient 创建一个包装已有 WebSocket 连接的 wsAPIClient。
func newWSAPIClient(conn *websocket.Conn, timeout time.Duration) *wsAPIClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	c := &wsAPIClient{
		conn:    conn,
		pending: make(map[string]*pending),
		timeout: timeout,
	}
	return c
}

// routeResponse 将收到的 API 响应分发给对应的等待调用。
// 当适配器的接收循环收到非事件 JSON 消息时调用此方法。
func (c *wsAPIClient) routeResponse(data []byte) {
	var resp APIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.Echo == "" {
		return
	}
	c.pendingMu.Lock()
	p, ok := c.pending[resp.Echo]
	c.pendingMu.Unlock()
	if ok {
		select {
		case p.ch <- &resp:
		default:
		}
	}
}

// Call 通过 WebSocket 实现 APIClient 接口。
func (c *wsAPIClient) Call(ctx stdctx.Context, action string, params any) (*APIResponse, error) {
	echo := fmt.Sprintf("e%d", c.counter.Add(1))

	req := APIRequest{
		Action: action,
		Params: params,
		Echo:   echo,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("onebot api: marshal request: %w", err)
	}

	// 发送前注册 pending 通道以避免竞争
	ch := make(chan *APIResponse, 1)
	c.pendingMu.Lock()
	c.pending[echo] = &pending{ch: ch}
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, echo)
		c.pendingMu.Unlock()
	}()

	// 发送请求。
	//
	// 必须设置写超时：gorilla/websocket 默认无写截止时间，对端停止读取时
	// WriteMessage 会在持有 c.mu 的情况下无限期阻塞，进而拖死所有并发 API
	// 调用——且此时读循环无错误可感知，不会触发重连。
	writeTimeout := c.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 && d < writeTimeout {
			writeTimeout = d
		}
	}
	c.mu.Lock()
	if err = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err == nil {
		err = c.conn.WriteMessage(websocket.TextMessage, b)
	}
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("onebot api: ws write: %w", err)
	}

	// 等待响应，带超时。
	//
	// 这里刻意使用完整的 c.timeout 而非上面收窄过的 writeTimeout：
	// 调用方自己的 deadline 由下面的 ctx.Done() 分支负责，让两者共用同一个
	// 时长会使定时器与 ctx.Done() 几乎同时就绪，select 随机选中定时器时会把
	// context.DeadlineExceeded 变成本地的 timeout 文本错误，
	// 破坏调用方的 errors.Is(err, context.DeadlineExceeded) 判断。
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, checkResponse(resp)
	case <-timer.C:
		return nil, fmt.Errorf("onebot api: timeout waiting for %q response", action)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// httpAPIClient — 基于 HTTP 的 API 客户端
// ────────────────────────────────────────────────────────────────────────────

// httpAPIClient 向 OneBot HTTP 服务器发送 API 请求。
type httpAPIClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// newHTTPAPIClient 创建一个指向给定 baseURL 的 httpAPIClient。
// baseURL 格式如 "http://127.0.0.1:5700"。
func newHTTPAPIClient(baseURL, token string, timeout time.Duration) *httpAPIClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &httpAPIClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: timeout},
	}
}

// Call 通过 HTTP POST 实现 APIClient 接口。
func (c *httpAPIClient) Call(ctx stdctx.Context, action string, params any) (*APIResponse, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("onebot api: marshal params: %w", err)
	}

	url := c.baseURL + "/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("onebot api: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onebot api: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("onebot api: read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("onebot api: unauthorized (check access token)")
	case http.StatusForbidden:
		return nil, fmt.Errorf("onebot api: forbidden")
	case http.StatusNotFound:
		return nil, fmt.Errorf("onebot api: action %q not found", action)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("onebot api: unmarshal response: %w", err)
	}
	return &apiResp, checkResponse(&apiResp)
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

// checkResponse 当 API 调用失败时返回 error。
func checkResponse(r *APIResponse) error {
	if r.IsOK() || r.IsAsync() {
		return nil
	}
	return fmt.Errorf("onebot api: action failed (status=%s, retcode=%d)", r.Status, r.Retcode)
}

// callDecoded 调用 API 动作并将响应的 Data 字段解码到 dst。
func callDecoded(ctx stdctx.Context, c APIClient, action string, params any, dst any) error {
	resp, err := c.Call(ctx, action, params)
	if err != nil {
		return err
	}
	if dst == nil || len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, dst)
}

// callCompat 调用主动作；若协议端返回"动作不存在"（retcode=404 或 not found），
// 依次尝试备选动作。
//
// 不同 OneBot 协议端对同一功能使用不同动作名（如群签到：
// NapCat 用 set_group_sign、LLB 用 send_group_sign；群公告公开名 vs
// 下划线隐藏名）。通过主/备动作链，一个方法即可跨端兼容。
//
// 注意：fallback 仅在动作不存在时触发，参数不变；业务错误（如权限不足）
// 不会触发 fallback。
func callCompat(ctx stdctx.Context, c APIClient, primary string, fallbacks []string, params any) (*APIResponse, error) {
	resp, err := c.Call(ctx, primary, params)
	if err == nil || !isActionNotFound(err) {
		return resp, err
	}
	for _, fb := range fallbacks {
		if resp2, err2 := c.Call(ctx, fb, params); err2 == nil || !isActionNotFound(err2) {
			return resp2, err2
		}
	}
	return resp, err
}

// callCompatDecoded 是 callCompat 的解码版本。
func callCompatDecoded(ctx stdctx.Context, c APIClient, primary string, fallbacks []string, params any, dst any) error {
	resp, err := callCompat(ctx, c, primary, fallbacks, params)
	if err != nil {
		return err
	}
	if dst == nil || len(resp.Data) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Data, dst)
}

// isActionNotFound 判断错误是否为"动作不存在"。
//
// 覆盖两种错误来源：
//   - wsAPIClient：checkResponse 产生的 "retcode=404"
//   - httpAPIClient：HTTP 404 产生的 "not found"
func isActionNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "retcode=404") || strings.Contains(msg, "not found")
}
