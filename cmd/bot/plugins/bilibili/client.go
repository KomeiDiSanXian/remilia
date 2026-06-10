package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// UserInfo B 站用户空间信息。
type UserInfo struct {
	Mid    int64  `json:"mid"`
	Name   string `json:"name"`
	Sign   string `json:"sign"`
	Avatar string `json:"face"`
	Level  int    `json:"level"`
}

// RelationStat B 站用户关系统计（关注、粉丝、动态数）。
type RelationStat struct {
	Following int64 `json:"following"`
	Follower  int64 `json:"follower"`
	Dynamic   int64 `json:"dynamic"`
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

// biliClient B 站 API 客户端，封装所有对 B 站接口的调用。
type biliClient struct {
	signer *wbiSigner
	client *http.Client
}

// newBiliClient 创建 B 站 API 客户端。
func newBiliClient() *biliClient {
	return &biliClient{
		signer: newWBISigner(),
		client: http.DefaultClient,
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bilibili.com/x/space/wbi/acc/info?"+signed.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"message"`
		Data    UserInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, err
	}
	if result.Code != 0 {
		return nil, nil, fmt.Errorf("bilibili api error: %s (code=%d)", result.Message, result.Code)
	}

	rel, _ := c.FetchRelationStat(ctx, mid)
	return &result.Data, rel, nil
}

// fetchUserInfoFallback 使用无 WBI 签名的旧接口获取用户信息。
func (c *biliClient) fetchUserInfoFallback(ctx context.Context, mid int64) (*UserInfo, *RelationStat, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/space/acc/info?mid=%d", mid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"message"`
		Data    UserInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, err
	}
	if result.Code != 0 {
		return nil, nil, fmt.Errorf("bilibili api error: %s (code=%d)", result.Message, result.Code)
	}

	rel, _ := c.FetchRelationStat(ctx, mid)
	return &result.Data, rel, nil
}

// FetchRelationStat 获取 UP 主的关注、粉丝、动态数量。
func (c *biliClient) FetchRelationStat(ctx context.Context, mid int64) (*RelationStat, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/relation/stat?vmid=%d", mid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    RelationStat  `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili relation api error: %s (code=%d)", result.Message, result.Code)
	}
	return &result.Data, nil
}

// FetchLiveInfo 获取 UP 主的直播状态和直播间信息。
func (c *biliClient) FetchLiveInfo(ctx context.Context, mid int64) (*LiveInfo, error) {
	u := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/getRoomInfoOld?mid=%d", mid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://live.bilibili.com/")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    LiveInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili live api error: %s (code=%d)", result.Message, result.Code)
	}
	result.Data.IsLiving = result.Data.LiveStatus == 1
	return &result.Data, nil
}

// SearchUser 按关键词搜索 B 站用户，返回匹配的用户列表和总结果数。
// 该接口无需 WBI 签名。
func (c *biliClient) SearchUser(ctx context.Context, keyword string, page int) ([]SearchUserResult, int, error) {
	u := fmt.Sprintf("https://api.bilibili.com/x/web-interface/search/type?search_type=bili_user&keyword=%s&page=%d", url.QueryEscape(keyword), page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://search.bilibili.com")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
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
		return nil, 0, err
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
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.bilibili.com")

	resp, err := http.DefaultClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://space.bilibili.com/"+strconv.FormatInt(mid, 10))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili video api error: %s (code=%d)", result.Message, result.Code)
	}
	return result.Data.List.VList, nil
}
