package anime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"time"
)

// WeekdayEntry Bangumi 日历中的一天（包含该日放送的番剧列表）。
type WeekdayEntry struct {
	Weekday  WeekdayInfo    `json:"weekday"`
	Subjects []AnimeSubject `json:"items"`
}

// WeekdayInfo 星期信息。
type WeekdayInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AnimeSubject Bangumi 番剧条目信息。
type AnimeSubject struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	NameCN  string            `json:"name_cn"`
	Summary string            `json:"summary"`
	Eps     int               `json:"total_episodes"`
	AirDate string            `json:"air_date"`
	Rating  RatingInfo        `json:"rating"`
	Rank    int               `json:"rank"`
	Images  map[string]string `json:"images"`
}

// ImageURL 返回番剧封面 URL，按 medium > large > common > grid 优先级选择。
func (s *AnimeSubject) ImageURL() string {
	if s.Images == nil {
		return ""
	}
	for _, key := range []string{"medium", "large", "common", "grid"} {
		if u, ok := s.Images[key]; ok && u != "" {
			return u
		}
	}
	return ""
}

// RatingInfo 评分信息。
type RatingInfo struct {
	Score float64 `json:"score"`
	Total int     `json:"total"`
}

// SearchResult Bangumi 搜索结果。
type SearchResult struct {
	Data  []AnimeSubject `json:"data"`
	Total int            `json:"total"`
}

// bangumiClient Bangumi API 客户端。
// 包含 700ms 间隔的限流器，确保不超过官方限制（100 次/分钟）。
type bangumiClient struct {
	client    *http.Client
	rateLimit *time.Ticker
	apiBase   string // API 基础 URL，可通过插件配置更换为镜像/代理
}

// newBangumiClient 创建 Bangumi API 客户端。
// 使用自定义 DNS 解析器（Cloudflare 1.1.1.1）绕过 DNS 污染，
// 支持 HTTP_PROXY/HTTPS_PROXY 环境变量配置代理。
// apiBase 为 API 基础 URL，默认 https://api.bgm.tv，可改为 http://api.bgm.tv 或代理地址。
func newBangumiClient(apiBase string) *bangumiClient {
	if apiBase == "" {
		apiBase = "https://api.bgm.tv"
	}
	return &bangumiClient{
		client:    newBypassHTTPClient(15 * time.Second),
		rateLimit: time.NewTicker(700 * time.Millisecond),
		apiBase:   apiBase,
	}
}

// fetchImageA 从 URL 下载并解码图片（支持 JPEG/PNG）。
func fetchImageA(rawURL string) (image.Image, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	client := &http.Client{
		Transport: bypassTransport,
		Timeout:   15 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}

// newBypassHTTPClient 创建一个可绕过 DNS 污染的 HTTP 客户端。
// 使用 Cloudflare DNS (1.1.1.1) 进行域名解析，避免 ISP 级别的 DNS 劫持。
// 支持标准 HTTP_PROXY / HTTPS_PROXY 环境变量配置代理。
func newBypassHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				Resolver: &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						d := net.Dialer{Timeout: 5 * time.Second}
						return d.DialContext(ctx, "udp", "1.1.1.1:53")
					},
				},
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// newBypassTransport 为 fetchImageA 等临时请求创建可复用的传输层。
var bypassTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", "1.1.1.1:53")
			},
		},
	}).DialContext,
	TLSHandshakeTimeout: 10 * time.Second,
}

// doRequest 发起带限流的 HTTP 请求，自动处理 JSON 反序列化。
// 请求前会等待限流器，确保 QPS 在安全范围内。
// 使用自定义 DNS + 代理支持，可通过 HTTP_PROXY / HTTPS_PROXY 环境变量配置代理。
func (c *bangumiClient) doRequest(ctx context.Context, method, url string, body io.Reader, result any) error {
	select {
	case <-c.rateLimit.C:
	case <-ctx.Done():
		return ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bangumi api error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("bangumi json error: %w body=%s", err, string(respBody))
	}
	return nil
}

// FetchCalendar 获取当季番剧每日放送列表。
func (c *bangumiClient) FetchCalendar(ctx context.Context) ([]WeekdayEntry, error) {
	var result []WeekdayEntry
	if err := c.doRequest(ctx, http.MethodGet, c.apiBase+"/calendar", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// FetchSubject 获取番剧条目的详细信息（评分、集数、简介等）。
func (c *bangumiClient) FetchSubject(ctx context.Context, id int64) (*AnimeSubject, error) {
	url := fmt.Sprintf("%s/v0/subjects/%d", c.apiBase, id)
	var result AnimeSubject
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchSubjects 按关键词搜索番剧（仅搜索动画类型，type=2）。
// limit 控制返回数量，最大 20。
func (c *bangumiClient) SearchSubjects(ctx context.Context, keyword string, limit int) ([]AnimeSubject, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	body := map[string]any{
		"keyword": keyword,
		"type":    2,
		"limit":   limit,
	}
	bodyBytes, _ := json.Marshal(body)

	url := c.apiBase + "/v0/search/subjects"
	var result SearchResult
	if err := c.doRequest(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes), &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}
