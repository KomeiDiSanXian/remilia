// Package bilibili 提供 Bilibili UP 主信息、直播状态查询和用户搜索功能。
//
// 命令: /bili user/live/search
// AI 工具: search_bilibili_user, get_bilibili_user_info, get_bilibili_live_status
package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// wbiKeys 存储 B 站 WBI 签名的 img_key 和 sub_key。
type wbiKeys struct {
	imgKey string
	subKey string
}

// wbiSigner 实现 B 站 WBI 签名算法，线程安全。
// 通过从 nav 接口获取密钥对，对请求参数进行排序后计算 MD5 签名。
type wbiSigner struct {
	mu       sync.RWMutex
	keys     *wbiKeys
	client   *http.Client
	sessdata string // 真实账号登录 Cookie（SESSDATA），用于规避风控限流
}

// newWBISigner 创建 WBI 签名器。
func newWBISigner(sessdata string) *wbiSigner {
	return &wbiSigner{client: http.DefaultClient, sessdata: sessdata}
}

// ensureKeys 确保密钥已加载。首次调用时会从 nav 接口拉取并缓存。
func (s *wbiSigner) ensureKeys(ctx context.Context) error {
	s.mu.RLock()
	if s.keys != nil {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys != nil {
		return nil
	}

	keys, err := s.fetchKeys(ctx)
	if err != nil {
		return fmt.Errorf("fetch wbi keys: %w", err)
	}
	s.keys = keys
	return nil
}

// fetchKeys 从 B 站导航接口获取 WBI 签名用的 img_key 和 sub_key。
// 这两个 key 从 data.wbi_img.img_url 和 data.wbi_img.sub_url 的路径中提取。
func (s *wbiSigner) fetchKeys(ctx context.Context) (*wbiKeys, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bilibili.com/x/web-interface/nav", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://www.bilibili.com")
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("Cookie", generateBiliCookies(s.sessdata))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("WBI nav 返回了 HTML 页面，可能触发了反爬: %s", string(body[:min(len(body), 120)]))
	}

	var nav struct {
		Code int `json:"code"`
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &nav); err != nil {
		return nil, err
	}
	if nav.Code != 0 {
		return nil, fmt.Errorf("nav api error: code=%d", nav.Code)
	}

	imgKey := extractKey(nav.Data.WbiImg.ImgURL)
	subKey := extractKey(nav.Data.WbiImg.SubURL)
	if imgKey == "" || subKey == "" {
		return nil, fmt.Errorf("cannot extract wbi keys from nav response")
	}
	return &wbiKeys{imgKey: imgKey, subKey: subKey}, nil
}

// extractKey 从形如 "https://i0.hdslb.com/bfs/seed/abc123.png" 的 URL 中提取 "abc123"。
func extractKey(rawURL string) string {
	if idx := strings.LastIndex(rawURL, "/"); idx >= 0 {
		part := rawURL[idx+1:]
		if before, _, ok := strings.Cut(part, "."); ok {
			return before
		}
		return part
	}
	return ""
}

// mixinKeyEncTab WBI 混合密钥索引表（官方算法）。
// mixin key = 取 img_key+sub_key 拼接串中这些索引位置的字符（前 32 个）。
var mixinKeyEncTab = [32]int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
}

// mixinKey 计算 WBI 混合密钥：
// 将 img_key 与 sub_key 拼接后，按 mixinKeyEncTab 索引取出 32 个字符。
func mixinKey(imgKey, subKey string) string {
	concat := imgKey + subKey
	b := make([]byte, 0, len(mixinKeyEncTab))
	for _, i := range mixinKeyEncTab {
		b = append(b, concat[i])
	}
	return string(b)
}

// sign 为请求参数添加 WBI 签名。算法：
//  1. 混合密钥: mixin_key = mixinKey(img_key, sub_key)
//  2. 添加时间戳 wts
//  3. 参数按 key 排序后拼接 + mixin_key
//  4. 计算 MD5 作为 w_rid
func (s *wbiSigner) sign(ctx context.Context, params url.Values) (url.Values, error) {
	if err := s.ensureKeys(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	subKey, imgKey := s.keys.subKey, s.keys.imgKey
	s.mu.RUnlock()

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	params.Set("wts", ts)

	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "w_rid" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params.Get(k))
	}
	sb.WriteString(mixinKey(imgKey, subKey))

	hash := md5.Sum([]byte(sb.String()))
	wRid := hex.EncodeToString(hash[:])
	params.Set("w_rid", wRid)

	return params, nil
}
