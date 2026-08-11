package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestPlugin(engine string) *Plugin {
	_ = initWebTransport("")
	cfg := DefaultConfig
	cfg.Engine = engine
	p := &Plugin{cfg: &cfg}
	p.client = newWebClient(5 * time.Second)
	p.engine = newSearchEngine(&cfg, p.client)
	return p
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)
	p.engine = newSearchEngine(p.cfg, p.client)

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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)
	p.engine = newSearchEngine(p.cfg, p.client)
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)

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

	orig := ssrfPolicy
	ssrfPolicy = func(string) bool { return true }
	defer func() { ssrfPolicy = orig }()

	// TLSServer 的自签证书：使用其自带的信任客户端。
	text, err := fetchPage(context.Background(), srv.Client(), srv.URL, 1<<20, 4000)
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()

	// http 协议拒绝（TLSServer 的 URL 是 https；构造一个 http URL 验证）
	httpURL := "http://" + strings.TrimPrefix(srv.URL, "https://")
	if _, err := fetchPage(ctx, client, httpURL, 1<<20, 4000); err == nil {
		t.Error("http scheme should be rejected")
	}
	if _, err := fetchPage(ctx, client, "https://127.0.0.1/", 1<<20, 4000); err == nil {
		t.Error("private IP should be rejected")
	}
	if _, err := fetchPage(ctx, client, "https://192.168.1.1/", 1<<20, 4000); err == nil {
		t.Error("private IP should be rejected")
	}
	// 重定向到内网拒绝
	redir := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1/x", http.StatusFound)
	}))
	defer redir.Close()
	if _, err := fetchPage(ctx, redir.Client(), redir.URL, 1<<20, 4000); err == nil {
		t.Error("redirect to private IP should be rejected")
	}
}

func TestFetchPageSizeLimit(t *testing.T) {
	content := strings.Repeat("A", 5000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	orig := ssrfPolicy
	ssrfPolicy = func(string) bool { return true }
	defer func() { ssrfPolicy = orig }()

	text, err := fetchPage(context.Background(), srv.Client(), srv.URL, 1000, 4000)
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

	orig := ssrfPolicy
	ssrfPolicy = func(string) bool { return true }
	defer func() { ssrfPolicy = orig }()

	text, err := fetchPage(context.Background(), srv.Client(), srv.URL, 1<<20, 200)
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
	p.cfg.SearxngURL = srv.URL
	p.engine = newSearchEngine(p.cfg, p.client)
	p.engine = newSearchEngine(p.cfg, p.client)

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
			result, err := tt.Execute(context.Background(), map[string]any{"query": "今日热点"})
			if err != nil {
				t.Fatalf("web_search execute failed: %v", err)
			}
			if !strings.Contains(result, "https://x.com") || !strings.Contains(result, "标题") {
				t.Errorf("unexpected search result: %q", result)
			}
		case "fetch_url":
			// 对 http 地址应报错（仅 https）
			if _, err := tt.Execute(context.Background(), map[string]any{"url": "http://x.com"}); err == nil {
				t.Error("fetch_url should reject http scheme")
			}
		}
	}
}
