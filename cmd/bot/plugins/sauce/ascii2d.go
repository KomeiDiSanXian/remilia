package sauce

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ── ASCII2D 客户端 ────────────────────────────────────────────────────
//
// ascii2d 于 2026 年改版后采用新的检索流程：
//   - 首页 GET 获取 CSRF token
//   - POST /search/uri（URL 检索）或 /search/file（文件上传）后 302 到 /search/color/<hash>
//   - /search/color/<hash> 为色合检索结果页
//   - /search/bovw/<hash> 为特征检索结果页（对裁剪图更友好）
//
// 注意：ascii2d 处于 Cloudflare 保护之下，来自脚本/数据中心的 POST 可能被拦截，
// 该引擎按配置默认关闭。文件上传端点（/search/file）近期不可用，故使用 URL 检索。

type ascii2dClient struct {
	httpClient *http.Client
}

// newASCII2DClient 创建 ASCII2D 客户端，启用 cookie 会话以维持登录态。
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

// Search 通过 ASCII2D 搜索图片来源，返回色合检索与特征检索合并后的结果。
//
// 需要可公开访问的图片 URL（ASCII2D 服务端自行抓取）。
func (c *ascii2dClient) Search(ctx context.Context, in engineInput, maxResults int) ([]SearchResult, error) {
	if in.ImageURL == "" {
		return nil, errors.New("ASCII2D 需要可公开访问的图片 URL")
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 token 失败: %w", err)
	}

	form := url.Values{
		"authenticity_token": {token},
		"uri":                {in.ImageURL},
		"utf8":               {"✓"},
		"search":             {""},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ascii2d.net/search/uri", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建搜索请求失败: %w", err)
	}
	setBrowserHeaders(req, "https://ascii2d.net/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

	// 跟随重定向后的最终 URL（形如 https://ascii2d.net/search/color/<hash>）
	hash := ""
	if resp.Request != nil && resp.Request.URL != nil {
		hash = extractAscii2dHash(resp.Request.URL.String())
	}

	colorResults := parseASCII2DResults(body)
	if hash == "" {
		return colorResults, nil
	}

	featureResults, ferr := c.fetchFeatureResults(ctx, hash)
	if ferr != nil {
		return colorResults, nil
	}

	return mergeASCII2DModes(colorResults, featureResults, maxResults), nil
}

// fetchFeatureResults 获取特征检索（BOVW）结果页并解析。
func (c *ascii2dClient) fetchFeatureResults(ctx context.Context, hash string) ([]SearchResult, error) {
	u := "https://ascii2d.net/search/bovw/" + url.PathEscape(hash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req, "https://ascii2d.net/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ASCII2D 返回状态 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseASCII2DResults(body), nil
}

// fetchToken 获取 ASCII2D 首页 URL 检索表单的 authenticity_token。
func (c *ascii2dClient) fetchToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ascii2d.net/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

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

	// URL 检索表单（action="/search/uri"）对应的 token 为页面中第一个。
	var token string
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom != atom.Input || token != "" {
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

// extractAscii2dHash 从结果页 URL 中提取检索 hash。
// URL 形如 https://ascii2d.net/search/color/<hash> 或 /search/bovw/<hash>。
func extractAscii2dHash(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if (p == "color" || p == "bovw") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
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
