package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// transport_http.go — Streamable HTTP 传输。
//
// 每条报文以 POST 发送；响应既可能是单条 JSON，也可能是 SSE 事件流（逐条
// 回调）。会话 id 由服务器在响应头 Mcp-Session-Id 下发，后续请求带上。
//
// 除请求-响应外，服务器还可能经**独立**的 GET 事件流主动推送通知（如
// tools/list_changed）。首个 POST 成功后即尽力打开该长连；服务器以 405 或
// 非 SSE 响应表示不提供推送，此时不再重试，其余断开按重连开关退避重连。

// errStreamUnsupported 表示服务器不提供服务器推送事件流（无需重试）。
var errStreamUnsupported = errors.New("mcp: server does not offer an event stream")

// 服务器推送事件流的重连退避上下限。
const (
	streamMinBackoff = 1 * time.Second
	streamMaxBackoff = 30 * time.Second
)

type httpTransport struct {
	cfg    ServerConfig
	client *http.Client
	// streamClient 与 client 共享底层传输（同一 SSRF/TLS 拨号），但不设整体
	// 超时，以便长连的事件流不被单次请求超时打断。
	streamClient *http.Client

	mu        sync.Mutex
	handler   func([]byte)
	sessionID string

	baseCtx    context.Context
	baseCancel context.CancelFunc

	streamOnce sync.Once

	closeOnce sync.Once
}

// newHTTPTransport 构造 http 传输（含 SSRF/TLS 防护的客户端）。
func newHTTPTransport(cfg ServerConfig) (*httpTransport, error) {
	hc, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &httpTransport{
		cfg:          cfg,
		client:       hc,
		streamClient: &http.Client{Transport: hc.Transport, CheckRedirect: hc.CheckRedirect},
		baseCtx:      ctx,
		baseCancel:   cancel,
	}, nil
}

// Start 记录回调；请求-响应与服务器推送都经此回调上抛。
func (t *httpTransport) Start(handler func([]byte)) error {
	t.mu.Lock()
	t.handler = handler
	t.mu.Unlock()
	return nil
}

// Send 发送一条报文并把收到的响应交给回调。
func (t *httpTransport) Send(ctx context.Context, msg []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.URL, bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("mcp: %s: build request: %w", t.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range expandHeaders(t.cfg.Headers) {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp: %s: request: %w", t.cfg.Name, err)
	}
	defer resp.Body.Close()

	if newSID := resp.Header.Get("Mcp-Session-Id"); newSID != "" {
		t.mu.Lock()
		t.sessionID = newSID
		t.mu.Unlock()
	}
	limit := t.cfg.maxResponseBytes()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("mcp: %s: read response: %w", t.cfg.Name, err)
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("mcp: %s: response exceeds %d bytes", t.cfg.Name, limit)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mcp: %s: http %d: %s", t.cfg.Name, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.dispatchSSE(body)
	} else {
		t.dispatch(body)
	}
	// 会话已建立：尽力打开服务器推送通道（仅一次）。
	t.ensureStream()
	return nil
}

// Close 取消事件流并释放空闲连接（幂等）。
func (t *httpTransport) Close() error {
	t.closeOnce.Do(func() {
		t.baseCancel()
		t.client.CloseIdleConnections()
	})
	return nil
}

// ensureStream 启动一次服务器推送通道（幂等）。
func (t *httpTransport) ensureStream() {
	if t.baseCtx.Err() != nil {
		return
	}
	t.streamOnce.Do(func() { go t.streamLoop() })
}

// streamLoop 维护长连的事件流：服务器明确不支持时退出，其余断开按重连开关
// 退避重试。
func (t *httpTransport) streamLoop() {
	backoff := streamMinBackoff
	for {
		if t.baseCtx.Err() != nil {
			return
		}
		err := t.openStream()
		if errors.Is(err, errStreamUnsupported) {
			logger.Debugf("[MCP] %s: server does not offer a notification event stream", t.cfg.Name)
			return
		}
		if t.baseCtx.Err() != nil {
			return
		}
		if err != nil {
			logger.Debugf("[MCP] %s: event stream ended: %v", t.cfg.Name, err)
		}
		if !t.cfg.reconnect() {
			return
		}
		select {
		case <-t.baseCtx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < streamMaxBackoff {
			backoff *= 2
			if backoff > streamMaxBackoff {
				backoff = streamMaxBackoff
			}
		}
	}
}

// openStream 打开一次 GET 事件流并在其上接收通知，直到断开或被关闭。
func (t *httpTransport) openStream() error {
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()

	ctx, cancel := context.WithCancel(t.baseCtx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("mcp: %s: build stream request: %w", t.cfg.Name, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range expandHeaders(t.cfg.Headers) {
		req.Header.Set(k, v)
	}
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := t.streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp: %s: stream request: %w", t.cfg.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return errStreamUnsupported
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return errStreamUnsupported
	}
	return t.consumeSSE(resp.Body)
}

// dispatch 把一条报文交给回调。
func (t *httpTransport) dispatch(msg []byte) {
	msg = bytes.TrimSpace(msg)
	if len(msg) == 0 {
		return
	}
	t.mu.Lock()
	h := t.handler
	t.mu.Unlock()
	if h != nil {
		h(msg)
	}
}

// dispatchSSE 解析一整段 SSE 事件流（响应内嵌的那类）。
func (t *httpTransport) dispatchSSE(body []byte) {
	if err := t.consumeSSE(bytes.NewReader(body)); err != nil {
		logger.Debugf("[MCP] %s: parse sse response: %v", t.cfg.Name, err)
	}
}

// consumeSSE 增量解析 SSE 事件流：每个事件由若干 data: 行组成，空行分隔。
func (t *httpTransport) consumeSSE(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), int(t.cfg.maxResponseBytes())+1)
	var data bytes.Buffer
	flush := func() {
		if data.Len() == 0 {
			return
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		if len(payload) > 0 {
			t.dispatch(payload)
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(after)
			data.WriteByte('\n')
		}
	}
	flush()
	return sc.Err()
}
