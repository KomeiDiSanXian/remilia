package bilibili

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const biliUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// UserInfo B 站用户空间信息。
type UserInfo struct {
	Mid    int64  `json:"mid"`
	Name   string `json:"name"`
	Sign   string `json:"sign"`
	Avatar string `json:"face"`
	Level  int    `json:"-"` // 通过 LevelInfo 解析
}

// LevelInfo B 站用户等级信息（嵌套在 data.level_info 中）。
type LevelInfo struct {
	CurrentLevel int `json:"current_level"`
}

// userInfoResponse 用于解析 API 响应中的 level_info 嵌套字段。
type userInfoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Mid       int64     `json:"mid"`
		Name      string    `json:"name"`
		Sign      string    `json:"sign"`
		Face      string    `json:"face"`
		LevelInfo LevelInfo `json:"level_info"`
	} `json:"data"`
}

// RelationStat B 站用户关系统计（关注、粉丝数）。
type RelationStat struct {
	Following int64 `json:"following"`
	Follower  int64 `json:"follower"`
}

// relationStatResponse 用于解析关系统计 API 响应。
type relationStatResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    RelationStat `json:"data"`
}

// LiveInfo B 站直播间信息。
type LiveInfo struct {
	RoomID     int64  `json:"roomid"`
	Title      string `json:"title"`
	Cover      string `json:"cover"`
	WatcherNum int64  `json:"watcher_num"`
	LiveStatus int    `json:"live_status"`
	UID        int64  `json:"uid"`
	UserName   string `json:"uname"`
	IsLiving   bool
}

// SearchUserResult B 站用户搜索结果。
type SearchUserResult struct {
	Mid    int64  `json:"mid"`
	Name   string `json:"uname"`
	Sign   string `json:"usign"`
	Avatar string `json:"upic"`
	Level  int    `json:"level"`
	Fans   int64  `json:"fans"`
	Videos int64  `json:"videos"`
	IsLive int    `json:"is_live"`
	RoomID int64  `json:"room_id"`
}

// AvatarURL 返回完整的头像 URL，自动补全 https: 协议前缀。
func (s *SearchUserResult) AvatarURL() string {
	if s.Avatar == "" {
		return ""
	}
	if len(s.Avatar) >= 2 && s.Avatar[:2] == "//" {
		return "https:" + s.Avatar
	}
	return s.Avatar
}

// VideoItem B 站视频列表项。
type VideoItem struct {
	BVID        string `json:"bvid"`
	Title       string `json:"title"`
	Play        int64  `json:"play"`
	VideoReview int64  `json:"video_review"`
	Duration    string `json:"duration"`
	Created     int64  `json:"created"`
	Pic         string `json:"pic"`
	Author      string `json:"author"`
	MID         int64  `json:"mid"`
}

// biliHeaderTransport 自动为每个请求添加标准 HTTP 头部和 Cookie。
// Cookie 在启动时生成一次，之后复用同一套浏览器指纹。
type biliHeaderTransport struct {
	inner  http.RoundTripper
	cookie string
}

func newBiliTransport() *biliHeaderTransport {
	return &biliHeaderTransport{
		inner:  http.DefaultTransport,
		cookie: generateBiliCookies(),
	}
}

// refreshCookie 生成新 Cookie，在检测到被风控时调用。
func (t *biliHeaderTransport) refreshCookie() {
	t.cookie = generateBiliCookies()
}

func (t *biliHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.inner.RoundTrip(req)
}

// generateBiliCookies 生成随机的 B 站浏览器指纹 Cookie。
func generateBiliCookies() string {
	buvid3 := randomHex(32) + "infoc"
	buvid4 := randomUUID()
	lsid := randomHex(8) + "_" + randomHex(8)
	return fmt.Sprintf("buvid3=%s; buvid4=%s; b_lsid=%s; _uuid=%s",
		buvid3, buvid4, lsid, randomUUID())
}

func randomHex(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(16))
		b[i] = hex[idx.Int64()]
	}
	return string(b)
}

func randomUUID() string {
	uuid := make([]byte, 16)
	rand.Read(uuid)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
}

// biliClient B 站 API 客户端，封装所有对 B 站接口的调用。
type biliClient struct {
	signer    *wbiSigner
	transport *biliHeaderTransport
	client    *http.Client
	rateLimit *time.Ticker // 1 秒间隔，避免触发 -799 频率限制
}

// newBiliClient 创建 B 站 API 客户端，自动生成随机浏览器指纹 Cookie 绕过反爬。
func newBiliClient() *biliClient {
	transport := newBiliTransport()
	return &biliClient{
		signer:    newWBISigner(),
		transport: transport,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		rateLimit: time.NewTicker(1 * time.Second),
	}
}

// newBiliRequest 创建带有完整浏览器头部和 Cookie 的 HTTP 请求。
// Cookie 必须在请求对象上直接设置（而非在 transport 中），否则 B 站反爬会拒绝。
func (c *biliClient) newBiliRequest(ctx context.Context, method, url, referer string) (*http.Request, error) {
	if err := c.waitRateLimit(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("Cookie", c.transport.cookie)
	return req, nil
}

// biliResponse 通用的 B 站 API 响应前缀（用于检测 code）。
type biliResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// waitRateLimit 等待限流器，确保请求间隔不低于 1 秒。
func (c *biliClient) waitRateLimit(ctx context.Context) error {
	select {
	case <-c.rateLimit.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// readBody 读取响应体并检查是否为 HTML（反爬页面）。
// B 站反爬系统会返回 HTML 页面代替 JSON，此时返回明确的错误信息。
func readBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && body[0] == '<' {
		snippet := string(body[:min(len(body), 120)])
		return nil, fmt.Errorf("B 站返回了 HTML 页面，可能触发了反爬机制: %s", snippet)
	}
	return body, nil
}

// checkBanned 检查响应是否被风控拦截（code=-412 / -799），返回对应的 sentinel 错误。
func checkBanned(body []byte) error {
	var br biliResponse
	if err := json.Unmarshal(body, &br); err != nil {
		return nil
	}
	if br.Code == -412 {
		return fmt.Errorf("banned: %s (code=%d)", br.Message, br.Code)
	}
	if br.Code == -799 {
		return fmt.Errorf("rate limited: %s (code=%d)", br.Message, br.Code)
	}
	return nil
}

// FetchUserInfo 获取 UP 主空间信息及关系统计。
//
// 优先使用带 WBI 签名的接口，失败时回退到无签名接口。
func (c *biliClient) FetchUserInfo(ctx context.Context, mid int64) (*UserInfo, *RelationStat, error) {
	params := url.Values{}
	params.Set("mid", strconv.FormatInt(mid, 10))
	signed, err := c.signer.sign(ctx, params)
	if err != nil {
		return c.fetchUserInfoFallback(ctx, mid)
	}

	ref := "https://space.bilibili.com/" + strconv.FormatInt(mid, 10)
	req, err := c.newBiliRequest(ctx, http.MethodGet, "https://api.bilibili.com/x/space/wbi/acc/info?"+signed.Encode(), ref)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, nil, fmt.Errorf("FetchUserInfo: %w", err)
	}

	var result userInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, fmt.Errorf("FetchUserInfo json: %w", err)
	}
	if result.Code != 0 {
		return nil, nil, fmt.Errorf("bilibili api error: %s (code=%d)", result.Message, result.Code)
	}

	user := &UserInfo{
		Mid:    result.Data.Mid,
		Name:   result.Data.Name,
		Sign:   result.Data.Sign,
		Avatar: result.Data.Face,
		Level:  result.Data.LevelInfo.CurrentLevel,
	}

	rel, _ := c.FetchRelationStat(ctx, mid)
	return user, rel, nil
}

func (c *biliClient) fetchUserInfoFallback(ctx context.Context, mid int64) (*UserInfo, *RelationStat, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/space/acc/info?mid=%d", mid)
	ref := "https://space.bilibili.com/" + strconv.FormatInt(mid, 10)
	req, err := c.newBiliRequest(ctx, http.MethodGet, u, ref)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, nil, fmt.Errorf("fetchUserInfoFallback: %w", err)
	}

	var result userInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, fmt.Errorf("fetchUserInfoFallback json: %w", err)
	}
	if result.Code != 0 {
		return nil, nil, fmt.Errorf("bilibili api error: %s (code=%d)", result.Message, result.Code)
	}

	user := &UserInfo{
		Mid:    result.Data.Mid,
		Name:   result.Data.Name,
		Sign:   result.Data.Sign,
		Avatar: result.Data.Face,
		Level:  result.Data.LevelInfo.CurrentLevel,
	}

	rel, _ := c.FetchRelationStat(ctx, mid)
	return user, rel, nil
}

// FetchRelationStat 获取 UP 主的关注和粉丝数量。
func (c *biliClient) FetchRelationStat(ctx context.Context, mid int64) (*RelationStat, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/relation/stat?vmid=%d", mid)
	req, err := c.newBiliRequest(ctx, http.MethodGet, u, "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("FetchRelationStat: %w", err)
	}

	var result relationStatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("FetchRelationStat json: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili relation api error: %s (code=%d)", result.Message, result.Code)
	}
	return &result.Data, nil
}

// FetchLiveInfo 获取 UP 主的直播状态和直播间信息。
func (c *biliClient) FetchLiveInfo(ctx context.Context, mid int64) (*LiveInfo, error) {
	u := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/getRoomInfoOld?mid=%d", mid)
	req, err := c.newBiliRequest(ctx, http.MethodGet, u, "https://live.bilibili.com/")
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("FetchLiveInfo: %w", err)
	}

	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    LiveInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("FetchLiveInfo json: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili live api error: %s (code=%d)", result.Message, result.Code)
	}
	result.Data.IsLiving = result.Data.LiveStatus == 1
	return &result.Data, nil
}

// retrySearch 在被风控时刷新 Cookie 并重试搜索。
func (c *biliClient) retrySearch(ctx context.Context, keyword string, page int) ([]SearchUserResult, int, error) {
	time.Sleep(2 * time.Second)
	c.transport.refreshCookie()
	return c.SearchUser(ctx, keyword, page)
}

// SearchUser 按关键词搜索 B 站用户，返回匹配的用户列表和总结果数。
// 该接口无需 WBI 签名。
func (c *biliClient) SearchUser(ctx context.Context, keyword string, page int) ([]SearchUserResult, int, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/web-interface/search/type?search_type=bili_user&keyword=%s&page=%d", url.QueryEscape(keyword), page)
	req, err := c.newBiliRequest(ctx, http.MethodGet, u, "https://search.bilibili.com")
	if err != nil {
		return nil, 0, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		if err2 := checkBanned([]byte(err.Error())); err2 != nil {
			return c.retrySearch(ctx, keyword, page)
		}
		return nil, 0, fmt.Errorf("SearchUser: %w", err)
	}

	if bannedErr := checkBanned(body); bannedErr != nil {
		return c.retrySearch(ctx, keyword, page)
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			NumResults int                `json:"numResults"`
			Result     []SearchUserResult `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("SearchUser json: %w", err)
	}
	if result.Code != 0 {
		return nil, 0, fmt.Errorf("bilibili search api error: %s (code=%d)", result.Message, result.Code)
	}
	return result.Data.Result, result.Data.NumResults, nil
}

// ResolveUID 将用户输入解析为 B 站 UID。
//
//   - 输入为纯数字时直接作为 UID 返回
//   - 输入含非数字字符时通过 SearchUser 搜索，取第一个匹配结果的 UID
//
// 返回 (uid, 搜索到的用户名, error)。用户名仅在搜索命中时非空。
func (c *biliClient) ResolveUID(ctx context.Context, input string) (int64, string, error) {
	if mid, err := strconv.ParseInt(input, 10, 64); err == nil {
		return mid, "", nil
	}

	results, _, err := c.SearchUser(ctx, input, 1)
	if err != nil {
		return 0, "", fmt.Errorf("搜索用户失败: %w", err)
	}
	if len(results) == 0 {
		return 0, "", fmt.Errorf("未找到与「%s」匹配的用户", input)
	}
	return results[0].Mid, results[0].Name, nil
}

// fetchImage 从 URL 下载并解码图片（支持 JPEG/PNG）。
func fetchImage(rawURL string) (image.Image, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.bilibili.com")
	req.Header.Set("Cookie", generateBiliCookies())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	return img, err
}

// FetchVideos 获取 UP 主的视频列表（按发布时间排序，每页 5 条）。
func (c *biliClient) FetchVideos(ctx context.Context, mid int64, page int) ([]VideoItem, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/space/wbi/arc/search?mid=%d&pn=%d&ps=5&order=pubdate", mid, page)
	req, err := c.newBiliRequest(ctx, http.MethodGet, u, "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("FetchVideos: %w", err)
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			List struct {
				VList []VideoItem `json:"vlist"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("FetchVideos json: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili video api error: %s (code=%d)", result.Message, result.Code)
	}
	return result.Data.List.VList, nil
}
