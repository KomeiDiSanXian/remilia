package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ===== 内存传输与假服务器 =====
//
// 这些测试注入包级 [newTransport]，用一个内存传输 + 假服务器把"外部工具服务器"
// 的协议行为本地化，从而在无网络、无子进程的条件下覆盖连接、刷新、断开与结果
// 转换。安全边界（SSRF/命令白名单/环境变量）用纯函数测试覆盖。

type memTransport struct {
	server *fakeServer
	done   chan struct{}
	once   sync.Once
}

func newMemTransport(s *fakeServer) *memTransport {
	return &memTransport{server: s, done: make(chan struct{})}
}

func (m *memTransport) Start(handler func([]byte)) error {
	m.server.attach(handler)
	return nil
}

func (m *memTransport) Send(_ context.Context, msg []byte) error {
	select {
	case <-m.done:
		return context.Canceled
	default:
	}
	if resp := m.server.handle(msg); resp != nil {
		m.server.push(resp)
	}
	return nil
}

func (m *memTransport) Close() error {
	m.once.Do(func() { close(m.done) })
	return nil
}

// Wait 让客户端在传输结束时关闭（与真实传输一致）。
func (m *memTransport) Wait() <-chan struct{} { return m.done }

type fakeServer struct {
	mu      sync.Mutex
	tools   []mcpTool
	handler func([]byte)
	onCall  func(name string, args map[string]any) *callToolResult
}

func newFakeServer(tools ...mcpTool) *fakeServer { return &fakeServer{tools: tools} }

func (s *fakeServer) attach(h func([]byte)) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}

func (s *fakeServer) push(msg []byte) {
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	if h != nil {
		h(msg)
	}
}

func (s *fakeServer) setTools(tools ...mcpTool) {
	s.mu.Lock()
	s.tools = tools
	s.mu.Unlock()
}

func (s *fakeServer) notifyToolsChanged() {
	b, _ := encodeNotification(methodToolsListChanged, nil)
	s.push(b)
}

// handle 处理一条客户端报文；通知返回 nil（无需响应）。
func (s *fakeServer) handle(raw []byte) []byte {
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.ID == nil {
		return nil
	}
	resp := rpcResponse{JSONRPC: jsonrpcVersion, ID: env.ID}
	switch env.Method {
	case methodInitialize:
		resp.Result, _ = json.Marshal(initializeResult{
			ProtocolVersion: ProtocolVersion,
			ServerInfo:      serverInfo{Name: "fake", Version: "1.0"},
		})
	case methodListTools:
		s.mu.Lock()
		tools := append([]mcpTool(nil), s.tools...)
		s.mu.Unlock()
		resp.Result, _ = json.Marshal(listToolsResult{Tools: tools})
	case methodCallTool:
		var p callToolParams
		_ = json.Unmarshal(env.Params, &p)
		var res *callToolResult
		if s.onCall != nil {
			res = s.onCall(p.Name, p.Arguments)
		} else {
			res = &callToolResult{Content: []contentItem{{Type: "text", Text: "ok"}}}
		}
		resp.Result, _ = json.Marshal(res)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	out, _ := json.Marshal(resp)
	return out
}

// transportFactory 记录每次由客户端构造的传输，便于测试主动断开。
type transportFactory struct {
	mu      sync.Mutex
	server  *fakeServer
	created []*memTransport
}

func (f *transportFactory) new(_ ServerConfig) (Transport, error) {
	mt := newMemTransport(f.server)
	f.mu.Lock()
	f.created = append(f.created, mt)
	f.mu.Unlock()
	return mt, nil
}

func (f *transportFactory) last() *memTransport {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.created) == 0 {
		return nil
	}
	return f.created[len(f.created)-1]
}

// installFactory 注入内存传输，并在测试结束时还原。
func installFactory(t *testing.T, f *transportFactory) {
	t.Helper()
	orig := newTransport
	newTransport = f.new
	t.Cleanup(func() { newTransport = orig })
}

func boolPtr(v bool) *bool { return &v }

// stdioConfig 构造一个通过校验的 stdio 服务器配置。
func stdioConfig(name string, tweak func(*ServerConfig)) *Config {
	sc := ServerConfig{
		Name:      name,
		Transport: "stdio",
		Command:   "test-mcp",
		Timeout:   Duration(2 * time.Second),
		// 初始重连间隔取小值，避免测试等待退避。
		ReconnectInterval: Duration(10 * time.Millisecond),
	}
	if tweak != nil {
		tweak(&sc)
	}
	return &Config{AllowedCommands: []string{"test-mcp"}, Servers: []ServerConfig{sc}}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

// ===== 工具物化与执行 =====

func TestManagerMaterializesToolsAndExecutes(t *testing.T) {
	srv := newFakeServer(mcpTool{
		Name:        "search",
		Description: "search things",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	})
	srv.onCall = func(name string, _ map[string]any) *callToolResult {
		return &callToolResult{Content: []contentItem{{Type: "text", Text: "result for " + name}}}
	}
	f := &transportFactory{server: srv}
	installFactory(t, f)

	mgr, err := NewManager(stdioConfig("github", nil))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "tool materialization")
	tool := mgr.ListTools()[0]

	if tool.Name != "mcp_github_search" {
		t.Fatalf("model function name = %q, want mcp_github_search", tool.Name)
	}
	if tool.Source != toolkit.MCPSource("github") {
		t.Fatalf("source = %q, want %q", tool.Source, toolkit.MCPSource("github"))
	}
	if !slices.Contains(tool.Categories, "mcp") || !slices.Contains(tool.Categories, "mcp:github") {
		t.Fatalf("categories = %v", tool.Categories)
	}
	if !tool.RequiresApproval {
		t.Error("external server tools must require approval by default")
	}
	if _, ok := tool.Parameters.Properties["q"]; !ok {
		t.Errorf("input schema not preserved: %+v", tool.Parameters)
	}
	if tool.ExecuteRich == nil {
		t.Fatal("expected rich executor to be bound")
	}

	res, err := tool.ExecuteRich(context.Background(), map[string]any{"q": "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := res.Flatten(); got != "result for search" {
		t.Fatalf("flattened result = %q", got)
	}
}

func TestManagerRefreshesOnToolsListChanged(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"})
	f := &transportFactory{server: srv}
	installFactory(t, f)

	mgr, err := NewManager(stdioConfig("srv", nil))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "initial tool list")

	srv.setTools(mcpTool{Name: "a"}, mcpTool{Name: "b"})
	// 通知可能在 onChange 尚未注册前发出，重试直到目录反映新集合（刷新幂等）。
	waitFor(t, func() bool {
		srv.notifyToolsChanged()
		return len(mgr.ListTools()) == 2
	}, "refresh after tools/list_changed")
}

func TestManagerKeepsToolsWhenDisconnected(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "search"})
	f := &transportFactory{server: srv}
	installFactory(t, f)

	// 关闭重连：断开后停留在软不可用，便于断言。
	mgr, err := NewManager(stdioConfig("srv", func(sc *ServerConfig) { sc.Reconnect = boolPtr(false) }))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "tool materialization")
	waitFor(t, func() bool { return f.last() != nil }, "transport creation")
	if err := f.last().Close(); err != nil {
		t.Fatalf("close transport: %v", err)
	}

	tool := mgr.ListTools()[0]
	// 断开后连接清空有短暂窗口：等待工具调用收敛到"来源暂不可用"的类型化错误。
	waitFor(t, func() bool {
		_, callErr := tool.ExecuteRich(context.Background(), nil)
		return callErr != nil && strings.Contains(callErr.Error(), "不可用")
	}, "soft-unavailable typed error")

	// 工具仍在稳定集合里（软不可用），只是调用失败。
	if got := len(mgr.ListTools()); got != 1 {
		t.Fatalf("tool must stay in catalog while disconnected, got %d", got)
	}
	if _, callErr := tool.ExecuteRich(context.Background(), nil); callErr == nil ||
		!strings.Contains(callErr.Error(), "不可用") {
		t.Fatalf("expected unavailable error, got %v", callErr)
	}
}

// ===== 配置与安全 =====

func TestNewManagerRejectsInvalidServers(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"missing name", &Config{Servers: []ServerConfig{{Transport: "stdio", Command: "x"}}}, "name is required"},
		{"unlisted command", &Config{Servers: []ServerConfig{{Name: "s", Transport: "stdio", Command: "x"}}}, "not in allowed_commands"},
		{"unknown transport", &Config{Servers: []ServerConfig{{Name: "s", Transport: "ws"}}}, "unknown transport"},
		{"missing url", &Config{Servers: []ServerConfig{{Name: "s", Transport: "http"}}}, "url is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewManager(c.cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("expected error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestNewManagerRejectsDuplicateServerNames(t *testing.T) {
	cfg := &Config{
		AllowedCommands: []string{"x"},
		Servers: []ServerConfig{
			{Name: "Files", Transport: "stdio", Command: "x"},
			{Name: "files", Transport: "stdio", Command: "x"},
		},
	}
	if _, err := NewManager(cfg); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate name error, got %v", err)
	}
}

func TestForbidIPRejectsInternalAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "::1", "fe80::1", "0.0.0.0", "224.0.0.1"} {
		if err := forbidIP(net.ParseIP(ip), false, false); err == nil {
			t.Errorf("expected %s to be rejected", ip)
		}
	}
	if err := forbidIP(net.ParseIP("8.8.8.8"), false, false); err != nil {
		t.Errorf("public address should be allowed: %v", err)
	}
}

func TestCheckHTTPURLEnforcesSchemeAndTLS(t *testing.T) {
	if _, err := checkHTTPURL("http://example.com/mcp", false, false); err == nil {
		t.Error("plain http must be rejected unless explicitly allowed")
	}
	if _, err := checkHTTPURL("ftp://example.com", true, false); err == nil {
		t.Error("non-http(s) schemes must be rejected")
	}
	if _, err := checkHTTPURL("https://example.com/mcp", false, false); err != nil {
		t.Errorf("public https should be accepted: %v", err)
	}
	if _, err := checkHTTPURL("http://127.0.0.1:9/mcp", true, false); err != nil {
		t.Errorf("loopback http with allow_insecure_http should be accepted: %v", err)
	}
}

func TestCommandAllowedIsExactMatch(t *testing.T) {
	allow := []string{"npx", "/usr/local/bin/my-server"}
	if !commandAllowed(allow, "npx") || !commandAllowed(allow, "/usr/local/bin/my-server") {
		t.Error("listed commands must be allowed")
	}
	if commandAllowed(allow, "rm") || commandAllowed(allow, "npx -y evil") {
		t.Error("unlisted or injected commands must be rejected")
	}
}

func TestBuildEnvRestrictsAndExpands(t *testing.T) {
	t.Setenv("MCP_SECRET_TOKEN", "s3cr3t")
	env := buildEnv([]string{"INJECTED=${MCP_SECRET_TOKEN}"}, nil)

	var injected bool
	for _, kv := range env {
		if strings.HasPrefix(kv, "MCP_SECRET_TOKEN=") {
			t.Fatalf("host secret leaked into child env: %q", kv)
		}
		if kv == "INJECTED=s3cr3t" {
			injected = true
		}
	}
	if !injected {
		t.Error("explicit env values must be expanded from the host environment")
	}

	// 显式加入 env_allowlist 的变量才允许透传。
	passed := buildEnv(nil, []string{"MCP_SECRET_TOKEN"})
	if !slices.Contains(passed, "MCP_SECRET_TOKEN=s3cr3t") {
		t.Error("variables listed in env_allowlist must pass through")
	}
}

// ===== 结果转换与策略映射 =====

func TestConvertCallResultPreservesRichParts(t *testing.T) {
	res := &callToolResult{Content: []contentItem{
		{Type: "text", Text: "hello"},
		{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("img")), MimeType: "image/png"},
		{Type: "resource", Resource: &resourceContent{URI: "mem://x", MimeType: "text/plain"}},
	}}
	res.StructuredContent = json.RawMessage(`{"k":1}`)

	got, err := convertCallResult(res)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(got.Parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(got.Parts))
	}
	if got.Parts[0].Kind != toolkit.ResultText || got.Parts[0].Text != "hello" {
		t.Errorf("text part = %+v", got.Parts[0])
	}
	if got.Parts[1].Kind != toolkit.ResultImage || string(got.Parts[1].Data) != "img" {
		t.Errorf("image part = %+v", got.Parts[1])
	}
	if got.Parts[3].Kind != toolkit.ResultStructured {
		t.Errorf("structured part = %+v", got.Parts[3])
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Kind != platform.AttachmentKindImage {
		t.Errorf("media parts must also surface as attachments: %+v", got.Attachments)
	}
}

func TestConvertCallResultIsErrorBecomesError(t *testing.T) {
	_, err := convertCallResult(&callToolResult{
		IsError: true,
		Content: []contentItem{{Type: "text", Text: "boom"}},
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("isError must become an error, got %v", err)
	}
}

func TestDestructiveHintForcesApproval(t *testing.T) {
	yes := true
	no := false
	sc := ServerConfig{Name: "s", RequireApproval: &no}
	s := &serverState{cfg: sc, prefix: "mcp_s_"}

	plain := (&Manager{}).buildTool(s, mcpTool{Name: "x"}, "mcp_s_x")
	if plain.RequiresApproval {
		t.Error("explicit require_approval=false should be honored for non-destructive tools")
	}

	risky := (&Manager{}).buildTool(s, mcpTool{Name: "x", Annotations: &toolAnnotations{DestructiveHint: &yes}}, "mcp_s_x")
	if !risky.RequiresApproval {
		t.Error("destructive hint must tighten approval even when config disables it")
	}
}
