package pic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	assert.Contains(t, md, "https://konachan.com/image/1.jpg")

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
