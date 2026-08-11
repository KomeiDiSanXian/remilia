package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// Result 一条网页搜索结果。
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// searchEngine 搜索引擎接口。
type searchEngine interface {
	// Search 执行搜索，返回至多 limit 条结果（按相关性排序）。
	Search(ctx context.Context, query string, limit int) ([]Result, error)
	// Name 引擎名称（用于错误提示）。
	Name() string
}

// newSearchEngine 根据配置创建搜索引擎。
func newSearchEngine(cfg *Config, client *http.Client) searchEngine {
	switch cfg.Engine {
	case "serper":
		return &serperEngine{client: client, apiKey: cfg.SerperAPIKey}
	default:
		return &searxngEngine{client: client, baseURL: cfg.SearxngURL}
	}
}

// --- SearXNG（JSON 优先，HTML 回退） ---
//
// JSON 输出（format=json）需实例开启；多数公共实例为防滥用禁用了它。
// 因此请求失败或响应非 JSON 时自动回退 HTML 输出并解析结果块——
// 自建实例走 JSON，公共实例走 HTML，两种部署都可用。

type searxngEngine struct {
	client  *http.Client
	baseURL string
}

func (e *searxngEngine) Name() string { return "SearXNG" }

type searxngResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *searxngEngine) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	base := strings.TrimRight(e.baseURL, "/")

	// 第一优先：JSON 输出（自建实例通常可用）。
	results, err := e.searchJSON(ctx, base, query, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}

	// 回退：HTML 输出解析（公共实例普遍可用）。
	if htmlResults, herr := e.searchHTML(ctx, base, query, limit); herr == nil && len(htmlResults) > 0 {
		return htmlResults, nil
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("searxng 未返回结果")
}

func (e *searxngEngine) searchJSON(ctx context.Context, base, query string, limit int) ([]Result, error) {
	u := base + "/search?format=json&q=" + url.QueryEscape(query)
	req, err := newRequest(ctx, u)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng 返回状态 %d", resp.StatusCode)
	}
	// 实例禁用了 JSON 输出时返回的是 HTML 页而非 JSON。
	if !jsonLooksLike(string(body)) {
		return nil, fmt.Errorf("searxng JSON 输出未启用（实例禁用了 format=json）")
	}

	var parsed searxngResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("searxng 响应解析失败: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("searxng 错误: %s", parsed.Error.Message)
	}

	out := make([]Result, 0, limit)
	for _, r := range parsed.Results {
		if r.Title == "" && r.Content == "" {
			continue
		}
		out = append(out, Result{Title: strings.TrimSpace(r.Title), URL: r.URL, Snippet: strings.TrimSpace(r.Content)})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// jsonLooksLike 粗略判断响应体是否为 JSON（以 { 或 [ 开头）。
func jsonLooksLike(body string) bool {
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// searchHTML 请求 HTML 输出并解析结果块（<article class="result">）。
func (e *searxngEngine) searchHTML(ctx context.Context, base, query string, limit int) ([]Result, error) {
	u := base + "/search?q=" + url.QueryEscape(query)
	req, err := newRequest(ctx, u)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng 返回状态 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseSearxngHTML(data, limit), nil
}

// parseSearxngHTML 解析 SearXNG 结果页：
// 每个 <article class="result"> 包含 <h3><a href>标题</a></h3> 与
// <p class="content">摘要</p>；提取 href 与文本。
func parseSearxngHTML(data []byte, limit int) []Result {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil
	}

	var out []Result
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if len(out) >= limit {
			return
		}
		if n.Type == html.ElementNode && n.Data == "article" && hasClass(n, "result") {
			if r := parseResultArticle(n); r != nil {
				out = append(out, *r)
			}
			return // 不深入 article 内部继续找 article
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// parseResultArticle 从单个 article.result 节点提取 标题/链接/摘要。
func parseResultArticle(article *html.Node) *Result {
	r := &Result{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch {
		case r.URL == "" && n.Data == "a" && n.Parent != nil && n.Parent.Data == "h3":
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					r.URL = attr.Val
				}
			}
			r.Title = nodeText(n)
		case r.Snippet == "" && n.Data == "p" && hasClass(n, "content"):
			r.Snippet = nodeText(n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(article)

	if r.Title == "" && r.Snippet == "" {
		return nil
	}
	if r.URL == "" {
		return nil // 无链接的结果无意义
	}
	r.Title = strings.TrimSpace(r.Title)
	r.Snippet = strings.TrimSpace(r.Snippet)
	return r
}

// nodeText 收集节点下全部文本（折叠空白）。
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// hasClass 判断元素 class 属性是否包含目标类名。
func hasClass(n *html.Node, target string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			if slices.Contains(strings.Fields(attr.Val), target) {
				return true
			}
		}
	}
	return false
}

// --- Serper（Google 搜索 API，需 key） ---

// serperSearchURL Serper 搜索端点（测试可覆盖）。
var serperSearchURL = "https://google.serper.dev/search"

type serperEngine struct {
	client *http.Client
	apiKey string
}

func (e *serperEngine) Name() string { return "Serper" }

type serperRequest struct {
	Q   string `json:"q"`
	Gl  string `json:"gl,omitempty"`
	Hl  string `json:"hl,omitempty"`
	Num int    `json:"num,omitempty"`
}

type serperResponse struct {
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic"`
	AnswerBox *struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"answerBox"`
}

func (e *serperEngine) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if e.apiKey == "" {
		return nil, fmt.Errorf("serper 引擎需要配置 serper_api_key")
	}
	payload, err := json.Marshal(serperRequest{Q: query, Gl: "cn", Hl: "zh-cn", Num: limit})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serperSearchURL,
		strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serper 返回状态 %d", resp.StatusCode)
	}

	var parsed serperResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("serper 响应解析失败: %w", err)
	}

	out := make([]Result, 0, limit+1)
	if parsed.AnswerBox != nil {
		out = append(out, Result{Title: parsed.AnswerBox.Title, URL: parsed.AnswerBox.Link, Snippet: parsed.AnswerBox.Snippet})
	}
	for _, r := range parsed.Organic {
		out = append(out, Result{Title: strings.TrimSpace(r.Title), URL: r.Link, Snippet: strings.TrimSpace(r.Snippet)})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
