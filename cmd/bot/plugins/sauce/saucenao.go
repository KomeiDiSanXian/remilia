package sauce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ── SauceNAO 客户端 ───────────────────────────────────────────────────

// saucenaoClient 调用 SauceNAO（https://saucenao.com）搜索图片来源。
//
// SauceNAO 是二次元插画/同人作品检索的主流库，覆盖面广但依赖 API key，
// 且限制严格（约每 30 秒一次、每日限额），配额耗尽时接口会返回带
// status=-1 的错误信息，调用方应将其作为错误上报而非静默忽略。
type saucenaoClient struct {
	httpClient *http.Client
}

// newSauceNAOClient 创建 SauceNAO 客户端。
func newSauceNAOClient() *saucenaoClient {
	return &saucenaoClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// saucenaoResponse SauceNAO API 响应结构。
type saucenaoResponse struct {
	Header  saucenaoResponseHeader `json:"header"`
	Results []saucenaoResultItem   `json:"results"`
}

// saucenaoResponseHeader SauceNAO 响应头。
type saucenaoResponseHeader struct {
	Status         int    `json:"status"`          // 0 表示成功，-1 表示错误（未配置 key / 限流等）
	ShortRemaining int    `json:"short_remaining"` // 30 秒窗口内剩余可用次数
	LongRemaining  int    `json:"long_remaining"`  // 24 小时窗口内剩余可用次数
	Message        string `json:"message"`         // 出错时的可读错误信息
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

// flexInt 兼容 SauceNAO 以字符串或数字返回的整型字段（如 twitter_user_id）。
// 部分索引（Kemono / Patreon 等）将 id 以字符串形式返回，直接用 int 会导致
// 整个结果集解析失败。
type flexInt int

// UnmarshalJSON 接受 JSON 数字或字符串形式的整数值，无法解析时置 0 而非报错，
// 避免单个异常字段拖垮整次搜索。
func (f *flexInt) UnmarshalJSON(b []byte) error {
	*f = 0
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if v, perr := strconv.Atoi(strings.TrimSpace(s)); perr == nil {
			*f = flexInt(v)
		}
	}
	return nil
}

type saucenaoItemData struct {
	ExtURLs           []string        `json:"ext_urls"`
	Title             string          `json:"title"`
	PixivID           flexInt         `json:"pixiv_id"`
	MemberName        string          `json:"member_name"`
	MemberID          flexInt         `json:"member_id"`
	Source            string          `json:"source"`
	AuthorName        string          `json:"author_name"`
	AuthorURL         string          `json:"author_url"`
	Creator           json.RawMessage `json:"creator"`
	Material          string          `json:"material"`
	Part              string          `json:"part"`
	DanbooruID        flexInt         `json:"danbooru_id"`
	GelbooruID        flexInt         `json:"gelbooru_id"`
	SankakuID         flexInt         `json:"sankaku_id"`
	TwitterUserHandle string          `json:"twitter_user_handle"`
	TwitterUserID     flexInt         `json:"twitter_user_id"`
}

// Search 通过 SauceNAO 搜索图片来源。
//
// 优先将本地图片字节以 multipart 直传（不依赖 SauceNAO 侧抓取远程 URL），
// 无字节数据时回退为 URL 检索。db 为要搜索的 SauceNAO 数据库（999 = 全部）。
func (c *saucenaoClient) Search(ctx context.Context, apiKey string, db int, in engineInput, maxResults int) ([]SearchResult, error) {
	if db <= 0 {
		db = 999
	}

	endpoint := fmt.Sprintf("https://saucenao.com/search.php?api_key=%s&output_type=2&db=%d",
		url.QueryEscape(apiKey), db)

	var req *http.Request
	var err error
	if len(in.Data) > 0 {
		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)
		fw, werr := w.CreateFormFile("file", "image")
		if werr != nil {
			return nil, fmt.Errorf("构建上传请求失败: %w", werr)
		}
		if _, werr = fw.Write(in.Data); werr != nil {
			return nil, fmt.Errorf("写入上传内容失败: %w", werr)
		}
		if werr = w.Close(); werr != nil {
			return nil, fmt.Errorf("结束上传内容失败: %w", werr)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
	} else {
		u := endpoint + "&url=" + url.QueryEscape(in.ImageURL)
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// 传输错误携带的 URL 含 api_key 查询参数，必须脱敏后上报
		return nil, fmt.Errorf("请求失败: %w", redactTransportError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 优先解析 JSON 响应体（SauceNAO 无 key/限流时也会返回带状态码的 JSON）
	var data saucenaoResponse
	if jsonErr := json.Unmarshal(body, &data); jsonErr == nil && data.Header.Status != 0 {
		if data.Header.Message != "" {
			return nil, fmt.Errorf("%s", data.Header.Message)
		}
		return nil, fmt.Errorf("返回状态码 %d", data.Header.Status)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SauceNAO 返回状态 %d", resp.StatusCode)
	}
	if jsonErr := json.Unmarshal(body, &data); jsonErr != nil {
		return nil, fmt.Errorf("解析响应失败: %w", jsonErr)
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

// lookupIndexName 将 SauceNAO 的 index_id 映射为可读站点名，映射失败时返回 fallback。
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
