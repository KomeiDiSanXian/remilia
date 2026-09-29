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

	"github.com/prometheus/client_golang/prometheus/testutil"
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

// SendStream 模拟长连订阅：先投递服务器对订阅请求的即时应答，再阻塞到连接关闭
// 或 ctx 取消——与真实传输"流随连接存续"的语义一致。
func (m *memTransport) SendStream(ctx context.Context, msg []byte) error {
	if err := m.Send(ctx, msg); err != nil {
		return err
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *memTransport) Close() error {
	m.once.Do(func() { close(m.done) })
	return nil
}

// Wait 让客户端在传输结束时关闭（与真实传输一致）。
func (m *memTransport) Wait() <-chan struct{} { return m.done }

type fakeServer struct {
	mu           sync.Mutex
	tools        []mcpTool
	queued       [][]mcpTool
	protoVersion string
	stateless    bool
	// discoverVersions 是 server/discover 声明支持的版本；为空表示只支持无状态版本。
	discoverVersions []string
	// rejectVersion 让服务器以 UnsupportedProtocolVersionError 拒绝所请求的版本。
	rejectVersion bool
	handler       func([]byte)
	onCall        func(name string, args map[string]any) *callToolResult
	requests      []string
	metas         []map[string]any
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

// scriptLists 为接下来的 tools/list 调用排队固定的返回集合（逐次出队）。
// 队列为空时回退到当前 tools。用于模拟服务器返回"半截"或反复变化的列表。
func (s *fakeServer) scriptLists(lists ...[]mcpTool) {
	s.mu.Lock()
	s.queued = append(s.queued, lists...)
	s.mu.Unlock()
}

// setProtocolVersion 设定 initialize 结果里返回的协议版本（空则回显客户端版本）。
func (s *fakeServer) setProtocolVersion(v string) {
	s.mu.Lock()
	s.protoVersion = v
	s.mu.Unlock()
}

// setStateless 让服务器实现（或不实现）无状态流程。
func (s *fakeServer) setStateless(v bool) {
	s.mu.Lock()
	s.stateless = v
	s.mu.Unlock()
}

// setDiscoverVersions 设置 server/discover 声明支持的版本。
func (s *fakeServer) setDiscoverVersions(v ...string) {
	s.mu.Lock()
	s.discoverVersions = v
	s.mu.Unlock()
}

// setRejectVersion 让服务器以 UnsupportedProtocolVersionError 拒绝所请求的版本。
func (s *fakeServer) setRejectVersion(v bool) {
	s.mu.Lock()
	s.rejectVersion = v
	s.mu.Unlock()
}

func (s *fakeServer) recordRequest(method string, meta map[string]any) {
	s.mu.Lock()
	s.requests = append(s.requests, method)
	s.metas = append(s.metas, meta)
	s.mu.Unlock()
}

// sawMethod 报告是否收到过某方法的请求。
func (s *fakeServer) sawMethod(method string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.requests, method)
}

// lastMeta 返回某方法最后一次请求携带的 _meta（无则 nil）。
func (s *fakeServer) lastMeta(method string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range slices.Backward(s.requests) {
		if v == method {
			return s.metas[i]
		}
	}
	return nil
}

func (s *fakeServer) notifyToolsChanged() {
	b, _ := encodeNotification(methodToolsListChanged, nil)
	s.push(b)
}

// handle 处理一条客户端报文；通知返回 nil（无需响应）。
func (s *fakeServer) handle(raw []byte) []byte {
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	if env.Method != "" {
		var carrier struct {
			Meta map[string]any `json:"_meta"`
		}
		_ = json.Unmarshal(env.Params, &carrier)
		s.recordRequest(env.Method, carrier.Meta)
	}
	// 订阅是长连：先回一条 ack 通知（无 id），流的生命周期由传输维持。
	if env.Method == methodSubscriptionsListen && env.ID != nil {
		ack, _ := encodeNotification(methodSubscriptionsAcknowledged, map[string]any{
			"_meta":         map[string]any{metaSubscriptionID: *env.ID},
			"notifications": map[string]any{"toolsListChanged": true},
		})
		return ack
	}
	if env.ID == nil {
		return nil
	}
	resp := rpcResponse{JSONRPC: jsonrpcVersion, ID: env.ID}
	switch env.Method {
	case methodInitialize:
		s.mu.Lock()
		pv := s.protoVersion
		s.mu.Unlock()
		if pv == "" {
			pv = HandshakeProtocolVersion
		}
		resp.Result, _ = json.Marshal(initializeResult{
			ProtocolVersion: pv,
			ServerInfo:      serverInfo{Name: "fake", Version: "1.0"},
		})
	case methodDiscover:
		s.mu.Lock()
		stateless := s.stateless
		reject := s.rejectVersion
		versions := append([]string(nil), s.discoverVersions...)
		s.mu.Unlock()
		if len(versions) == 0 {
			versions = []string{StatelessProtocolVersion}
		}
		if !stateless {
			// 握手时代的服务器不认识它，只回一个实现自定义（非现代）的错误。
			resp.Error = &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
			break
		}
		if reject {
			resp.Error = &rpcError{
				Code:    rpcCodeUnsupportedProtocolVersion,
				Message: "Unsupported protocol version",
				Data:    map[string]any{"supported": versions, "requested": StatelessProtocolVersion},
			}
			break
		}
		resp.Result, _ = json.Marshal(discoverResult{
			ResultType:        resultTypeComplete,
			SupportedVersions: versions,
			Capabilities:      map[string]any{"tools": map[string]any{}},
			Meta:              resultMeta{ServerInfo: serverInfo{Name: "fake", Version: "1.0"}},
		})
	case methodListTools:
		s.mu.Lock()
		var tools []mcpTool
		if len(s.queued) > 0 {
			tools = append([]mcpTool(nil), s.queued[0]...)
			s.queued = s.queued[1:]
		} else {
			tools = append([]mcpTool(nil), s.tools...)
		}
		s.mu.Unlock()
		resp.Result, _ = json.Marshal(listToolsResult{Tools: tools, ResultType: resultTypeComplete})
	case methodCallTool:
		var p callToolParams
		_ = json.Unmarshal(env.Params, &p)
		var res *callToolResult
		if s.onCall != nil {
			res = s.onCall(p.Name, p.Arguments)
		} else {
			res = &callToolResult{Content: []contentItem{{Type: "text", Text: "ok"}}}
		}
		res.ResultType = resultTypeComplete
		resp.Result, _ = json.Marshal(res)
	default:
		resp.Error = &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
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

// stdioConfig 构造一个通过校验的 stdio 服务器配置。
func stdioConfig(name string, tweak func(*ServerConfig)) *Config {
	sc := ServerConfig{
		Name:      name,
		Transport: "stdio",
		Command:   "test-mcp",
		Timeout:   Duration(2 * time.Second),
		// 初始重连间隔取小值，避免测试等待退避。
		ReconnectInterval: Duration(10 * time.Millisecond),
		// 稳定窗口取小值，避免测试等待。
		ToolStabilizeWindow: Duration(20 * time.Millisecond),
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
	ctx := t.Context()
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
	ctx := t.Context()
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

func TestManagerStabilizesToolListBeforeAdopting(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"})
	f := &transportFactory{server: srv}
	installFactory(t, f)

	mgr, err := NewManager(stdioConfig("stab", nil))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "initial tool list")
	adoptedBefore := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("stab", "adopted"))

	// 集合变化不得在稳定窗口内被立即采用。
	srv.setTools(mcpTool{Name: "a"}, mcpTool{Name: "b"})
	srv.notifyToolsChanged()
	if got := len(mgr.ListTools()); got != 1 {
		t.Fatalf("change adopted before the stabilize window: %d tools", got)
	}

	// 窗口另一端复读到同样的集合后才切换。
	waitFor(t, func() bool { return len(mgr.ListTools()) == 2 }, "stabilized adoption")
	if got := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("stab", "adopted")) - adoptedBefore; got != 1 {
		t.Fatalf("adopted delta = %v, want 1", got)
	}
}

func TestManagerDoesNotAdoptJitteryToolList(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"})
	f := &transportFactory{server: srv}
	installFactory(t, f)

	mgr, err := NewManager(stdioConfig("jitter", nil))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "initial tool list")
	jitterBefore := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("jitter", "jitter"))
	adoptedBefore := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("jitter", "adopted"))

	// 窗口开始时读到"半截"列表，窗口另一端又变回原样：视为抖动，不切换。
	srv.scriptLists([]mcpTool{{Name: "a"}, {Name: "b"}})
	srv.notifyToolsChanged()

	// 远大于数个稳定窗口后集合仍应保持原样（不推进目录代数）。
	time.Sleep(300 * time.Millisecond)
	tools := mgr.ListTools()
	if len(tools) != 1 || tools[0].Name != "mcp_jitter_a" {
		t.Fatalf("jittery list must not be adopted, got %+v", tools)
	}
	if got := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("jitter", "jitter")) - jitterBefore; got != 1 {
		t.Fatalf("jitter delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(mcpToolListChanges.WithLabelValues("jitter", "adopted")) - adoptedBefore; got != 0 {
		t.Fatalf("adopted delta = %v, want 0 (jitter must not be adopted)", got)
	}
}

// TestSupportedProtocolVersions 锁定版本声明范围：两条流程各自的版本都在声明
// 之列，且按新→旧排列（协商时取最新的一版）。
func TestSupportedProtocolVersions(t *testing.T) {
	for _, v := range []string{StatelessProtocolVersion, HandshakeProtocolVersion} {
		if !SupportsProtocolVersion(v) {
			t.Fatalf("offered version %q must be in the supported list", v)
		}
	}
	if SupportsProtocolVersion("") {
		t.Error("empty version must not be reported as supported")
	}
	if SupportedProtocolVersions[0] != StatelessProtocolVersion {
		t.Errorf("versions must be listed newest first, got %v", SupportedProtocolVersions)
	}
}

func TestBestSupportedVersionPicksNewest(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
		ok   bool
	}{
		{"stateless wins", []string{"2025-06-18", StatelessProtocolVersion}, StatelessProtocolVersion, true},
		{"handshake only", []string{"2025-03-26", HandshakeProtocolVersion}, HandshakeProtocolVersion, true},
		{"nothing in common", []string{"2024-11-05", "1900-01-01"}, "", false},
		{"empty", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := bestSupportedVersion(c.in)
			if got != c.want || ok != c.ok {
				t.Fatalf("bestSupportedVersion(%v) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

// ===== 无状态流程 =====

func TestStatelessSessionConnectsExecutesAndSubscribes(t *testing.T) {
	srv := newFakeServer(mcpTool{
		Name:        "search",
		Description: "search things",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	})
	srv.setStateless(true)
	srv.onCall = func(name string, _ map[string]any) *callToolResult {
		return &callToolResult{Content: []contentItem{{Type: "text", Text: "result for " + name}}}
	}
	installFactory(t, &transportFactory{server: srv})

	negBefore := testutil.ToFloat64(mcpProtocolNegotiations.WithLabelValues("stateless", StatelessProtocolVersion))
	fallbacksBefore := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("stateless"))

	mgr, err := NewManager(stdioConfig("stateless", func(sc *ServerConfig) {
		sc.AllowStateless = true
		sc.Reconnect = new(false)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "stateless tool materialization")

	if !srv.sawMethod(methodDiscover) {
		t.Error("the stateless flow must probe server/discover")
	}
	if !srv.sawMethod(methodSubscriptionsListen) {
		t.Error("the stateless flow must subscribe to tool list changes")
	}
	if srv.sawMethod(methodInitialize) {
		t.Error("the stateless flow must not perform an initialize handshake")
	}
	if delta := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("stateless")) - fallbacksBefore; delta != 0 {
		t.Errorf("a stateless server must not trigger a fallback, delta = %v", delta)
	}
	if delta := testutil.ToFloat64(mcpProtocolNegotiations.WithLabelValues("stateless", StatelessProtocolVersion)) - negBefore; delta != 1 {
		t.Errorf("negotiation delta = %v, want 1", delta)
	}
	// 每个请求都必须携带版本、客户端能力与身份。
	for _, method := range []string{methodDiscover, methodListTools} {
		meta := srv.lastMeta(method)
		if meta == nil {
			t.Fatalf("%s must carry _meta", method)
		}
		if got := meta[metaProtocolVersion]; got != StatelessProtocolVersion {
			t.Errorf("%s: %s = %v, want %q", method, metaProtocolVersion, got, StatelessProtocolVersion)
		}
		if _, ok := meta[metaClientCapabilities]; !ok {
			t.Errorf("%s: %s is required", method, metaClientCapabilities)
		}
		if _, ok := meta[metaClientInfo]; !ok {
			t.Errorf("%s: %s is required", method, metaClientInfo)
		}
	}

	tool := mgr.ListTools()[0]
	if tool.Name != "mcp_stateless_search" {
		t.Fatalf("model function name = %q, want mcp_stateless_search", tool.Name)
	}
	if !tool.RequiresApproval {
		t.Error("external server tools must still require approval")
	}
	res, err := tool.ExecuteRich(context.Background(), map[string]any{"q": "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := res.Flatten(); got != "result for search" {
		t.Fatalf("flattened result = %q", got)
	}
	if meta := srv.lastMeta(methodCallTool); meta == nil || meta[metaProtocolVersion] != StatelessProtocolVersion {
		t.Errorf("tools/call must carry the stateless _meta, got %v", meta)
	}
}

func TestStatelessProbeFallsBackToHandshake(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"}) // 未实现无状态流程
	installFactory(t, &transportFactory{server: srv})

	fallbacksBefore := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("fallback"))
	negBefore := testutil.ToFloat64(mcpProtocolNegotiations.WithLabelValues("fallback", HandshakeProtocolVersion))

	mgr, err := NewManager(stdioConfig("fallback", func(sc *ServerConfig) {
		sc.AllowStateless = true
		sc.Reconnect = new(false)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "fallback tool materialization")

	if !srv.sawMethod(methodDiscover) {
		t.Error("the probe must be attempted first")
	}
	if !srv.sawMethod(methodInitialize) {
		t.Error("a non-modern server must fall back to the initialize handshake")
	}
	if srv.sawMethod(methodSubscriptionsListen) {
		t.Error("no subscription may be opened on a handshake-era server")
	}
	if delta := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("fallback")) - fallbacksBefore; delta != 1 {
		t.Errorf("fallback delta = %v, want 1", delta)
	}
	if delta := testutil.ToFloat64(mcpProtocolNegotiations.WithLabelValues("fallback", HandshakeProtocolVersion)) - negBefore; delta != 1 {
		t.Errorf("handshake negotiation delta = %v, want 1", delta)
	}
}

// TestStatelessProbeFallsBackWhenVersionRejected 覆盖"对端是现代实现但不支持
// 所选版本"：服务器以 UnsupportedProtocolVersionError 作答，客户端据此改用
// 握手流程（而不是把它当成协议错误直接失败）。
func TestStatelessProbeFallsBackWhenVersionRejected(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"})
	srv.setStateless(true)
	srv.setDiscoverVersions(HandshakeProtocolVersion)
	srv.setRejectVersion(true)
	installFactory(t, &transportFactory{server: srv})

	fallbacksBefore := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("rejected"))

	mgr, err := NewManager(stdioConfig("rejected", func(sc *ServerConfig) {
		sc.AllowStateless = true
		sc.Reconnect = new(false)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "fallback tool materialization")

	if !srv.sawMethod(methodInitialize) {
		t.Error("a server rejecting the version must fall back to the initialize handshake")
	}
	if srv.sawMethod(methodSubscriptionsListen) {
		t.Error("no subscription may be opened when the stateless flow was rejected")
	}
	if delta := testutil.ToFloat64(mcpProtocolFallbacks.WithLabelValues("rejected")) - fallbacksBefore; delta != 1 {
		t.Errorf("fallback delta = %v, want 1", delta)
	}
}

func TestStatelessDisabledByDefault(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "a"})
	srv.setStateless(true)
	installFactory(t, &transportFactory{server: srv})

	mgr, err := NewManager(stdioConfig("handshake-only", func(sc *ServerConfig) {
		sc.Reconnect = new(false)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "handshake tool materialization")

	if srv.sawMethod(methodDiscover) {
		t.Error("the stateless probe must be opt-in (allow_stateless)")
	}
	if !srv.sawMethod(methodInitialize) {
		t.Error("the handshake flow must be used by default")
	}
}

func TestManagerKeepsToolsWhenDisconnected(t *testing.T) {
	srv := newFakeServer(mcpTool{Name: "search"})
	f := &transportFactory{server: srv}
	installFactory(t, f)

	// 关闭重连：断开后停留在软不可用，便于断言。
	mgr, err := NewManager(stdioConfig("srv", func(sc *ServerConfig) { sc.Reconnect = new(false) }))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := t.Context()
	mgr.Start(ctx)
	defer mgr.Close()

	waitFor(t, func() bool { return len(mgr.ListTools()) == 1 }, "tool materialization")
	if up := testutil.ToFloat64(mcpServerUp.WithLabelValues("srv")); up != 1 {
		t.Fatalf("server_up = %v, want 1 while connected", up)
	}
	waitFor(t, func() bool { return f.last() != nil }, "transport creation")
	if err := f.last().Close(); err != nil {
		t.Fatalf("close transport: %v", err)
	}

	unavailableBefore := testutil.ToFloat64(mcpUnavailableCalls.WithLabelValues("srv"))
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
	if up := testutil.ToFloat64(mcpServerUp.WithLabelValues("srv")); up != 0 {
		t.Fatalf("server_up = %v, want 0 after disconnect", up)
	}
	if got := testutil.ToFloat64(mcpUnavailableCalls.WithLabelValues("srv")) - unavailableBefore; got < 1 {
		t.Fatalf("unavailable call delta = %v, want >= 1", got)
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
