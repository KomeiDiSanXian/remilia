// Package pic booru.go — booru 图库 API 客户端与响应解析。
//
// 支持两种协议：
//   - Gelbooru 系（safebooru / gelbooru / rule34）：index.php?page=dapi&s=post&q=index&json=1
//     注意各子站根属性键名不一致（"attributes" 与 "@attributes"），解析时需兼容。
//   - Moebooru（konachan / yande.re）：/post.json
//
// 两种协议均支持通过 rating:xxx 标签过滤内容分级（部分站点已迁移分级体系，
// 见 site.ratingSearchTag），随机取图分别使用 sort:random（Gelbooru 系）
// 与 order:random（Moebooru）。
package pic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// picUserAgent 模拟浏览器 UA，降低被目标站点拦截的概率。
const picUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// maxPicBytes 单张图片下载体积上限（字节）。
const maxPicBytes = 20 * 1024 * 1024

// booruCredentials 各站点的 API 认证凭据。
//
// 需要认证的站点：
//   - gelbooru.com：user_id + api_key（免费注册获取，否则返回 401）
//   - rule34.xxx：user_id + api_key（账号设置页获取，否则返回 Missing authentication）
//   - safebooru / konachan.net / yande.re：无需认证
type booruCredentials struct {
	GelbooruUserID string
	GelbooruAPIKey string
	Rule34UserID   string
	Rule34APIKey   string
}

// booruClient 聚合两类 booru 协议的 HTTP 客户端。
type booruClient struct {
	httpClient *http.Client
	creds      booruCredentials
}

// newBooruClient 创建 booru 客户端，带认证凭据。
//
// proxyURL 为可选代理地址（如 "http://127.0.0.1:7890"），仅本插件的
// 请求走该代理（SauceNAO 等被墙站点的图库访问需要）；为空时沿用
// 环境变量代理（HTTPS_PROXY/HTTP_PROXY）或直连。
func newBooruClient(creds booruCredentials, proxyURL string) (*booruClient, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, perr := url.Parse(proxyURL)
		if perr != nil {
			return nil, fmt.Errorf("无效的代理地址 %q: %w", proxyURL, perr)
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	return &booruClient{
		// 30s：gelbooru.com 的 dapi 查询全库较慢，15s 会频繁超时
		httpClient: &http.Client{Timeout: 30 * time.Second, Transport: tr},
		creds:      creds,
	}, nil
}

// picPost 平台无关的图片作品模型，由两种协议解析结果统一而来。
type picPost struct {
	ID       int
	Rating   Rating
	Tags     []string
	FileURL  string
	Source   string // 原始来源链接（画师主页等）
	Author   string // 画师名（Gelbooru 系可能为空，可从 artist: 标签提取）
	Score    int
	SiteName string // 来源站点展示名
	Change   int64  // 上传/更新时间（unix 秒），用于 recency 过滤；站点未提供时为 0
}

// fetchRandom 从指定站点获取随机图片。
//
// 服务端随机方式（依据各站官方文档实测，2026-08）：
//   - Gelbooru 系：tags 中附加 sort:random meta-tag（gelbooru cheatsheet 文档）
//   - Moebooru：tags 中附加 order:random meta-tag（Danbooru 兼容语法）
//
// 查询参数 random=1 / order=random 均无效（被忽略），不要使用。
//
// 双保险：请求 randomPoolSize(count) 条服务端随机结果，再本地随机选取 count 张。
//
// recentDays 为"近 N 天内上传"过滤（0 = 不过滤）：
//   - Moebooru（konachan / yande.re）：服务端 date:YYYY-MM-DD.. 过滤（实测可靠）
//   - Gelbooru 系（safebooru / gelbooru / rule34）：不支持 date meta-tag，
//     改用客户端按 change 字段过滤；随机池放大并累积补充直到凑够 count。
func (c *booruClient) fetchRandom(ctx context.Context, s site, tags []string, rng RatingRange, count, recentDays int) ([]picPost, error) {
	if count <= 0 {
		count = 1
	}
	pool := randomPoolSize(count)
	if recentDays > 0 && s.Protocol == protocolGelbooru {
		// 客户端过滤：放大随机池，保证过滤后仍有足够候选
		pool = pool * recencyPoolMultiplier
	}
	var (
		posts []picPost
		err   error
	)
	if s.Protocol == protocolMoebooru {
		posts, err = c.fetchMoebooru(ctx, s, tags, rng, pool, recentDays)
		if err != nil {
			return nil, err
		}
		return pickRandomPosts(posts, count), nil
	}

	// Gelbooru 系：客户端过滤 + 累积补充。
	// 每次拉取放大后的随机池，过滤后并入结果；不足 count 时继续拉取，
	// 最多 maxRecencyAttempts 次（避免冷门标签/低新图占比时只返回 1 张）。
	var collected []picPost
	for attempt := 0; attempt < maxRecencyAttempts && len(collected) < count; attempt++ {
		posts, err = c.fetchGelbooru(ctx, s, tags, rng, pool)
		if err != nil {
			return nil, err
		}
		if recentDays > 0 && hasChangeField(posts) {
			// 站点提供时间字段：按近 N 天过滤
			posts = filterRecent(posts, recentDays)
		}
		// 站点不提供时间字段（整批 Change=0）：跳过过滤直接使用，
		// 避免"年份过滤"在该站点上永久返回空结果。
		collected = append(collected, posts...)
	}
	return pickRandomPosts(collected, count), nil
}

// maxRecencyAttempts 客户端年份过滤的最大拉取次数（凑够 count 用）。
const maxRecencyAttempts = 3

// recencyPoolMultiplier 客户端年份过滤时随机池的放大倍数。
// 放大后池内近 N 天新图期望数 = pool × 新图占比；放大 3 倍保证
// 冷门标签（老图占比高）下仍有足够候选。
const recencyPoolMultiplier = 3

// hasChangeField 报告结果中是否存在携带时间字段的帖子。
// 整批 Change=0 说明站点未提供时间字段，客户端年份过滤应跳过。
func hasChangeField(posts []picPost) bool {
	for _, p := range posts {
		if p.Change != 0 {
			return true
		}
	}
	return false
}

// filterRecent 按"近 N 天内上传"过滤结果（recentDays <= 0 时原样返回）。
func filterRecent(posts []picPost, recentDays int) []picPost {
	if recentDays <= 0 {
		return posts
	}
	cutoff := time.Now().AddDate(0, 0, -recentDays).Unix()
	out := make([]picPost, 0, len(posts))
	for _, p := range posts {
		if p.Change >= cutoff {
			out = append(out, p)
		}
	}
	return out
}

// randomPoolSize 本地随机池大小：至少 10 条，随请求张数放大。
func randomPoolSize(count int) int {
	if n := count * 3; n > 10 {
		return n
	}
	return 10
}

// randomTag 返回协议对应的服务端随机排序 meta-tag。
func randomTag(proto protocol) string {
	if proto == protocolMoebooru {
		return "order:random"
	}
	return "sort:random"
}

// recencyTag 返回协议对应的"近 N 天内上传"过滤 meta-tag。
//
// 仅 Moebooru（konachan / yande.re）支持 date:YYYY-MM-DD.. 服务端过滤
// （实测可靠）；Gelbooru 系不支持 date meta-tag，返回空串由调用方
// 客户端过滤。recentDays <= 0 时返回空串。
func recencyTag(proto protocol, recentDays int) string {
	if recentDays <= 0 || proto != protocolMoebooru {
		return ""
	}
	cutoff := time.Now().AddDate(0, 0, -recentDays)
	return "date:" + cutoff.Format("2006-01-02") + ".."
}

// searchTags 拼接查询标签：用户标签 + 区间对应的 rating 过滤 +
// 近 N 天过滤（Moebooru 服务端）+ 随机排序 meta-tag。
//
// rating 过滤按站点生成（见 site.rangeTags）：gelbooru.com 已迁移至
// Danbooru 式分级，safe 需使用 rating:general 而非已失效的 rating:safe。
func searchTags(s site, userTags []string, rng RatingRange, recentDays int) string {
	base := buildTags(s, userTags, rng)
	if t := recencyTag(s.Protocol, recentDays); t != "" {
		if base == "" {
			base = t
		} else {
			base += " " + t
		}
	}
	if base == "" {
		return randomTag(s.Protocol)
	}
	return base + " " + randomTag(s.Protocol)
}

// pickRandomPosts 从结果池中随机选取 count 张（不重复）。
// count 大于池大小时返回全部。
func pickRandomPosts(posts []picPost, count int) []picPost {
	if len(posts) == 0 || count <= 0 {
		return nil
	}
	if count >= len(posts) {
		return posts
	}
	idx := rand.Perm(len(posts))[:count]
	out := make([]picPost, 0, count)
	for _, i := range idx {
		out = append(out, posts[i])
	}
	return out
}

// buildTags 拼接查询标签：用户标签 + 区间对应的 rating 过滤标签。
func buildTags(s site, userTags []string, rng RatingRange) string {
	parts := append([]string(nil), userTags...)
	for _, tag := range s.rangeTags(rng) {
		parts = append(parts, tag)
	}
	return strings.Join(parts, " ")
} // ── Gelbooru 系协议 ─────────────────────────────────────────────────────

// gelbooruResponse Gelbooru 系对象包装格式响应（gelbooru.com）。
//
// 响应结构（实测，2026-08）：
//   - gelbooru.com：{"attributes": {...}, "post": [...]}
//   - safebooru.org / api.rule34.xxx：扁平 JSON 数组（见 gelbooruFlatPost）
type gelbooruResponse struct {
	Posts []gelbooruPost `json:"post"`
}

// gelbooruPost Gelbooru 系单条结果。
//
// 字段类型依据各站 API 实测（2026-08）：
//   - id / score / creator_id：JSON 数字
//   - rating / tags / file_url / source / owner：JSON 字符串
//   - creator_id：仅 gelbooru.com 返回，扁平数组格式（safebooru/rule34）下缺省为 0
type gelbooruPost struct {
	ID        int    `json:"id"`
	Rating    string `json:"rating"`
	Tags      string `json:"tags"`
	FileURL   string `json:"file_url"`
	Source    string `json:"source"`
	Score     int    `json:"score"`
	Owner     string `json:"owner"`
	CreatorID int    `json:"creator_id"`
	Change    int64  `json:"change"` // 上传/更新时间（unix 秒）
}

// fetchGelbooru 请求 Gelbooru 系站点。
//
// 注意各子站响应格式与认证要求不一致：
//   - gelbooru.com：{"attributes": {...}, "post": [...]}，需要 user_id + api_key
//   - api.rule34.xxx：扁平 JSON 数组 [...]，需要 user_id + api_key
//   - safebooru.org：扁平 JSON 数组 [...]，无需认证
//
// 认证参数仅在对应站点凭据配置非空时附加。
// Gelbooru 系不支持 date meta-tag，recency 由调用方按 change 字段客户端过滤。
func (c *booruClient) fetchGelbooru(ctx context.Context, s site, tags []string, rng RatingRange, count int) ([]picPost, error) {
	if count <= 0 {
		count = 1
	}
	endpoint := fmt.Sprintf("https://%s/index.php?page=dapi&s=post&q=index&json=1&limit=%d&tags=%s",
		s.Domain, count, url.QueryEscape(searchTags(s, tags, rng, 0))) + cacheBust()

	userID, apiKey := "", ""
	if s.Name == "rule34" {
		userID, apiKey = c.creds.Rule34UserID, c.creds.Rule34APIKey
	} else {
		userID, apiKey = c.creds.GelbooruUserID, c.creds.GelbooruAPIKey
	}
	if userID != "" {
		endpoint += "&user_id=" + url.QueryEscape(userID)
	}
	if apiKey != "" {
		endpoint += "&api_key=" + url.QueryEscape(apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", picUserAgent)

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}

	posts, err := parseGelbooruPosts(body)
	if err != nil {
		return nil, err
	}

	out := make([]picPost, 0, len(posts))
	for _, raw := range posts {
		post := gelbooruToPost(raw, s)
		if post.FileURL == "" {
			continue
		}
		out = append(out, post)
	}
	return out, nil
}

// parseGelbooruPosts 解析 Gelbooru 系响应体，兼容对象包装与扁平数组两种格式。
func parseGelbooruPosts(body []byte) ([]gelbooruPost, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		// safebooru / api.rule34.xxx 直接返回扁平数组
		var posts []gelbooruPost
		if err := json.Unmarshal(body, &posts); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return posts, nil
	}

	var resp gelbooruResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return resp.Posts, nil
}

// gelbooruToPost 将 Gelbooru 系原始结果转换为统一模型。
func gelbooruToPost(raw gelbooruPost, s site) picPost {
	post := picPost{
		ID:       raw.ID,
		Rating:   parseRating(raw.Rating),
		Tags:     strings.Fields(raw.Tags),
		FileURL:  raw.FileURL,
		Source:   raw.Source,
		Score:    raw.Score,
		Author:   raw.Owner,
		SiteName: s.DisplayName,
		Change:   raw.Change,
	}

	// owner 为空时尝试从 artist: 标签提取（少量帖子的兜底）
	if post.Author == "" {
		for _, t := range post.Tags {
			if v, ok := strings.CutPrefix(t, "artist:"); ok {
				post.Author = v
				break
			}
		}
	}
	return post
}

// ── Moebooru 协议 ────────────────────────────────────────────────────────

// moebooruRawPost Moebooru 单条结果（响应为 JSON 数组）。
type moebooruRawPost struct {
	ID        int    `json:"id"`
	Rating    string `json:"rating"`
	Tags      string `json:"tags"`
	FileURL   string `json:"file_url"`
	Source    string `json:"source"`
	Author    string `json:"author"`
	Score     int    `json:"score"`
	CreatedAt int64  `json:"created_at"` // 上传时间（unix 秒）
}

// fetchMoebooru 请求 Moebooru 站点（konachan / yande.re）。
//
// recentDays > 0 时附加 date:YYYY-MM-DD.. 服务端过滤（实测可靠）。
func (c *booruClient) fetchMoebooru(ctx context.Context, s site, tags []string, rng RatingRange, count, recentDays int) ([]picPost, error) {
	if count <= 0 {
		count = 1
	}
	endpoint := fmt.Sprintf("https://%s/post.json?limit=%d&tags=%s",
		s.Domain, count, url.QueryEscape(searchTags(s, tags, rng, recentDays))) + cacheBust()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", picUserAgent)

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}

	var raws []moebooruRawPost
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	posts := make([]picPost, 0, len(raws))
	for _, raw := range raws {
		if raw.FileURL == "" {
			continue
		}
		posts = append(posts, picPost{
			ID:       raw.ID,
			Rating:   parseMoebooruRating(raw.Rating),
			Tags:     strings.Fields(raw.Tags),
			FileURL:  raw.FileURL,
			Source:   raw.Source,
			Author:   raw.Author,
			Score:    raw.Score,
			SiteName: s.DisplayName,
			Change:   raw.CreatedAt,
		})
	}
	return posts, nil
}

// parseMoebooruRating 将 Moebooru 的单字母 rating（s/q/e）转为全称。
func parseMoebooruRating(s string) Rating {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "s":
		return RatingSafe
	case "q":
		return RatingQuestionable
	case "e":
		return RatingExplicit
	}
	return parseRating(s)
}

// ── 公共 ────────────────────────────────────────────────────────────────

// cacheBust 生成防缓存的请求参数片段。
//
// sort:random / order:random 依赖服务器端实时随机，但代理/CDN/连接
// 复用可能缓存相同 URL 的响应，导致连续请求返回同一批结果。
// 每次请求附加唯一参数并声明 no-cache，确保随机真正生效。
func cacheBust() string {
	return fmt.Sprintf("&t=%d", time.Now().UnixNano())
}

// do 执行请求并读取响应体（限制体积防止恶意响应）。
func (c *booruClient) do(req *http.Request) ([]byte, error) {
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// 传输错误是 *url.Error，其 Error() 携带完整请求 URL；
		// URL 查询参数中含 api_key / user_id 等凭据，必须脱敏后上报。
		return nil, fmt.Errorf("请求失败: %w", redactTransportError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("站点返回 401：gelbooru.com 需要 API key（在 plugins.pic.gelbooru_api_key 中配置，免费注册获取）")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("站点返回状态 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 反爬页（Cloudflare 挑战 / CAPTCHA）以 200 返回 HTML，提前给出可读错误
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, fmt.Errorf("站点返回非 JSON 响应（可能被反爬拦截或需要 API key）")
	}
	return body, nil
}

// downloadImage 下载图片字节，maxBytes 为体积上限。
//
// referer 为图片来源站点主页（如 "https://gelbooru.com/"）。
// Gelbooru 系 CDN 有热链保护：无 Referer 时重定向到 hotlink.php 返回错误页。
// 使用客户端自身的 Transport（含代理配置）。
func (c *booruClient) downloadImage(ctx context.Context, rawURL, referer string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", picUserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("图片过大 (%d bytes)", resp.ContentLength)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

// redactTransportError 抹掉传输错误 URL 中的认证查询参数。
//
// net/http 的传输错误是 *url.Error，其 Error() 会带上完整请求 URL，
// 而 URL 查询参数里可能携带 api_key / user_id 等凭据（gelbooru/rule34），
// 一次超时/DNS 抖动就会把凭据写进日志或回复给用户。
// 保留 errors.Is/As 判定能力（重新构造 *url.Error）。
func redactTransportError(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}
	parsed, perr := url.Parse(uerr.URL)
	if perr != nil {
		return err
	}
	q := parsed.Query()
	changed := false
	for _, key := range []string{"api_key", "user_id"} {
		if q.Has(key) {
			q.Set(key, "<redacted>")
			changed = true
		}
	}
	if !changed {
		return err
	}
	parsed.RawQuery = q.Encode()
	return &url.Error{Op: uerr.Op, URL: parsed.String(), Err: uerr.Err}
}
