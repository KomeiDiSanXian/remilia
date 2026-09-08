package pic

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePicConfig 最小化的 plugin.ConfigReader 实现，用于测试配置读取。
type fakePicConfig struct {
	vals map[string]any
}

func (f *fakePicConfig) Get(k string) any { return f.vals[k] }
func (f *fakePicConfig) GetString(k, d string) string {
	if v, ok := f.vals[k].(string); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetInt(k string, d int) int {
	if v, ok := f.vals[k].(int); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetBool(k string, d bool) bool {
	if v, ok := f.vals[k].(bool); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetDuration(k string, d time.Duration) time.Duration {
	if v, ok := f.vals[k].(time.Duration); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetFloat64(k string, d float64) float64 {
	if v, ok := f.vals[k].(float64); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetStringSlice(k string, d []string) []string {
	if v, ok := f.vals[k].([]string); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetStringMap(k string, d map[string]any) map[string]any {
	if v, ok := f.vals[k].(map[string]any); ok {
		return v
	}
	return d
}
func (f *fakePicConfig) GetAll() map[string]any { return f.vals }

func TestParsePicArgs(t *testing.T) {
	// 无参数 → 随机 1 张
	args := parsePicArgs(nil, 3)
	assert.Equal(t, 1, args.Count)
	assert.Empty(t, args.Tags)
	assert.Empty(t, args.Site)

	// 纯标签
	args = parsePicArgs([]string{"touhou", "hairband"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"touhou", "hairband"}, args.Tags)

	// 末尾 xN 后缀（不区分大小写）为数量
	args = parsePicArgs([]string{"cat", "x3"}, 3)
	assert.Equal(t, 3, args.Count)
	assert.Equal(t, []string{"cat"}, args.Tags)

	args = parsePicArgs([]string{"cat", "X2"}, 3)
	assert.Equal(t, 2, args.Count)

	// 无标签时 /pic x3 = 3 张随机
	args = parsePicArgs([]string{"x3"}, 3)
	assert.Equal(t, 3, args.Count)
	assert.Empty(t, args.Tags)

	// 数量超过 maxCount 被截断
	args = parsePicArgs([]string{"cat", "x10"}, 3)
	assert.Equal(t, 3, args.Count)

	// 非法数量后缀视为标签
	args = parsePicArgs([]string{"x", "xabc"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"x", "xabc"}, args.Tags)

	// 中间位置的 xN 一律视为标签（消歧）
	args = parsePicArgs([]string{"x2", "cat"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"x2", "cat"}, args.Tags)

	args = parsePicArgs([]string{"cat", "x2", "dog"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"cat", "x2", "dog"}, args.Tags)

	// -site 指定站点
	args = parsePicArgs([]string{"-site", "rule34", "xxx"}, 3)
	assert.Equal(t, "rule34", args.Site)
	assert.Equal(t, []string{"xxx"}, args.Tags)

	// 组合
	args = parsePicArgs([]string{"touhou", "-site", "konachan", "x2"}, 3)
	assert.Equal(t, 2, args.Count)
	assert.Equal(t, "konachan", args.Site)
	assert.Equal(t, []string{"touhou"}, args.Tags)

	// -count 显式张数（与 xN 等价，优先级更高）
	args = parsePicArgs([]string{"-count", "2", "cat"}, 3)
	assert.Equal(t, 2, args.Count)
	assert.Equal(t, []string{"cat"}, args.Tags)

	args = parsePicArgs([]string{"-count", "3"}, 3)
	assert.Equal(t, 3, args.Count)
	assert.Empty(t, args.Tags)

	// -count 存在时末尾 xN 作为标签（消歧）
	args = parsePicArgs([]string{"x3", "-count", "1"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"x3"}, args.Tags)

	args = parsePicArgs([]string{"cat", "x2", "-count", "3"}, 3)
	assert.Equal(t, 3, args.Count)
	assert.Equal(t, []string{"cat", "x2"}, args.Tags)

	// -count 非法值回退默认 1
	args = parsePicArgs([]string{"cat", "-count", "abc"}, 3)
	assert.Equal(t, 1, args.Count)
	assert.Equal(t, []string{"cat"}, args.Tags)
}

// TestParsePicArgsRecent 验证 -recent 时间过滤参数。
func TestParsePicArgsRecent(t *testing.T) {
	// 未指定 → Recent = -1（使用配置默认值）
	args := parsePicArgs([]string{"cat"}, 3)
	assert.Equal(t, -1, args.Recent)
	assert.Equal(t, []string{"cat"}, args.Tags)

	// -recent 30 → 近 30 天
	args = parsePicArgs([]string{"cat", "-recent", "30"}, 3)
	assert.Equal(t, 30, args.Recent)
	assert.Equal(t, []string{"cat"}, args.Tags)

	// -recent 0 → 不过滤
	args = parsePicArgs([]string{"-recent", "0", "touhou"}, 3)
	assert.Equal(t, 0, args.Recent)
	assert.Equal(t, []string{"touhou"}, args.Tags)

	// -recent all → 不过滤（等价 0）
	args = parsePicArgs([]string{"cat", "-recent", "all"}, 3)
	assert.Equal(t, 0, args.Recent)

	// -recent 非法值 → 回退未指定（-1，使用配置默认）
	args = parsePicArgs([]string{"cat", "-recent", "abc"}, 3)
	assert.Equal(t, -1, args.Recent)
	assert.Equal(t, []string{"cat"}, args.Tags)

	// 与其他参数共存
	args = parsePicArgs([]string{"touhou", "-recent", "90", "-site", "yandere", "x2"}, 3)
	assert.Equal(t, 90, args.Recent)
	assert.Equal(t, "yandere", args.Site)
	assert.Equal(t, 2, args.Count)
	assert.Equal(t, []string{"touhou"}, args.Tags)
}

func TestFormatResults(t *testing.T) {
	posts := []picPost{
		{
			ID: 1, Rating: RatingSafe, Tags: []string{"touhou", "reimu"},
			FileURL: "https://konachan.com/image/1.jpg", Source: "https://twitter.com/zun",
			Author: "zun", Score: 42, SiteName: "Konachan",
		},
	}

	md := formatResultsMD("Konachan", posts)
	assert.Contains(t, md, "🖼 随机图片 *1* 张（来自 Konachan）")
	assert.Contains(t, md, "画师: **zun**")
	assert.Contains(t, md, "[来源](https://twitter.com/zun)")
	assert.Contains(t, md, "[原始图](https://konachan.com/image/1.jpg)",
		"原始图应为可点击链接而非裸 URL")
	assert.NotContains(t, md, "[原始图](https://konachan.com/image/1.jpg)\n原图",
		"不应再有裸 URL 行")

	text := formatResultsText("Konachan", posts)
	assert.Contains(t, text, "随机图片 1 张（来自 Konachan）")
	assert.Contains(t, text, "画师: zun")
	assert.Contains(t, text, "来源: https://twitter.com/zun")

	// 无来源时给出兜底文案
	posts[0].Source = ""
	text = formatResultsText("Konachan", posts)
	assert.Contains(t, text, "来源: 无来源")

	// AI 工具结果
	tool := formatToolResult("Konachan", posts)
	assert.Contains(t, tool, "1. https://konachan.com/image/1.jpg")
	assert.Contains(t, tool, "标签: touhou, reimu")
}

func TestFormatPostSourceURLNormalization(t *testing.T) {
	post := picPost{
		Source: "//twitter.com/zun", FileURL: "https://x/img.jpg", SiteName: "Konachan",
	}
	out := formatPostText(post, 1)
	assert.Contains(t, out, "https://twitter.com/zun")
}

func TestFormatPostMDLinks(t *testing.T) {
	// 常规来源：来源与原始图均为链接
	post := picPost{Source: "https://twitter.com/zun", FileURL: "https://konachan.net/a.png"}
	md := formatPostMD(post, 1)
	assert.Contains(t, md, "[来源](https://twitter.com/zun)  |  [原始图](https://konachan.net/a.png)")

	// 无来源：只渲染原始图链接，不出现空链接
	post.Source = ""
	md = formatPostMD(post, 1)
	assert.NotContains(t, md, "[来源]()")
	assert.Contains(t, md, "[原始图](https://konachan.net/a.png)")

	// 协议相对来源补全 https
	post.Source = "//twitter.com/zun"
	md = formatPostMD(post, 1)
	assert.Contains(t, md, "[来源](https://twitter.com/zun)")
}

// TestMdLinkHrefParensEscaped URL 中的裸括号会被 Markdown 链接解析截断，
// 必须 percent-encode（RFC 3986 合法，语义不变）。
func TestMdLinkHrefParensEscaped(t *testing.T) {
	in := "https://konachan.net/image/abc/Foo_(series)%20bar.png"
	assert.Equal(t,
		"https://konachan.net/image/abc/Foo_%28series%29%20bar.png",
		mdLinkHref(in))
	assert.Equal(t, "https://x/plain.png", mdLinkHref("https://x/plain.png"))
}

func TestSendPicCompressionConfig(t *testing.T) {
	p := &Plugin{}
	assert.Equal(t, int64(5*1024*1024), p.sendPicMaxBytes())
	assert.Equal(t, 4096, p.sendPicMaxDimension())

	p2 := &Plugin{cfg: &fakePicConfig{vals: map[string]any{
		"send_thumbnail_max_bytes":     1024 * 1024,
		"send_thumbnail_max_dimension": 2000,
	}}}
	assert.Equal(t, int64(1024*1024), p2.sendPicMaxBytes())
	assert.Equal(t, 2000, p2.sendPicMaxDimension())

	p3 := &Plugin{cfg: &fakePicConfig{vals: map[string]any{"send_thumbnail_max_bytes": 0}}}
	assert.Equal(t, int64(0), p3.sendPicMaxBytes(), "0 表示关闭体积压缩")
}

func TestSniffMimeAndExt(t *testing.T) {
	assert.Equal(t, "image/jpeg", sniffMime(nil))
	assert.Equal(t, "image/jpeg", sniffMime([]byte("not an image")))
	assert.Equal(t, ".jpg", extByMime("image/jpeg"))
	assert.Equal(t, ".png", extByMime("image/png"))
	assert.Equal(t, ".gif", extByMime("image/gif"))
	assert.Equal(t, ".webp", extByMime("image/webp"))
}

// TestSanitizeUserTags 验证 meta 标签 / 取反 / 通配符注入被丢弃：
// 这些注入可绕过内容分级（rating:questionable 等，实测生效）或破坏查询。
func TestSanitizeUserTags(t *testing.T) {
	clean, dropped := sanitizeUserTags([]string{
		"cat", " rating:explicit ", "rating:e", "-dog", "sort:score",
		"order:random", "date:2020-01-01..", "a*", "b?", "", "  ",
		"TOUHOU",
	})
	assert.Equal(t, []string{"cat", "TOUHOU"}, clean)
	assert.Equal(t, []string{"rating:explicit", "rating:e", "-dog", "sort:score", "order:random", "date:2020-01-01..", "a*", "b?"}, dropped)

	// 正常标签不受影响
	clean, dropped = sanitizeUserTags([]string{"touhou", "hair_band"})
	assert.Equal(t, []string{"touhou", "hair_band"}, clean)
	assert.Empty(t, dropped)
}

// TestFetchPostsSanitizesTags 端到端验证 fetchPosts 在请求前过滤注入标签。
func TestFetchPostsSanitizesTags(t *testing.T) {
	var gotTags atomic.Value
	srv, client := newGelbooruTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotTags.Store(r.URL.Query().Get("tags"))
		_, _ = w.Write([]byte(`[{"id":1,"file_url":"https://x/1.jpg","tags":"a","change":123}]`))
	})
	defer srv.Close()

	c := &booruClient{httpClient: client}
	p := &Plugin{client: c}
	s, _ := findSite("safebooru") // 真实站点模型（含 q/e 档位）
	s.Domain = srv.Listener.Addr().String()

	_, err := p.fetchPosts(context.Background(), s,
		[]string{"cat", "rating:explicit", "-dog", "sort:score"}, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "cat -rating:questionable sort:random", gotTags.Load(),
		"注入的 meta/取反标签应被过滤，仅保留合法标签与插件自身注入的 rating/sort 标签")
}
