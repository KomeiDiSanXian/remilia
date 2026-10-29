package qq_test

// ark_data_test.go — 入站结构化卡片（message_type=3）的解析辅助。
//
// 官方在 2026-09-16 的变更里补全了 ark_type 枚举（新增 picture），并把
// fields 的常见键名写进了文档。业务侧通常只关心"这张卡片是什么类型、标题和
// 预览图是什么"，因此这里把解析收敛成 ArkDataFromSegment / ParseArkData，
// 而不要求调用方自己掏 gjson。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArkData_MiniApp(t *testing.T) {
	raw := `{
		"ark_type": "miniapp",
		"ark_name": "小程序",
		"prompt": "[小程序] 点我",
		"fields": {
			"title": "哔哩哔哩",
			"desc": "一个视频小程序",
			"jump_url": "https://b23.tv/xxx",
			"preview": "https://i0.hdslb.com/cover.jpg",
			"tag": ["视频", "推荐"]
		}
	}`

	data, ok := qq.ParseArkData(raw)
	require.True(t, ok)
	assert.Equal(t, qq.ArkTypeMiniApp, data.Type)
	assert.Equal(t, "小程序", data.Name)
	assert.Equal(t, "[小程序] 点我", data.Prompt)
	assert.Equal(t, "哔哩哔哩", data.Fields["title"])
	assert.Equal(t, "https://i0.hdslb.com/cover.jpg", data.Fields["preview"])
	assert.Equal(t, "视频,推荐", data.Fields["tag"], "数组字段应逗号拼接")
}

// TestParseArkData_Picture 覆盖 2026-09-16 新增的 ark_type=picture。
func TestParseArkData_Picture(t *testing.T) {
	raw := `{"ark_type":"picture","ark_name":"图片","fields":{"preview":"https://example.com/a.png","width":"800"}}`

	data, ok := qq.ParseArkData(raw)
	require.True(t, ok)
	assert.Equal(t, qq.ArkTypePicture, data.Type)
	assert.Equal(t, "https://example.com/a.png", data.Fields["preview"])
	assert.Equal(t, "800", data.Fields["width"], "数值字段也按字符串收集")
}

// TestParseArkData_NoFields 验证缺 fields 时不 panic 且 map 为空。
func TestParseArkData_NoFields(t *testing.T) {
	data, ok := qq.ParseArkData(`{"ark_type":"feed"}`)
	require.True(t, ok)
	assert.Equal(t, qq.ArkTypeFeed, data.Type)
	assert.Empty(t, data.Fields)
}

// TestParseArkData_NestedObject 验证对象类型字段保留原始 JSON
// （官方文档未定义嵌套结构，原样透传比猜字段安全）。
func TestParseArkData_NestedObject(t *testing.T) {
	raw := `{"ark_type":"map","fields":{"address":{"name":"深圳","lat":22.5}}}`
	data, ok := qq.ParseArkData(raw)
	require.True(t, ok)
	assert.JSONEq(t, `{"name":"深圳","lat":22.5}`, data.Fields["address"])
}

func TestParseArkData_Invalid(t *testing.T) {
	for _, raw := range []string{"", "{not json", "null"} {
		_, ok := qq.ParseArkData(raw)
		assert.False(t, ok, "raw=%q 应被拒绝", raw)
	}
}

// TestArkDataFromSegment 验证从事件段直接取卡片信息（业务侧的主要入口）。
func TestArkDataFromSegment(t *testing.T) {
	seg := platform.Segment{
		Type: platform.SegmentUnknown,
		Extra: map[string]any{
			"type":             "ark_data",
			qq.ExtraKeyArkData: `{"ark_type":"feed","fields":{"title":"群相册"}}`,
		},
	}
	data, ok := qq.ArkDataFromSegment(seg)
	require.True(t, ok)
	assert.Equal(t, qq.ArkTypeFeed, data.Type)
	assert.Equal(t, "群相册", data.Fields["title"])

	// 非卡片段（如纯文本段）不应被误认
	_, ok = qq.ArkDataFromSegment(platform.Segment{Type: platform.SegmentText, Text: "hi"})
	assert.False(t, ok)

	// Extra 里没有 ark_data 键
	_, ok = qq.ArkDataFromSegment(platform.Segment{Extra: map[string]any{"type": "ark_data"}})
	assert.False(t, ok)
}
