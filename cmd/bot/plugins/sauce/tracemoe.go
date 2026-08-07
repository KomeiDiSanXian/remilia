package sauce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// traceMoeClient 调用 trace.moe（https://trace.moe）识别动画截图来源。
//
// 与 SauceNAO 的 pHash 不同，trace.moe 逐帧比对动画画面，能识别出
// SauceNAO 经常搜不到的动画截图，并给出番名、话数与精确到秒的时间点。
// 免费、无需 API key。
type traceMoeClient struct {
	httpClient *http.Client
}

// newTraceMoeClient 创建 trace.moe 客户端。
func newTraceMoeClient(httpClient *http.Client) *traceMoeClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &traceMoeClient{
		httpClient: httpClient,
	}
}

// traceMoeResponse trace.moe API 响应结构。
type traceMoeResponse struct {
	Error  string         `json:"error"`  // 错误信息，为空表示成功
	Result []traceMoeItem `json:"result"` // 命中结果，按相似度降序
}

// traceMoeItem trace.moe 单个命中条目。
type traceMoeItem struct {
	Anilist    traceMoeAnilist `json:"anilist"`    // 番剧信息（anilistInfo 参数开启时返回）
	Filename   string          `json:"filename"`   // 来源视频文件名
	Episode    json.RawMessage `json:"episode"`    // 话数，可为数字/字符串/数组/null
	From       float64         `json:"from"`       // 命中场景起始时间（秒）
	To         float64         `json:"to"`         // 命中场景结束时间（秒）
	Similarity float64         `json:"similarity"` // 相似度（0-1）
	Video      string          `json:"video"`      // 命中片段预览视频 URL
	Image      string          `json:"image"`      // 命中画面预览图 URL
}

// traceMoeAnilist 命中番剧的 AniList 信息。
type traceMoeAnilist struct {
	ID      int           `json:"id"`      // AniList ID
	Title   traceMoeTitle `json:"title"`   // 各语言标题
	SiteURL string        `json:"siteUrl"` // AniList 作品页 URL
}

// traceMoeTitle 番剧标题的多语言字段。
type traceMoeTitle struct {
	Native  string `json:"native"`  // 原生语言标题（如日文）
	Romaji  string `json:"romaji"`  // 罗马音标题
	Chinese string `json:"chinese"` // 中文标题
	English string `json:"english"` // 英文标题
}

// Search 通过 trace.moe 搜索动画截图来源。
//
// 直传本地图片字节（multipart image 字段）。minSimilarity 为相似度下限（0-1），
// 低于该值的弱匹配会被过滤，避免返回无意义的垃圾结果。
func (c *traceMoeClient) Search(ctx context.Context, in engineInput, maxResults int, minSimilarity float64) ([]SearchResult, error) {
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("trace.moe 需要图片字节数据")
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("image", "image.jpg")
	if err != nil {
		return nil, err
	}
	if _, err = fw.Write(in.Data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.trace.moe/search?anilistInfo&cutBorders", body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 非 200 时响应仍为 JSON，携带可读错误信息
	var data traceMoeResponse
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if data.Error != "" {
		return nil, fmt.Errorf("trace.moe 错误: %s", data.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trace.moe 返回状态 %d", resp.StatusCode)
	}

	if minSimilarity <= 0 {
		minSimilarity = 0.75
	}

	results := make([]SearchResult, 0, len(data.Result))
	for _, item := range data.Result {
		if item.Similarity < minSimilarity {
			continue
		}
		results = append(results, toTraceMoeResult(item))
	}

	return limitResults(results, maxResults), nil
}

// toTraceMoeResult 将 trace.moe 原始条目转换为统一结果。
func toTraceMoeResult(item traceMoeItem) SearchResult {
	r := SearchResult{
		Source:     "TraceMoe",
		Similarity: formatSimilarity(item.Similarity * 100),
		Thumbnail:  item.Image,
		PreviewURL: item.Image,
		VideoURL:   item.Video,
		SourceName: "Trace.moe",
	}

	title := item.Anilist.Title
	r.Title = title.Chinese
	if r.Title == "" {
		r.Title = title.Romaji
	}
	if r.Title == "" {
		r.Title = title.English
	}
	if r.Title == "" {
		r.Title = title.Native
	}
	if r.Title == "" {
		r.Title = strings.TrimSpace(item.Filename)
	}

	r.Episode = traceMoeEpisode(item.Episode)
	r.Timestamp = formatTraceMoeTime(item.From)

	if item.Anilist.SiteURL != "" {
		r.ExtURLs = []string{item.Anilist.SiteURL}
	}

	return r
}

// traceMoeEpisode 从 episode 原始字段中提取话数文本（兼容数字/字符串/数组/null）。
func traceMoeEpisode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var num int
	if err := json.Unmarshal(raw, &num); err == nil {
		return strconv.Itoa(num)
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return traceMoeEpisode(arr[0])
	}

	return ""
}

// formatTraceMoeTime 将秒数格式化为 分:秒（超过 1 小时显示 时:分:秒）。
func formatTraceMoeTime(seconds float64) string {
	s := int(seconds)
	if s < 0 {
		s = 0
	}
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}
