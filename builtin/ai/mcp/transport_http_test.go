package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// TestHTTPTransportRoundTrip 通过真实的 Streamable HTTP 传输跑完
// initialize → tools/list → tools/call 全链路，覆盖 SSRF 拨号路径（回环地址需
// 显式放行）与响应头会话 id 的透传。
func TestHTTPTransportRoundTrip(t *testing.T) {
	fake := newFakeServer(mcpTool{Name: "ping", Description: "ping"})

	var sawSession bool
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Mcp-Session-Id") != "" {
			sawSession = true
		}
		w.Header().Set("Mcp-Session-Id", "sess-1")
		resp := fake.handle(body)
		if resp == nil { // 通知：无需响应体
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer httpSrv.Close()

	cfg := &Config{Servers: []ServerConfig{{
		Name:              "h",
		Transport:         "http",
		URL:               httpSrv.URL,
		AllowInsecureHTTP: true, // httptest 服务是回环明文地址
		Timeout:           Duration(2 * time.Second),
		Reconnect:         boolPtr(false),
	}}}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "http tool materialization")
	tool := mgr.ListTools()[0]
	if tool.Name != "mcp_h_ping" || tool.Source != toolkit.MCPSource("h") {
		t.Fatalf("unexpected tool: %+v", tool)
	}
	if !sawSession {
		t.Error("session id from the server must be echoed on subsequent requests")
	}

	if _, err := tool.ExecuteRich(context.Background(), nil); err != nil {
		t.Fatalf("http tool execution failed: %v", err)
	}
}

// TestHTTPTransportParsesSSE 覆盖 SSE 事件流响应：内容以 text/event-stream 返回时
// 逐事件解析并回调。
func TestHTTPTransportParsesSSE(t *testing.T) {
	fake := newFakeServer(mcpTool{Name: "ping"})
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := fake.handle(body)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: "))
		_, _ = w.Write(resp)
		_, _ = w.Write([]byte("\n\n"))
	}))
	defer httpSrv.Close()

	cfg := ServerConfig{Name: "s", Transport: "http", URL: httpSrv.URL, AllowInsecureHTTP: true, Timeout: Duration(2 * time.Second)}
	cl, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer cl.close()
	if err := cl.start(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("initialize over SSE failed: %v", err)
	}
	tools, err := cl.listTools(context.Background())
	if err != nil {
		t.Fatalf("tools/list over SSE failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("SSE response not parsed: %+v", tools)
	}
}
