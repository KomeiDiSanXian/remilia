//go:build network

// 临时联网验证：确认各站点 API 请求、响应解析、图片下载真实可用。
// 运行: go test -tags network -run TestNetworkFetchAllSites -v ./cmd/bot/plugins/pic/
// 需要认证的站点（gelbooru / rule34）通过环境变量传入凭据：
//
//	PIC_GELBOORU_USER_ID / PIC_GELBOORU_API_KEY
//	PIC_RULE34_USER_ID / PIC_RULE34_API_KEY
package pic

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mustNewClient 测试辅助：创建客户端并忽略代理配置错误（测试不传代理）。
func mustNewClient(t *testing.T, creds booruCredentials) *booruClient {
	t.Helper()
	c, err := newBooruClient(creds, "")
	require.NoError(t, err)
	return c
}

func TestNetworkFetchAllSites(t *testing.T) {
	creds := booruCredentials{
		GelbooruUserID: os.Getenv("PIC_GELBOORU_USER_ID"),
		GelbooruAPIKey: os.Getenv("PIC_GELBOORU_API_KEY"),
		Rule34UserID:   os.Getenv("PIC_RULE34_USER_ID"),
		Rule34APIKey:   os.Getenv("PIC_RULE34_API_KEY"),
	}

	for _, s := range builtinSites {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			if s.Name == "gelbooru" && (creds.GelbooruUserID == "" || creds.GelbooruAPIKey == "") {
				t.Skip("PIC_GELBOORU_USER_ID/PIC_GELBOORU_API_KEY 未配置，跳过 gelbooru")
			}
			if s.Name == "rule34" && (creds.Rule34UserID == "" || creds.Rule34APIKey == "") {
				t.Skip("PIC_RULE34_USER_ID/PIC_RULE34_API_KEY 未配置，跳过 rule34")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			c := mustNewClient(t, creds)
			// 用该站点允许的最高分级验证（rule34 在 safe 策略下本就不可用）
			posts, err := c.fetchRandom(ctx, s, nil, RatingRange{Min: RatingSafe, Max: RatingExplicit}, 2, 730)
			if err != nil {
				t.Fatalf("fetch failed: %v", err)
			}
			if len(posts) == 0 {
				t.Fatalf("no posts returned")
			}
			t.Logf("got %d posts", len(posts))

			for _, p := range posts {
				if p.FileURL == "" {
					t.Error("empty FileURL")
					continue
				}
				data, err := c.downloadImage(ctx, p.FileURL, "https://"+s.Domain+"/", maxPicBytes)
				if err != nil {
					t.Errorf("download %s: %v", p.FileURL, err)
					continue
				}
				if !isImageBytes(data) {
					t.Errorf("downloaded bytes are not an image: %s (%d bytes)", p.FileURL, len(data))
					continue
				}
				t.Logf("ok: %s (%d bytes, score=%d, author=%q)", p.FileURL, len(data), p.Score, p.Author)
			}
		})
	}
}

func TestNetworkFetchWithTags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := mustNewClient(t, booruCredentials{})
	s, _ := findSite("safebooru")
	posts, err := c.fetchRandom(ctx, s, []string{"touhou"}, RatingRange{Min: RatingSafe, Max: RatingExplicit}, 3, 730)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(posts) == 0 {
		t.Fatal("no posts returned for tag touhou")
	}
	t.Logf("got %d posts for tag touhou", len(posts))
}

// TestNetworkRecencyFilter 验证 recent_days 过滤真实生效：
// 所有返回帖子的 Change（上传时间）都应落在近 N 天内。
func TestNetworkRecencyFilter(t *testing.T) {
	creds := booruCredentials{
		GelbooruUserID: os.Getenv("PIC_GELBOORU_USER_ID"),
		GelbooruAPIKey: os.Getenv("PIC_GELBOORU_API_KEY"),
		Rule34UserID:   os.Getenv("PIC_RULE34_USER_ID"),
		Rule34APIKey:   os.Getenv("PIC_RULE34_API_KEY"),
	}

	recentDays := 730
	cutoff := time.Now().AddDate(0, 0, -recentDays).Unix()

	for _, s := range builtinSites {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			if s.Name == "gelbooru" && (creds.GelbooruUserID == "" || creds.GelbooruAPIKey == "") {
				t.Skip("凭据未配置，跳过 gelbooru")
			}
			if s.Name == "rule34" && (creds.Rule34UserID == "" || creds.Rule34APIKey == "") {
				t.Skip("凭据未配置，跳过 rule34")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			c := mustNewClient(t, creds)
			posts, err := c.fetchRandom(ctx, s, nil, RatingRange{Min: RatingSafe, Max: RatingExplicit}, 3, recentDays)
			if err != nil {
				t.Fatalf("fetch failed: %v", err)
			}
			require.NotEmpty(t, posts)
			for _, p := range posts {
				if p.Change < cutoff {
					t.Errorf("%s: post %d 上传时间 %d 早于截止 %d（不在近 %d 天内）",
						s.DisplayName, p.ID, p.Change, cutoff, recentDays)
				}
			}
		})
	}
}

// TestNetworkFetchWithFallback 验证多站并发取最快成功者的降级路径与耗时。
func TestNetworkFetchWithFallback(t *testing.T) {
	creds := booruCredentials{
		GelbooruUserID: os.Getenv("PIC_GELBOORU_USER_ID"),
		GelbooruAPIKey: os.Getenv("PIC_GELBOORU_API_KEY"),
		Rule34UserID:   os.Getenv("PIC_RULE34_USER_ID"),
		Rule34APIKey:   os.Getenv("PIC_RULE34_API_KEY"),
	}
	p := &Plugin{client: mustNewClient(t, creds)}
	candidates := candidateSites(nil, RatingRange{Min: RatingSafe, Max: RatingExplicit})
	require.NotEmpty(t, candidates)

	start := time.Now()
	s, posts, err := p.fetchWithFallback(context.Background(), candidates, []string{"touhou"}, 2, 730)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, posts)
	t.Logf("fallback 耗时 %v，命中站点 %s，%d 张", elapsed, s.DisplayName, len(posts))
}

// TestNetworkProxyConfig 验证 plugins.pic.proxy 配置独立生效：
// 即使环境变量代理被清空，客户端级代理仍能访问被墙站点。
func TestNetworkProxyConfig(t *testing.T) {
	creds := booruCredentials{
		GelbooruUserID: os.Getenv("PIC_GELBOORU_USER_ID"),
		GelbooruAPIKey: os.Getenv("PIC_GELBOORU_API_KEY"),
		Rule34UserID:   os.Getenv("PIC_RULE34_USER_ID"),
		Rule34APIKey:   os.Getenv("PIC_RULE34_API_KEY"),
	}
	proxyURL := os.Getenv("PIC_PROXY_URL")
	if proxyURL == "" {
		t.Skip("PIC_PROXY_URL 未配置，跳过代理验证")
	}

	// 清空环境变量代理，仅依赖客户端配置
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")

	c, err := newBooruClient(creds, proxyURL)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, _ := findSite("safebooru")
	posts, err := c.fetchRandom(ctx, s, []string{"touhou"}, RatingRange{Min: RatingSafe, Max: RatingExplicit}, 1, 730)
	require.NoError(t, err)
	require.NotEmpty(t, posts)
	t.Logf("客户端级代理生效：%s", posts[0].FileURL)
}

func isImageBytes(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if data[0] == 0xFF && data[1] == 0xD8 { // JPEG
		return true
	}
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' { // PNG
		return true
	}
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' { // GIF
		return true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" { // WebP
		return true
	}
	return false
}
