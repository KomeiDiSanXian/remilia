package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// transport_http.go — Streamable HTTP 传输。
//
// 每条报文以 POST 发送；响应既可能是单条 JSON，也可能是 SSE 事件流（逐条
// 回调）。会话 id 由服务器在响应头 Mcp-Session-Id 下发，后续请求带上。
//
// 说明：服务器经独立通道主动推送的通知（如长连 SSE）在此为尽力而为——单条
// JSON 响应与响应内 SSE 均被完整处理；工具集合的最终一致依靠工具列表刷新。

type httpTransport struct {
	cfg    ServerConfig
	client *http.Client

	mu        sync.Mutex
	handler   func([]byte)
	sessionID string

	closeOnce sync.Once
}

// newHTTPTransport 构造 http 传输（含 SSRF/TLS 防护的客户端）。
func newHTTPTransport(cfg ServerConfig) (*httpTransport, error) {
	hc, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &httpTransport{cfg: cfg, client: hc}, nil
}

// Start 记录回调；http 传输的接收由各次请求驱动。
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
		return nil
	}
	t.dispatch(body)
	return nil
}

// Close 释放空闲连接（幂等）。
func (t *httpTransport) Close() error {
	t.closeOnce.Do(func() {
		t.client.CloseIdleConnections()
	})
	return nil
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

// dispatchSSE 解析 SSE 事件流：每个事件由若干 data: 行组成，空行分隔。
func (t *httpTransport) dispatchSSE(body []byte) {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), int(t.cfg.maxResponseBytes())+1)
	var data bytes.Buffer
	flush := func() {
		if data.Len() == 0 {
			return
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		t.dispatch(payload)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimPrefix(line, "data:"))
			data.WriteByte('\n')
		}
	}
	flush()
}
