package sauce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ── 统一结果类型 ──────────────────────────────────────────────────────

type SearchResult struct {
	Source    string   // "SauceNAO" / "ASCII2D"
	Similarity string  // SauceNAO 的相似度百分比
	Title     string
	Author    string
	Thumbnail string   // 缩略图 URL
	ExtURLs   []string // 外部链接
	SourceName string  // 来源站点名（Pixiv / Twitter / Danbooru 等）
}

// ── SauceNAO 客户端 ───────────────────────────────────────────────────

type saucenaoClient struct {
	httpClient *http.Client
}

func newSauceNAOClient() *saucenaoClient {
	return &saucenaoClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// saucenaoResponse SauceNAO API 响应结构
type saucenaoResponse struct {
	Header  saucenaoResponseHeader `json:"header"`
	Results []saucenaoResultItem   `json:"results"`
}

type saucenaoResponseHeader struct {
	Status          int    `json:"status"`
	ShortRemaining  int    `json:"short_remaining"`
	LongRemaining   int    `json:"long_remaining"`
	Message         string `json:"message"`
}

type saucenaoResultItem struct {
	Header saucenaoItemHeader `json:"header"`
	Data   saucenaoItemData   `json:"data"`
}

type saucenaoItemHeader struct {
	Similarity string `json:"similarity"`
	Thumbnail  string `json:"thumbnail"`
	IndexID    int    `json:"index_id"`
	IndexName  string `json:"index_name"`
}

type saucenaoItemData struct {
	ExtURLs    []string `json:"ext_urls"`
	Title      string   `json:"title"`
	PixivID    int      `json:"pixiv_id"`
	MemberName string   `json:"member_name"`
	MemberID   int      `json:"member_id"`
	Source     string   `json:"source"`
	AuthorName string   `json:"author_name"`
	AuthorURL  string   `json:"author_url"`
	Creator    json.RawMessage `json:"creator"`
	Material   string          `json:"material"`
	Part       string   `json:"part"`
	DanbooruID int      `json:"danbooru_id"`
	GelbooruID int      `json:"gelbooru_id"`
	SankakuID  int      `json:"sankaku_id"`
	TwitterUserHandle string `json:"twitter_user_handle"`
	TwitterUserID     int    `json:"twitter_user_id"`
}

// Search 通过 SauceNAO 搜索图片来源。
// imageURL 为图片的远程 URL。
func (c *saucenaoClient) Search(ctx context.Context, apiKey, imageURL string, maxResults int) ([]SearchResult, error) {
	u := fmt.Sprintf("https://saucenao.com/search.php?api_key=%s&output_type=2&db=999&url=%s",
		url.QueryEscape(apiKey), url.QueryEscape(imageURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SauceNAO 返回状态 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var data saucenaoResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if data.Header.Status != 0 {
		if data.Header.Message != "" {
			return nil, fmt.Errorf("SauceNAO 错误: %s", data.Header.Message)
		}
		return nil, fmt.Errorf("SauceNAO 返回状态码 %d", data.Header.Status)
	}

	if len(data.Results) == 0 {
		return nil, nil
	}

	count := maxResults
	if count <= 0 || count > len(data.Results) {
		count = len(data.Results)
	}

	results := make([]SearchResult, 0, count)
	for _, item := range data.Results[:count] {
		r := SearchResult{
			Source:     "SauceNAO",
			Similarity: item.Header.Similarity,
			Thumbnail:  item.Header.Thumbnail,
			ExtURLs:    item.Data.ExtURLs,
		}

		// 确定来源站点名
		r.SourceName = lookupIndexName(item.Header.IndexID, item.Header.IndexName)
		if r.SourceName == "" {
			r.SourceName = item.Data.Source
		}

		// 标题
		r.Title = item.Data.Title
		if r.Title == "" {
			r.Title = item.Data.Material
		}
		if r.Title == "" {
			r.Title = item.Data.Part
		}

		// 作者
		r.Author = item.Data.AuthorName
		if r.Author == "" {
			r.Author = item.Data.MemberName
		}
		if r.Author == "" && len(item.Data.Creator) > 0 {
			r.Author = extractString(item.Data.Creator)
		}
		if r.Author == "" && item.Data.TwitterUserHandle != "" {
			r.Author = "@" + item.Data.TwitterUserHandle
		}

		results = append(results, r)
	}

	return results, nil
}

func lookupIndexName(id int, fallback string) string {
	switch id {
	case 5, 6, 22:
		return "Pixiv"
	case 8:
		return "Nico Nico Seiga"
	case 9:
		return "Danbooru"
	case 10:
		return "Drawr"
	case 11:
		return "Nijie"
	case 12:
		return "Yande.re"
	case 15:
		return "Shutter Stock"
	case 16:
		return "FAKKU"
	case 18:
		return "nhentai"
	case 21:
		return "AniDB"
	case 29:
		return "MangaDex"
	case 30:
		return "Manga Fox"
	case 36:
		return "Gelbooru"
	case 37:
		return "Sankaku"
	case 38:
		return "Anime-Pictures"
	case 40:
		return "IMDb"
	case 999:
		return "3D"
	}
	if id >= 20 && id <= 28 {
		return "Anime"
	}
	if id >= 31 && id <= 35 {
		return "H-Misc"
	}
	return fallback
}

// ── ASCII2D 客户端 ────────────────────────────────────────────────────

type ascii2dClient struct {
	httpClient *http.Client
}

func newASCII2DClient() *ascii2dClient {
	jar, _ := cookiejar.New(nil)
	return &ascii2dClient{
		httpClient: &http.Client{
			Timeout: 25 * time.Second,
			Jar:     jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("重定向次数过多")
				}
				return nil
			},
		},
	}
}

// Search 通过 ASCII2D 搜索图片来源。
// 流程：获取首页得到 CSRF token → POST 提交 URL → 跟随重定向 → 解析结果页面 HTML。
func (c *ascii2dClient) Search(ctx context.Context, imageURL string, maxResults int) ([]SearchResult, error) {
	token, err := c.fetchToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 token 失败: %w", err)
	}

	form := url.Values{
		"authenticity_token": {token},
		"uri":                {imageURL},
		"utf8":               {"✓"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ascii2d.net/search/uri", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建搜索请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://ascii2d.net/")
	req.Header.Set("Origin", "https://ascii2d.net")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ASCII2D 返回状态 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	results := parseASCII2DResults(body)
	if len(results) == 0 {
		return nil, nil
	}

	count := maxResults
	if count <= 0 || count > len(results) {
		count = len(results)
	}

	return results[:count], nil
}

// fetchToken 获取 ASCII2D 首页的 authenticity_token。
func (c *ascii2dClient) fetchToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ascii2d.net/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}

	var token string
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom != atom.Input {
			return
		}
		if getAttr(n, "name") == "authenticity_token" {
			token = getAttr(n, "value")
		}
	})

	if token == "" {
		return "", fmt.Errorf("未找到 authenticity_token")
	}
	return token, nil
}

// parseASCII2DResults 解析 ASCII2D 结果页面的 HTML，提取搜索结果。
func parseASCII2DResults(body []byte) []SearchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	var results []SearchResult
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom != atom.Div {
			return
		}
		class := getAttr(n, "class")
		if class != "row item-box" {
			return
		}
		if r := extractASCII2DItem(n); r != nil {
			results = append(results, *r)
		}
	})

	return results
}

// extractASCII2DItem 从单个 .row.item-box 节点中提取搜索结果。
func extractASCII2DItem(n *html.Node) *SearchResult {
	r := &SearchResult{Source: "ASCII2D"}

	// 提取缩略图
	visitNodes(n, func(cn *html.Node) {
		if cn.DataAtom == atom.Img && cn.Parent != nil && hasClass(cn.Parent, "image-box") {
			if src := getAttr(cn, "data-src"); src != "" {
				r.Thumbnail = normalizeASCII2DURL(src)
			}
		}
	})

	// 提取详情
	visitNodes(n, func(cn *html.Node) {
		if cn.DataAtom != atom.Div {
			return
		}
		class := getAttr(cn, "class")
		if !strings.Contains(class, "detail-box") {
			return
		}
		extractDetailBox(cn, r)
	})

	if len(r.ExtURLs) == 0 {
		return nil
	}
	return r
}

// extractDetailBox 从 detail-box 节点中提取链接和标题信息。
func extractDetailBox(n *html.Node, r *SearchResult) {
	visitNodes(n, func(cn *html.Node) {
		if cn.DataAtom == atom.A {
			href := getAttr(cn, "href")
			if href == "" || strings.HasPrefix(href, "/") {
				return
			}
			href = normalizeASCII2DURL(href)
			r.ExtURLs = append(r.ExtURLs, href)

			text := getTextContent(cn)
			if text != "" && r.Title == "" {
				r.Title = text
			}
			if strings.Contains(href, "twitter.com") {
				r.SourceName = "Twitter"
				if r.Author == "" {
					parts := strings.Split(strings.TrimPrefix(href, "https://twitter.com/"), "/")
					if len(parts) > 0 && parts[0] != "" {
						r.Author = "@" + parts[0]
					}
				}
			} else if strings.Contains(href, "pixiv.net") {
				r.SourceName = "Pixiv"
			} else if strings.Contains(href, "danbooru") {
				r.SourceName = "Danbooru"
			} else if strings.Contains(href, "yande.re") {
				r.SourceName = "Yande.re"
			} else if strings.Contains(href, "sankaku") {
				r.SourceName = "Sankaku"
			} else if strings.Contains(href, "gelbooru") {
				r.SourceName = "Gelbooru"
			}
		}
	})
}

// ── HTML 工具函数 ─────────────────────────────────────────────────────

// visitNodes 深度遍历 HTML 节点树，对每个节点调用 fn。
func visitNodes(n *html.Node, fn func(*html.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visitNodes(c, fn)
	}
}

// getAttr 获取节点的指定属性值。
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// hasClass 检查节点是否包含指定的 class。
func hasClass(n *html.Node, class string) bool {
	c := getAttr(n, "class")
	return strings.Contains(c, class)
}

// getTextContent 获取节点下的纯文本内容。
func getTextContent(n *html.Node) string {
	var b strings.Builder
	visitNodes(n, func(cn *html.Node) {
		if cn.Type == html.TextNode {
			b.WriteString(strings.TrimSpace(cn.Data))
		}
	})
	return strings.TrimSpace(b.String())
}

// normalizeASCII2DURL 规范化 ASCII2D 中的相对/绝对 URL。
func normalizeASCII2DURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://ascii2d.net" + raw
	}
	return raw
}

// ── 工具函数 ──────────────────────────────────────────────────────────

// extractString 从 json.RawMessage 中提取字符串，兼容字符串和数组类型。
func extractString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0]
	}
	return ""
}

// parseSimilarity 解析相似度字符串为浮点数，失败时返回 0。
func parseSimilarity(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		return 0
	}
	return v
}
