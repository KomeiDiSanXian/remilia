package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/outbound"
	"golang.org/x/time/rate"
)

// newTestPlugin 构造测试插件：抓取客户端为出厂严格策略，搜索客户端指向
// 默认配置的端点（测试需要换端点时调用 useEndpoint）。
func newTestPlugin(engine string) *Plugin {
	cfg := DefaultConfig
	cfg.Engine = engine
	p := &Plugin{cfg: &cfg, limiters: make(map[string]*rate.Limiter)}
	p.client, _ = newWebClient(5*time.Second, "")
	p.searchClient, _ = newSearchClient(5*time.Second, "", cfg.searchEndpoint())
	p.sem = make(chan struct{}, cfg.MaxConcurrent)
	p.engine = newSearchEngine(&cfg, p.searchClient)
	return p
}

// useEndpoint 把搜索端点切到测试服务器并重建搜索客户端（受信端点）。
func (p *Plugin) useEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	client, err := newSearchClient(5*time.Second, "", endpoint)
	if err != nil {
		t.Fatalf("newSearchClient(%q): %v", endpoint, err)
	}
	p.cfg.SearxngURL = endpoint
	p.searchClient = client
	p.engine = newSearchEngine(p.cfg, client)
}

// newTestFetchClient 构造可访问测试服务器（回环）的抓取客户端：只把该
// 服务器声明为受信源，并沿用测试服务器的自签证书配置。
func newTestFetchClient(t *testing.T, srv *httptest.Server) *outbound.Client {
	t.Helper()
	policy := outbound.Policy{
		Component: "websearch.test.fetch",
		Class:     outbound.TrustedOrigin,
		Origins:   []string{srv.URL},
		Timeout:   5 * time.Second,
	}
	if tr, ok := srv.Client().Transport.(*http.Transport); ok {
		policy.TLSConfig = tr.TLSClientConfig
	}
	client, err := outbound.New(policy)
	if err != nil {
		t.Fatalf("构造测试抓取客户端失败: %v", err)
	}
	return client
}

// newStrictFetchClient 构造出厂策略的抓取客户端（禁止内网/明文）。
func newStrictFetchClient(t *testing.T) *outbound.Client {
	t.Helper()
	client, err := newWebClient(5*time.Second, "")
	if err != nil {
		t.Fatalf("newWebClient: %v", err)
	}
	return client
}

func TestSearxngEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("format") != "json" || q.Get("q") != "今日热点" {
			t.Errorf("unexpected query: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[
			{"title":"热点新闻标题","url":"https://example.com/news","content":"新闻摘要内容"},
			{"title":"","url":"https://example.com/empty","content":""},
			{"title":"第二条","url":"https://example.com/2","content":"摘要2"}
		]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)

	results, err := p.engine.Search(context.Background(), "今日热点", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 non-empty results, got %d", len(results))
	}
	if results[0].Title != "热点新闻标题" || results[0].URL != "https://example.com/news" {
		t.Errorf("result mismatch: %+v", results[0])
	}
}

func TestSearxngEngineLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var items []string
		for i := range 8 {
			items = append(items, `{"title":"t`+string(rune('0'+i))+`","url":"https://x.com/`+string(rune('0'+i))+`","content":"s"}`)
		}
		w.Write([]byte(`{"results":[` + strings.Join(items, ",") + `]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)
	results, err := p.engine.Search(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (limit), got %d", len(results))
	}
}

func TestSearxngEngineError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)
	if _, err := p.engine.Search(context.Background(), "q", 5); err == nil {
		t.Error("expected error on non-200")
	}
}

func TestSearxngHTMLFallback(t *testing.T) {
	// 模拟公共实例：format=json 被禁用（返回 HTML 而非 JSON），
	// 无 format 参数时返回结果页。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "json" {
			// 禁用 JSON：返回 HTML 页面（而非 JSON）
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>search</body></html>"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
			<article class="result">
				<h3><a href="https://example.com/a">HTML结果一</a></h3>
				<p class="content">HTML摘要一内容</p>
			</article>
			<article class="result">
				<h3><a href="https://example.com/b">HTML结果二</a></h3>
				<p class="content">HTML摘要二内容</p>
			</article>
			<article class="other">不应解析</article>
		</body></html>`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)

	results, err := p.engine.Search(context.Background(), "测试", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results from HTML fallback, got %d", len(results))
	}
	if results[0].Title != "HTML结果一" || results[0].URL != "https://example.com/a" ||
		!strings.Contains(results[0].Snippet, "HTML摘要一") {
		t.Errorf("HTML result mismatch: %+v", results[0])
	}
}

func TestParseSearxngHTML(t *testing.T) {
	data := []byte(`<html><body>
		<article class="result">
			<h3><a href="https://x.com/1">标题一</a></h3>
			<p class="content">摘要一</p>
		</article>
		<article class="result">
			<h3><a href="https://x.com/2">标题二</a></h3>
			<p class="content">摘要二</p>
		</article>
		<article class="result"><h3><a href="">空结果</a></h3></article>
	</body></html>`)
	results := parseSearxngHTML(data, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 parsed results, got %d", len(results))
	}
	// 条数上限
	limited := parseSearxngHTML(data, 1)
	if len(limited) != 1 {
		t.Errorf("limit should apply, got %d", len(limited))
	}
}

func TestSerperEngineRequiresKey(t *testing.T) {
	p := newTestPlugin("serper")
	if _, err := p.engine.Search(context.Background(), "q", 5); err == nil {
		t.Error("expected error without api key")
	}
}

func TestSerperEngine(t *testing.T) {
	var gotKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-KEY")
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"answerBox":{"title":"直接答案","link":"https://a.com","snippet":"答案内容"},
			"organic":[
				{"title":"结果一","link":"https://b.com","snippet":"摘要一"},
				{"title":"结果二","link":"https://c.com","snippet":"摘要二"}
			]
		}`))
	}))
	defer srv.Close()

	orig := serperSearchURL
	serperSearchURL = srv.URL
	defer func() { serperSearchURL = orig }()

	eng := &serperEngine{client: srv.Client(), apiKey: "k123"}
	results, err := eng.Search(context.Background(), "测试查询", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if gotKey != "k123" {
		t.Errorf("expected X-API-KEY header, got %q", gotKey)
	}
	if !strings.Contains(gotBody, "测试查询") {
		t.Errorf("expected query in body, got %q", gotBody)
	}
	// answerBox 置顶 + organic
	if len(results) != 3 {
		t.Fatalf("expected 3 results (answerBox + 2 organic), got %d", len(results))
	}
	if results[0].Title != "直接答案" {
		t.Errorf("answerBox should be first: %+v", results[0])
	}
}

func TestFetchPageExtractsText(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>测试页面</title></head><body>
			<script>var x=1;</script>
			<nav>导航栏内容</nav>
			<p>第一段内容。</p>
			<p>第二段内容！包含<a href="#">链接文字</a>。</p>
			<footer>页脚</footer>
		</body></html>`))
	}))
	defer srv.Close()

	// TLSServer 的自签证书：使用其自带的信任客户端。
	text, err := fetchPage(context.Background(), newTestFetchClient(t, srv), srv.URL, 1<<20, 4000)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}
	if !strings.Contains(text, "测试页面") {
		t.Errorf("title should be extracted: %q", text)
	}
	if !strings.Contains(text, "第一段内容") || !strings.Contains(text, "第二段内容") {
		t.Errorf("paragraphs should be extracted: %q", text)
	}
	if strings.Contains(text, "var x") || strings.Contains(text, "导航栏") || strings.Contains(text, "页脚") {
		t.Errorf("script/nav/footer should be skipped: %q", text)
	}
}

func TestFetchPageSSRF(t *testing.T) {
	client := newStrictFetchClient(t)
	ctx := context.Background()

	if _, err := fetchPage(ctx, client, "http://example.com/x", 1<<20, 4000); err == nil {
		t.Error("http scheme should be rejected")
	}
	if _, err := fetchPage(ctx, client, "https://127.0.0.1/", 1<<20, 4000); err == nil {
		t.Error("private IP should be rejected")
	}
	if _, err := fetchPage(ctx, client, "https://192.168.1.1/", 1<<20, 4000); err == nil {
		t.Error("private IP should be rejected")
	}
	if _, err := fetchPage(ctx, client, "file:///etc/passwd", 1<<20, 4000); err == nil {
		t.Error("non-http scheme should be rejected")
	}
	// 重定向到内网：严格客户端逐跳拒绝（准入在发出第二跳前完成）。
	redir := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1/x", http.StatusFound)
	}))
	defer redir.Close()
	if _, err := fetchPage(ctx, client, redir.URL, 1<<20, 4000); err == nil {
		t.Error("redirect to private IP should be rejected")
	}
}

func TestFetchPageSizeLimit(t *testing.T) {
	content := strings.Repeat("A", 5000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	text, err := fetchPage(context.Background(), newTestFetchClient(t, srv), srv.URL, 1000, 4000)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}
	if len(text) >= 4000 {
		t.Errorf("size limit should truncate response, got %d chars", len(text))
	}
}

func TestFetchPageRuneTruncation(t *testing.T) {
	body := "<p>" + strings.Repeat("内容内容", 500) + "</p>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	text, err := fetchPage(context.Background(), newTestFetchClient(t, srv), srv.URL, 1<<20, 200)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}
	if len([]rune(text)) > 220 {
		t.Errorf("runes should be truncated to ~200, got %d", len([]rune(text)))
	}
	if !strings.Contains(text, "已截断") {
		t.Errorf("truncation marker expected: %q", text)
	}
}

func TestExtractPageTextFallback(t *testing.T) {
	text := extractPageText([]byte("<html><p>你好</p>世界"))
	if !strings.Contains(text, "你好") || !strings.Contains(text, "世界") {
		t.Errorf("fallback extraction should keep text: %q", text)
	}
}

func TestFormatResults(t *testing.T) {
	out := formatResults("测试", []Result{
		{Title: "标题一", URL: "https://a.com", Snippet: "摘要一"},
	})
	if !strings.Contains(out, "测试") || !strings.Contains(out, "标题一") ||
		!strings.Contains(out, "https://a.com") || !strings.Contains(out, "摘要一") {
		t.Errorf("format mismatch: %q", out)
	}
}

func TestListToolsAndWebSearchExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"标题","url":"https://x.com","content":"摘要"}]}`))
	}))
	defer srv.Close()

	p := newTestPlugin("searxng")
	p.useEndpoint(t, srv.URL)

	tools := p.ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Name] = true
	}
	if !names["web_search"] || !names["fetch_url"] {
		t.Errorf("expected web_search/fetch_url tools, got %v", names)
	}

	for _, tt := range tools {
		switch tt.Name {
		case "web_search":
			result, err := tt.ExecuteText(context.Background(), nil, map[string]any{"query": "今日热点"})
			if err != nil {
				t.Fatalf("web_search execute failed: %v", err)
			}
			if !strings.Contains(result, "https://x.com") || !strings.Contains(result, "标题") {
				t.Errorf("unexpected search result: %q", result)
			}
		case "fetch_url":
			// 对 http 地址应报错（仅 https）
			if _, err := tt.ExecuteText(context.Background(), nil, map[string]any{"url": "http://x.com"}); err == nil {
				t.Error("fetch_url should reject http scheme")
			}
		}
	}
}
