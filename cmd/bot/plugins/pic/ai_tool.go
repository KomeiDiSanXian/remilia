// Package pic ai_tool.go — 向 AI 插件暴露随机图片工具。
//
// 实现 ai.ToolProvider 接口，由 AI 插件在容器冻结后通过
// DiscoverToolProviders 自动发现注册。工具返回纯文本（图片 URL 与
// 作品信息），不依赖多模态能力；需要把图片发给用户时 LLM 可使用
// /pic 命令（自动发现即可触发）。
package pic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// ListTools 实现 ai.ToolProvider，提供 get_random_image 工具。
//
// 参数：
//   - tags: 标签列表（空格分隔），可为空表示完全随机
//   - count: 张数（1-3），可选
//   - site: 指定站点名（可选，受 rating 策略约束）
//   - recent: 近 N 天内上传的图片（可选，默认取配置 recent_days；0 = 不过滤）
//
// 返回图片 URL 与作品信息文本，不下载图片字节。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{{
		Name:        "get_random_image",
		Categories:  []string{"pic"},
		Description: "根据标签从图库获取随机图片的 URL 与作品信息（画师、来源、标签）。需要直接发送图片给用户时，提示用户使用 /pic 命令",
		Parameters: ai.ToolParamSchema{
			Type: "object",
			Properties: map[string]ai.ToolParamSchema{
				"tags": {
					Type:        "string",
					Description: "图片标签，多个标签用空格分隔，如 \"touhou hairband\"；为空表示完全随机",
				},
				"count": {
					Type:        "integer",
					Description: "返回张数，1-3，默认 1",
				},
				"site": {
					Type:        "string",
					Description: "指定图库站点：safebooru / gelbooru / rule34 / konachan / yandere（可选，默认自动选择）",
				},
				"recent": {
					Type:        "integer",
					Description: "只取近 N 天内上传的图片（0 = 不过滤；默认取配置 recent_days，通常为 730）",
				},
			},
		},
		Execute: p.executePicTool,
	}}
}

// executePicTool 是 get_random_image 工具的 Execute 回调。
func (p *Plugin) executePicTool(ctx context.Context, args map[string]any) (string, error) {
	count := 1
	if v, ok := args["count"].(float64); ok && int(v) > 0 {
		count = int(v)
	}
	if count > p.maxCount() {
		count = p.maxCount()
	}

	tags := strings.Fields(firstString(args["tags"]))

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	siteName, _ := args["site"].(string)
	var candidates []site
	if siteName != "" {
		s, ok := p.resolveSite(strings.TrimSpace(siteName))
		if !ok {
			return "", fmt.Errorf("站点 %q 不可用（不存在或当前 rating 策略下不允许）", siteName)
		}
		candidates = []site{s}
	} else {
		candidates = candidateSites(p.enabledSites(), p.rating())
		if len(candidates) == 0 {
			return "", fmt.Errorf("没有可用的图库站点（请检查 sites 白名单与 rating 配置）")
		}
	}

	// recent 参数覆盖配置默认值（0 = 不过滤；未指定用 recent_days）
	recentDays := p.recentDays()
	if v, ok := args["recent"].(float64); ok {
		recentDays = max(int(v), 0)
	}

	s, posts, err := p.fetchWithFallback(reqCtx, candidates, tags, count, recentDays)
	if err != nil {
		return "", err
	}
	if len(posts) == 0 {
		return fmt.Sprintf("没有找到匹配标签 %q 的图片", strings.Join(tags, " ")), nil
	}
	return formatToolResult(s.DisplayName, posts), nil
}

// firstString 提取字符串参数，非字符串类型返回空字符串。
func firstString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
