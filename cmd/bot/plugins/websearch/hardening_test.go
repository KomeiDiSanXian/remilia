// hardening_test.go — websearch 加固项的回归测试：
// 外部数据边界标记、参数收敛、拨号层 SSRF 守卫、重定向降级拦截、
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

	"github.com/KomeiDiSanXian/remilia/infra/cache"
	"github.com/KomeiDiSanXian/remilia/infra/netguard"
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)

	tool := findTool(t, p, "web_search")
	result, err := tool(context.Background(), map[string]any{"query": "测试"})
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

	restore := allowLocalSSRF()
	defer restore()

	p := newTestPlugin("searxng")
	p.client = srv.Client()
	p.cache = newTestCache()

	tool := findTool(t, p, "fetch_url")
	result, err := tool(context.Background(), map[string]any{"url": srv.URL})
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)

	result, err := findTool(t, p, "web_search")(context.Background(), map[string]any{
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

// ── SSRF：拨号层守卫与重定向降级 ───────────────────────────────────

// TestDefaultDialGuardBlocksLoopback 验证出厂配置下，Transport 的拨号守卫
// 阻止连接回环地址（学校验之后的 DNS 重绑定窗口由这一层兜住）。
func TestDefaultDialGuardBlocksLoopback(t *testing.T) {
	tr := guardDial(http.DefaultTransport.(*http.Transport))
	if tr.DialContext == nil {
		t.Fatal("guarded transport must set DialContext")
	}
	_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("dial to loopback should be blocked by the default guard")
	}
}

// TestClientRedirectPolicyBlocksSchemeDowngrade 验证客户端拒绝 https→http
// 与到内网地址的重定向（逐跳校验，防入口校验被重定向绕过）。
func TestClientRedirectPolicyBlocksSchemeDowngrade(t *testing.T) {
	p := newTestPlugin("searxng")
	policy := p.client.CheckRedirect
	if policy == nil {
		t.Fatal("client must carry a redirect policy")
	}

	req, err := http.NewRequest(http.MethodGet, "http://insecure.example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy(req, nil); err == nil {
		t.Error("https→http redirect should be blocked")
	}

	req, err = http.NewRequest(http.MethodGet, "https://127.0.0.1/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy(req, nil); err == nil {
		t.Error("redirect to loopback should be blocked")
	}

	req, err = http.NewRequest(http.MethodGet, "https://example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy(req, nil); err != nil {
		t.Errorf("https→https redirect to a public host should pass, got %v", err)
	}
}

// TestFetchPageRedirectPolicyStillDerivesFromGuard 说明 fetchPage 不再在请求期
// 改共享 client 的 CheckRedirect：重定向策略在构造期固化（并发调用无数据竞争）。
func TestFetchPageRedirectPolicyStillDerivesFromGuard(t *testing.T) {
	p := newTestPlugin("searxng")
	if p.client.CheckRedirect == nil {
		t.Fatal("client constructed without redirect policy")
	}
	if netguard.RedirectPolicy(5) == nil {
		t.Fatal("netguard.RedirectPolicy should never return nil")
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

	restore := allowLocalSSRF()
	defer restore()

	got, err := fetchPage(context.Background(), srv.Client(), srv.URL, 1<<20, 4000)
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

	restore := allowLocalSSRF()
	defer restore()

	p := newTestPlugin("searxng")
	p.client = srv.Client()
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

	restore := allowLocalSSRF()
	defer restore()

	p := newTestPlugin("searxng")
	p.client = srv.Client()

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
			return tool.Execute
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

// toolExecutor 是工具执行回调的最小类型别名。
type toolExecutor = func(ctx context.Context, args map[string]any) (string, error)

// newTestCache 构造测试用抓取缓存（禁用后台 GC，避免 goroutine 泄漏）。
func newTestCache() *cache.Map[string, fetchEntry] {
	return cache.New[string, fetchEntry](0)
}
