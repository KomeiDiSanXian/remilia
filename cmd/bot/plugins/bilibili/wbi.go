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
	mu     sync.RWMutex
	keys   *wbiKeys
	client *http.Client
}

// newWBISigner 创建 WBI 签名器。
func newWBISigner() *wbiSigner {
	return &wbiSigner{client: http.DefaultClient}
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
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
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
		if dot := strings.Index(part, "."); dot >= 0 {
			return part[:dot]
		}
		return part
	}
	return ""
}

// sign 为请求参数添加 WBI 签名。算法：
//  1. 混合密钥: mix_key = sub_key[:4] + img_key[:4]
//  2. 添加时间戳 wts
//  3. 参数按 key 排序后拼接 + mix_key
//  4. 计算 MD5 作为 w_rid
func (s *wbiSigner) sign(ctx context.Context, params url.Values) (url.Values, error) {
	if err := s.ensureKeys(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	mixKey := s.keys.subKey[:4] + s.keys.imgKey[:4]
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
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params.Get(k))
		sb.WriteString("&")
	}
	sb.WriteString(mixKey)

	hash := md5.Sum([]byte(sb.String()))
	wRid := hex.EncodeToString(hash[:])
	params.Set("w_rid", wRid)

	return params, nil
}
