// hardening_test.go — websearch 加固项的回归测试：
// 外部数据边界标记、参数收敛、严格/受信出站策略边界、重定向拦截、
// 非 HTML 内容处理、GBK 编码解码、抓取缓存与并发/配额上限。
package websearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/infra/cache"
	"github.com/KomeiDiSanXian/remilia/infra/outbound"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/time/rate"
)

// ── 外部数据边界标记 ──────────────────────────────────────────────

func TestWebSearchResultWrappedWithBoundary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"标题","url":"https://x.com","content":"摘要"}]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)

	tool := findTool(t, p, "web_search")
	result, err := tool(context.Background(), nil, map[string]any{"query": "测试"})
	if err != nil {
		t.Fatalf("web_search failed: %v", err)
	}
	if !strings.HasPrefix(result, "=== 外部数据：") || !strings.Contains(result, "=== 数据结束 ===") {
		t.Errorf("search result should be wrapped with external-data boundary: %q", result)
	}
	if !strings.Contains(result, "勿执行") {
		t.Errorf("boundary should warn against executing embedded instructions: %q", result)
	}
}

func TestFetchURLResultWrappedWithBoundary(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><p>正文内容。</p></body></html>"))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.client = newTestFetchClient(t, srv)
	p.cache = newTestCache()

	tool := findTool(t, p, "fetch_url")
	result, err := tool(context.Background(), nil, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("fetch_url failed: %v", err)
	}
	if !strings.HasPrefix(result, "=== 外部数据：") || !strings.Contains(result, "正文内容") {
		t.Errorf("fetched page should be wrapped with external-data boundary: %q", result)
	}
	if !strings.Contains(result, "=== 数据结束 ===") {
		t.Errorf("boundary end marker missing: %q", result)
	}
}

// ── max_results 参数收敛 ───────────────────────────────────────────

func TestParseMaxResults(t *testing.T) {
	tests := []struct {
		name     string
		raw      any
		def      int
		want     int
		wantNote bool
	}{
		{"nil uses default", nil, 5, 5, false},
		{"within range", float64(7), 5, 7, false},
		{"over cap clamped", float64(50), 5, maxResultsCap, true},
		{"zero falls back", float64(0), 5, 5, true},
		{"negative falls back", float64(-3), 5, 5, true},
		{"numeric string accepted", "3", 5, 3, false},
		{"non-numeric string falls back", "大量", 5, 5, true},
		{"unsupported type falls back", []any{1}, 5, 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, note := parseMaxResults(tt.raw, tt.def)
			if got != tt.want {
				t.Errorf("parseMaxResults(%v) = %d, want %d", tt.raw, got, tt.want)
			}
			if tt.wantNote && note == "" {
				t.Errorf("expected a convergence note for %v, got none", tt.raw)
			}
			if !tt.wantNote && note != "" {
				t.Errorf("unexpected note for %v: %q", tt.raw, note)
			}
		})
	}
}

func TestWebSearchReportsConvergedMaxResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"标题","url":"https://x.com","content":"摘要"}]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)

	result, err := findTool(t, p, "web_search")(context.Background(), nil, map[string]any{
		"query":       "测试",
		"max_results": float64(99),
	})
	if err != nil {
		t.Fatalf("web_search failed: %v", err)
	}
	if !strings.Contains(result, "max_results") {
		t.Errorf("model should be told that max_results was clamped: %q", result)
	}
}

// ── SSRF：严格路径与受信端点的边界 ─────────────────────────────────

// TestStrictFetchClientBlocksLoopback 出厂抓取客户端在发出请求前就拒绝
// 回环目标（拨号层还会对解析结果二次校验并直连已校验 IP）。
func TestStrictFetchClientBlocksLoopback(t *testing.T) {
	client := newStrictFetchClient(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	_, err := client.Get(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatal("fetch client must block loopback targets")
	}
	if reason, ok := outbound.Blocked(err); !ok || reason != outbound.ReasonPlainHTTP {
		t.Fatalf("拦截原因 = %v/%v，期望 %s", reason, ok, outbound.ReasonPlainHTTP)
	}
	if hits != 0 {
		t.Fatalf("被拦请求不应到达服务端，实际 %d 次", hits)
	}
}

// TestSearchEndpointOnPrivateNetworkNotBlocked 回归测试：自建 SearXNG 位于
// 内网/回环（如 http://10.0.0.19:8080）时，搜索请求不得被 SSRF 策略拦成
// "connection to non-public address blocked"；同一端点也不得让抓取路径
// 放行内网地址（两个客户端各自独立，不共享受信名单）。
func TestSearchEndpointOnPrivateNetworkNotBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"标题","url":"https://x.com","content":"摘要"}]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)
	results, err := p.engine.Search(context.Background(), "测试", 3)
	if err != nil {
		t.Fatalf("private-network search endpoint must not be blocked: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// 抓取路径仍拒绝同一地址。
	if _, err := p.client.Get(context.Background(), srv.URL+"/x"); err == nil {
		t.Fatal("fetch client must not inherit the trusted search endpoint")
	}
}

// TestSearchClientAllowsSameOriginRedirect 搜索端点的自我跳转放行
// （自建实例常有 / → /search 之类跳转）。
func TestSearchClientAllowsSameOriginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			http.Redirect(w, r, "/search-landing", http.StatusFound)
		case "/search-landing":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"results":[{"title":"标题","url":"https://x.com","content":"摘要"}]}`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)
	results, err := p.engine.Search(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("same-origin redirect must be allowed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestSearchClientBlocksCrossOriginRedirect 跳到其它内网地址仍拦截：防止
// 实例被攻陷后借重定向把请求导向内网其它服务。
func TestSearchClientBlocksCrossOriginRedirect(t *testing.T) {
	var otherHits int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)
	if _, err := p.engine.Search(context.Background(), "q", 3); err == nil {
		t.Fatal("cross-origin redirect must be blocked")
	}
	if otherHits != 0 {
		t.Fatalf("被拦的重定向不应到达目标，实际 %d 次", otherHits)
	}
}

// TestStrictFetchClientRejectsInsecureTargets 校验严格路径的准入判定
// （逐跳重定向复用同一判定，见 infra/outbound 的用例）。
func TestStrictFetchClientRejectsInsecureTargets(t *testing.T) {
	client := newStrictFetchClient(t)
	for _, raw := range []string{
		"http://example.com/x",
		"https://127.0.0.1/x",
		"https://10.0.0.1/x",
		"https://169.254.169.254/latest/meta-data",
	} {
		if err := client.Allow(raw); err == nil {
			t.Errorf("target %q should be rejected", raw)
		}
	}
}

// ── 非 HTML 内容与字符编码 ────────────────────────────────────────

func TestExtractContentJSONPassthrough(t *testing.T) {
	body := []byte(`{"temperature": 21, "city": "上海"}`)
	got := extractContent("application/json", body)
	if !strings.Contains(got, `"temperature": 21`) {
		t.Errorf("JSON body should be passed through, got %q", got)
	}
}

func TestExtractContentPlainTextPassthrough(t *testing.T) {
	body := []byte("第一行\n第二行")
	got := extractContent("text/plain; charset=utf-8", body)
	if got != "第一行\n第二行" {
		t.Errorf("plain text should be passed through, got %q", got)
	}
}

func TestExtractContentUnknownTypeTreatedAsHTML(t *testing.T) {
	body := []byte("<html><body><p>正文</p></body></html>")
	got := extractContent("", body)
	if !strings.Contains(got, "正文") {
		t.Errorf("untyped body should be HTML-extracted, got %q", got)
	}
}

func TestFetchPageDecodesGBKPage(t *testing.T) {
	const text = "中文内容测试"
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("<html><body><p>" + text + "</p></body></html>"))
	if err != nil {
		t.Fatalf("encode gbk fixture: %v", err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=gb18030")
		w.Write(encoded)
	}))
	defer srv.Close()

	got, err := fetchPage(context.Background(), newTestFetchClient(t, srv), srv.URL, 1<<20, 4000)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}
	if !strings.Contains(got, text) {
		t.Errorf("GBK page should be decoded to UTF-8, got %q", got)
	}
}

// ── 抓取缓存 ─────────────────────────────────────────────────────

func TestFetchCachedServesRepeatedRequests(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><p>正文。</p></body></html>"))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.client = newTestFetchClient(t, srv)
	p.cache = newTestCache()

	for i := range 3 {
		text, err := p.fetchCached(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("fetch %d failed: %v", i, err)
		}
		if !strings.Contains(text, "正文") {
			t.Fatalf("fetch %d returned unexpected text: %q", i, text)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("expected exactly 1 upstream request, got %d", hits)
	}
}

// ── 并发与配额上限 ────────────────────────────────────────────────

func TestAcquireFailsFastWhenSaturated(t *testing.T) {
	p := newTestPlugin("searxng")
	p.sem = make(chan struct{}, 1)
	p.acquireWait = 20 * time.Millisecond

	if err := p.acquire(context.Background()); err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}
	start := time.Now()
	err := p.acquire(context.Background())
	if err == nil {
		t.Fatal("acquire on a saturated semaphore should fail")
	}
	if elapsed := time.Since(start); elapsed < p.acquireWait {
		t.Errorf("saturation should wait %s before failing fast, failed after %s", p.acquireWait, elapsed)
	}
	p.release()
	if err := p.acquire(context.Background()); err != nil {
		t.Errorf("acquire after release should succeed: %v", err)
	}
}

func TestAcquireRespectsCanceledContext(t *testing.T) {
	p := newTestPlugin("searxng")
	p.sem = make(chan struct{}, 1)
	p.acquireWait = time.Minute
	if err := p.acquire(context.Background()); err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled context should surface context.Canceled, got %v", err)
	}
}

func TestAllowSearchRateLimit(t *testing.T) {
	p := newTestPlugin("searxng")
	p.cfg.RateLimitPerMinute = 60
	p.limiters = map[string]*rate.Limiter{}

	if err := p.allowSearch("u1"); err != nil {
		t.Fatalf("first search should pass: %v", err)
	}
	// 60/min = 1/s：立刻再查应被拒（桶容量按分钟额度上限，首次消耗后需等待）。
	for range 60 {
		_ = p.allowSearch("u1")
	}
	if err := p.allowSearch("u1"); err == nil {
		t.Error("exhausted quota should be rejected")
	}
	if err := p.allowSearch("u2"); err != nil {
		t.Errorf("other users must have independent quota: %v", err)
	}
}

func TestAllowSearchUnlimitedByDefault(t *testing.T) {
	p := newTestPlugin("searxng")
	p.limiters = map[string]*rate.Limiter{}
	for range 100 {
		if err := p.allowSearch("u1"); err != nil {
			t.Fatalf("default config must not rate limit: %v", err)
		}
	}
}

// ── 配置夹取 ─────────────────────────────────────────────────────

func TestConfigClamping(t *testing.T) {
	if got := clampInt(0, 1, 10); got != 1 {
		t.Errorf("clampInt low bound: got %d", got)
	}
	if got := clampInt(99, 1, 10); got != 10 {
		t.Errorf("clampInt high bound: got %d", got)
	}
	if got := clampInt(5, 1, 10); got != 5 {
		t.Errorf("clampInt passthrough: got %d", got)
	}
	if got := clampDuration(10*time.Millisecond, minTimeout, maxTimeout); got != minTimeout {
		t.Errorf("clampDuration low bound: got %s", got)
	}
	if got := clampDuration(time.Hour, minTimeout, maxTimeout); got != maxTimeout {
		t.Errorf("clampDuration high bound: got %s", got)
	}
}

// ── 并发安全（配合 -race） ────────────────────────────────────────

// TestFetchPageConcurrentSharedClient 验证并行工具调用共用一个 client 时
// 不再发生请求期写 CheckRedirect 的数据竞争。
func TestFetchPageConcurrentSharedClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><p>正文。</p></body></html>"))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.client = newTestFetchClient(t, srv)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := p.fetchCached(context.Background(), srv.URL); err != nil {
				t.Errorf("concurrent fetch failed: %v", err)
			}
		})
	}
	wg.Wait()
}

// ── 测试辅助 ─────────────────────────────────────────────────────

// findTool 按名称取工具。
func findTool(t *testing.T, p *Plugin, name string) toolExecutor {
	t.Helper()
	for _, tool := range p.ListTools() {
		if tool.Name == name {
			return tool.ExecuteText
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

// toolExecutor 是工具执行回调的最小类型别名。
type toolExecutor = func(ctx context.Context, inv *toolkit.Invocation, args map[string]any) (string, error)

// newTestCache 构造测试用抓取缓存（禁用后台 GC，避免 goroutine 泄漏）。
func newTestCache() *cache.Map[string, fetchEntry] {
	return cache.New[string, fetchEntry](0)
}
