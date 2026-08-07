// Package sauce ai_tool.go — 以图搜图能力向 AI 插件暴露为工具。
//
// Plugin 实现 ai.ToolProvider 接口，AI 插件在容器冻结后通过
// DiscoverToolProviders 自动扫描并注册本插件提供的工具，无需手动注册。
// 工具名 "sauce" 与 /sauce 命令同名，显式注册会覆盖 AI 对命令的自动发现
// （自动发现的命令工具无法携带图片，对搜图场景无意义）。
package sauce

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// aiToolName 提供给 AI 的工具名，与 /sauce 命令同名以便 LLM 理解。
const aiToolName = "sauce"

// ListTools 实现 ai.ToolProvider，由 AI 插件的 DiscoverToolProviders
// 自动发现注册（无需手动调用 RegisterToolProvider）。
//
// 工具接受 image_url（必填）以及可选的 db / max_results 参数，
// 执行时下载图片并走与 /sauce 命令完全相同的多引擎检索流程。
// 不暴露权限敏感能力：仅返回来源检索结果文本。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{{
		Name:        aiToolName,
		Description: "以图搜图，聚合 SauceNAO / IQDB / TraceMoe / AnimeTrace 查找图片出处（画作、插画、动画截图等）。提供图片 URL 作为 image_url 参数即可检索",
		Categories:  []string{ai.CategoryGeneral},
		Parameters: ai.ToolParamSchema{
			Type: "object",
			Properties: map[string]ai.ToolParamSchema{
				"image_url": {
					Type:        "string",
					Description: "要搜索来源的图片的公开可访问 URL（必填）",
				},
				"db": {
					Type:        "integer",
					Description: "SauceNAO 检索数据库 ID，999 = 全部（默认 999）",
				},
				"max_results": {
					Type:        "integer",
					Description: "最多返回的结果数，默认 3",
				},
			},
			Required: []string{"image_url"},
		},
		Execute: p.executeSauceTool,
	}}
}

// executeSauceTool 是 sauce 工具的 Execute 回调。
//
// 流程与 /sauce 命令一致：下载图片 → 预处理 → 并发检索各已启用引擎 →
// 合并去重排序 → 返回格式化文本。与命令模式的区别在于直接以文本
// 形式返回结果给 LLM，不附带缩略图。
func (p *Plugin) executeSauceTool(ctx context.Context, args map[string]any) (string, error) {
	imageURL := strings.TrimSpace(firstStringArg(args["image_url"]))
	if imageURL == "" {
		return "", fmt.Errorf("缺少必填参数 image_url（图片的公开 URL）")
	}

	maxResults := p.maxResults()
	if v, ok := args["max_results"].(float64); ok && int(v) > 0 {
		maxResults = int(v)
	}

	db := p.saucenaoDB()
	if v, ok := args["db"].(float64); ok && int(v) > 0 {
		db = int(v)
	}
	customDB := db != p.saucenaoDB()
	if customDB && p.apiKey() == "" {
		// 提前校验：非默认数据库只能走 SauceNAO，未配置 key 时直接报错，
		// 避免先下载图片后才发现无法检索。
		return "", fmt.Errorf("未配置 SauceNAO API Key，无法使用 db 参数")
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.searchTimeout())
	defer cancel()

	raw, err := p.downloadImage(reqCtx, imageURL, 20*1024*1024)
	if err != nil {
		return "", fmt.Errorf("下载图片失败: %w", err)
	}

	proc, err := preprocessImage(raw, PreprocessOptions{UpscaleSmall: p.upscaleSmall()})
	if err != nil {
		return "", fmt.Errorf("图片处理失败: %w", err)
	}

	in := engineInput{ImageURL: imageURL, Data: proc.Data, Mime: proc.Mime}

	if customDB {
		// 参数指定了非默认数据库时仅提交 SauceNAO（其余引擎无 db 概念）
		results, err := p.saucenao.Search(reqCtx, p.apiKey(), db, in, maxResults)
		if err != nil {
			return "", fmt.Errorf("SauceNAO 检索失败: %w", err)
		}
		return p.finishToolSearch(results, nil, maxResults)
	}

	allResults, errReports := p.searchAll(reqCtx, in, maxResults, p.allEngines())
	return p.finishToolSearch(allResults, errReports, maxResults)
}

// finishToolSearch 对原始结果做合并、过滤、截断并格式化为工具返回文本。
func (p *Plugin) finishToolSearch(allResults []SearchResult, errReports []string, maxResults int) (string, error) {
	merged := mergeResults(allResults, p.similarityThreshold())
	results := pickResults(merged, maxResults)

	if len(results) == 0 {
		msg := "未找到匹配结果"
		if len(errReports) > 0 {
			msg += "\n\n（引擎异常：\n" + strings.Join(errReports, "\n") + "）"
		}
		return msg, nil
	}
	return p.formatResults(results, errReports, p.reportErrors()), nil
}

// firstStringArg 提取字符串参数，兼容 JSON Schema 数值/字符串混合类型。
func firstStringArg(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
