package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		Reconnect:         new(false),
	}}}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
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

// TestHTTPTransportReceivesServerPush 覆盖服务器经独立 GET 事件流主动推送通知：
// 客户端在建连后打开 text/event-stream，并把推送的 tools/list_changed 交给回调。
func TestHTTPTransportReceivesServerPush(t *testing.T) {
	fake := newFakeServer(mcpTool{Name: "ping"})

	type streamReq struct{ accept, session string }
	streams := make(chan streamReq, 1)
	notified := make(chan struct{}, 1)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			select {
			case streams <- streamReq{accept: r.Header.Get("Accept"), session: r.Header.Get("Mcp-Session-Id")}:
			default:
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Mcp-Session-Id", "sess-1")
		resp := fake.handle(body)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer httpSrv.Close()

	cfg := ServerConfig{
		Name:              "push",
		Transport:         "http",
		URL:               httpSrv.URL,
		AllowInsecureHTTP: true,
		Timeout:           Duration(2 * time.Second),
		Reconnect:         new(false),
	}
	cl, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	cl.setOnChange(func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	})
	if err := cl.start(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cl.close()

	select {
	case <-notified:
	case <-time.After(3 * time.Second):
		t.Fatal("server-pushed tools/list_changed was not delivered")
	}
	select {
	case req := <-streams:
		if !strings.Contains(req.accept, "text/event-stream") {
			t.Errorf("GET stream must accept text/event-stream, got %q", req.accept)
		}
		if req.session != "sess-1" {
			t.Errorf("GET stream must carry the session id, got %q", req.session)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not open a server-push stream")
	}
}

// TestHTTPTransportSendsNegotiatedProtocolVersion 覆盖协议版本头：initialize
// 不带版本头，协商完成后（HTTP 传输）后续请求必须携带对端选定的版本。
func TestHTTPTransportSendsNegotiatedProtocolVersion(t *testing.T) {
	const serverVersion = "2025-06-18"

	fake := newFakeServer(mcpTool{Name: "ping"})
	fake.setProtocolVersion(serverVersion)

	problems := make(chan string, 4)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), `"method":"initialize"`):
			if v := r.Header.Get("MCP-Protocol-Version"); v != "" {
				problems <- "initialize must not carry MCP-Protocol-Version, got " + v
			}
		case strings.Contains(string(body), `"method":"tools/list"`):
			if v := r.Header.Get("MCP-Protocol-Version"); v != serverVersion {
				problems <- "tools/list must carry the negotiated MCP-Protocol-Version " + serverVersion + ", got " + v
			}
		}
		resp := fake.handle(body)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer httpSrv.Close()

	cfg := ServerConfig{
		Name:              "pv",
		Transport:         "http",
		URL:               httpSrv.URL,
		AllowInsecureHTTP: true,
		Timeout:           Duration(2 * time.Second),
		Reconnect:         new(false),
	}
	cl, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if err := cl.start(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cl.close()

	if _, err := cl.listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	close(problems)
	for p := range problems {
		t.Error(p)
	}
}

// TestHTTPStatelessHeadersAndNoStream 覆盖无状态流程的 HTTP 约定：每个请求都带
// 协议版本头与方法镜像头（tools/call 还带工具名），且不再打开独立的 GET 事件流
// ——变更通知改由 subscriptions/listen 订阅。
func TestHTTPStatelessHeadersAndNoStream(t *testing.T) {
	fake := newFakeServer(mcpTool{Name: "search"})
	fake.setStateless(true)

	type seen struct{ mirrored, body, version string }
	var mu sync.Mutex
	var got []seen
	var gets int

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mu.Lock()
			gets++
			mu.Unlock()
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Method string `json:"method"`
			Name   string `json:"name"`
		}
		_ = json.Unmarshal(body, &env)
		mu.Lock()
		got = append(got, seen{
			mirrored: r.Header.Get("Mcp-Method"),
			body:     env.Method,
			version:  r.Header.Get("MCP-Protocol-Version"),
		})
		mu.Unlock()
		resp := fake.handle(body)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer httpSrv.Close()

	cfg := ServerConfig{
		Name:              "stateless-http",
		Transport:         "http",
		URL:               httpSrv.URL,
		AllowInsecureHTTP: true,
		AllowStateless:    true,
		Timeout:           Duration(2 * time.Second),
		Reconnect:         new(false),
	}
	cl, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer cl.close()
	ctx := t.Context()
	if err := cl.start(ctx, 2*time.Second); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := cl.watch(ctx); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if _, err := cl.listTools(ctx); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if _, err := cl.callTool(ctx, "search", nil); err != nil {
		t.Fatalf("callTool: %v", err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range got {
			if s.body == methodSubscriptionsListen {
				return true
			}
		}
		return false
	}, "subscription request")

	mu.Lock()
	defer mu.Unlock()
	if gets != 0 {
		t.Errorf("the stateless flow must not open the legacy GET event stream, got %d GET(s)", gets)
	}
	byMethod := make(map[string]seen, len(got))
	for _, s := range got {
		if s.version != StatelessProtocolVersion {
			t.Errorf("%s: MCP-Protocol-Version = %q, want %q", s.body, s.version, StatelessProtocolVersion)
		}
		if s.mirrored != s.body {
			t.Errorf("Mcp-Method = %q must mirror the body method %q", s.mirrored, s.body)
		}
		byMethod[s.body] = s
	}
	for _, want := range []string{methodDiscover, methodListTools, methodCallTool, methodSubscriptionsListen} {
		if _, ok := byMethod[want]; !ok {
			t.Errorf("expected a %s request", want)
		}
	}
	if _, ok := byMethod[methodInitialize]; ok {
		t.Error("the stateless flow must not perform an initialize handshake")
	}
}

// TestMirrorHeaders 覆盖方法镜像头：Mcp-Method 对所有请求必填，Mcp-Name 只在
// tools/call（resources/read、prompts/get）上出现。
func TestMirrorHeaders(t *testing.T) {
	call, _ := encodeRequest(1, methodCallTool, callToolParams{Name: "get_weather"})
	h := http.Header{}
	mirrorHeaders(h, call)
	if h.Get("Mcp-Method") != methodCallTool {
		t.Errorf("Mcp-Method = %q, want %q", h.Get("Mcp-Method"), methodCallTool)
	}
	if h.Get("Mcp-Name") != "get_weather" {
		t.Errorf("Mcp-Name = %q, want get_weather", h.Get("Mcp-Name"))
	}

	list, _ := encodeRequest(2, methodListTools, listToolsParams{})
	h = http.Header{}
	mirrorHeaders(h, list)
	if h.Get("Mcp-Method") != methodListTools {
		t.Errorf("Mcp-Method = %q, want %q", h.Get("Mcp-Method"), methodListTools)
	}
	if h.Get("Mcp-Name") != "" {
		t.Errorf("Mcp-Name must be absent on tools/list, got %q", h.Get("Mcp-Name"))
	}
}

// TestEncodeHeaderValue 锁定 Base64 哨兵编码：无法安全表示的值（非 ASCII、首尾
// 空白、换行）与本身匹配哨兵模式的值都必须编码，取值与规范示例一致。
func TestEncodeHeaderValue(t *testing.T) {
	cases := map[string]string{
		"get_weather":        "get_weather",
		"Hello, 世界":          "=?base64?SGVsbG8sIOS4lueVjA==?=",
		" padded ":           "=?base64?IHBhZGRlZCA=?=",
		"line1\nline2":       "=?base64?bGluZTEKbGluZTI=?=",
		"=?base64?literal?=": "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=",
	}
	for in, want := range cases {
		if got := encodeHeaderValue(in); got != want {
			t.Errorf("encodeHeaderValue(%q) = %q, want %q", in, got, want)
		}
	}
}
