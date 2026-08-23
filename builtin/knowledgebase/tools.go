package knowledgebase

import (
	"context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// ListTools 返回可供 AI 调用的知识库工具。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	if !p.cfg.Enabled {
		return nil // 未启用时不注册 AI 工具，避免 LLM 看到两个永远查不到的工具
	}
	return []ai.Tool{
		{
			Name:        "kb_search",
			Categories:  []string{"general"},
			Description: p.toolDescription(),
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"query": {Type: "string", Description: "检索问题或关键词，如 如何实现一个定时任务插件"},
					"limit": {Type: "integer", Description: "返回片段条数上限（默认 3，最大 5）"},
				},
				Required: []string{"query"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				query, _ := args["query"].(string)
				query = strings.TrimSpace(query)
				if query == "" {
					return "", fmt.Errorf("query 不能为空")
				}
				limit := p.cfg.MaxResults
				if v, ok := args["limit"].(float64); ok && v > 0 {
					limit = int(v)
				}
				if limit > p.cfg.MaxResults {
					limit = p.cfg.MaxResults
				}
				hits, err := p.Search(gctx, query, limit)
				if err != nil {
					return "", err
				}
				return FormatHits(query, hits, 800), nil
			},
		},
		{
			Name:        "kb_stats",
			Categories:  []string{"general"},
			Description: "查看本地知识库索引状态：已索引文档数、分块数、嵌入模型、最近构建时间。在回答涉及「知识库是否包含某内容」「检索是否可用」时使用",
			Parameters: ai.ToolParamSchema{
				Type:       "object",
				Properties: map[string]ai.ToolParamSchema{},
			},
			Execute: func(_ context.Context, _ map[string]any) (string, error) {
				return p.statsText(), nil
			},
		},
	}
}

// toolDescription 返回 kb_search 的工具描述。
// 配置了 tool_description 时使用自定义文本（描述知识库实际内容）；
// 否则使用通用描述并带上数据源目录，避免写死"项目文档"误导其他用途的用户。
func (p *Plugin) toolDescription() string {
	if p.cfg.ToolDescription != "" {
		return p.cfg.ToolDescription
	}
	if src := strings.TrimSpace(p.cfg.SourceDir); src != "" {
		return "在本地知识库（数据源：" + src + "）中检索与查询最相关的文档片段，" +
			"返回来源路径、标题、相关度与内容。当用户询问知识库/文档中相关内容、或需要核对文档中的说法时使用"
	}
	return "在本地知识库中检索与查询最相关的文档片段，返回来源路径、标题、相关度与内容。" +
		"当用户询问知识库/文档中相关内容、或需要核对文档中的说法时使用"
}

// statsText 生成知识库状态文本（kb_stats 工具与 /kb status 命令共用）。
func (p *Plugin) statsText() string {
	p.mu.RLock()
	chunks := len(p.index)
	model := p.model
	builtAt := p.builtAt
	p.mu.RUnlock()

	var b strings.Builder
	b.WriteString("📚 **知识库状态**\n\n")
	if p.store == nil {
		b.WriteString("  - 状态：未启用（enabled=false）\n")
		return b.String()
	}
	if !sourceDirExists(p.cfg.SourceDir) {
		b.WriteString("  - ⚠️ 数据源目录不存在：`" + p.cfg.SourceDir + "`（索引为空）\n")
	}
	files, err := p.store.SourceFiles()
	fileCount := len(files)
	if err != nil {
		b.WriteString("  - 状态：索引读取失败（" + err.Error() + "）\n")
		return b.String()
	}
	b.WriteString("  - 数据源：`" + p.cfg.SourceDir + "`\n")
	b.WriteString(fmt.Sprintf("  - 文档数：`%d`\n", fileCount))
	b.WriteString(fmt.Sprintf("  - 分块数：`%d`\n", chunks))
	if model != "" {
		b.WriteString("  - 嵌入模型：`" + model + "`\n")
	} else {
		b.WriteString("  - 嵌入模型：未配置（仅关键词检索）\n")
	}
	if p.building.Load() {
		b.WriteString("  - 状态：⏳ 正在重建索引\n")
	} else if !builtAt.IsZero() {
		b.WriteString("  - 最近构建：`" + builtAt.Format("2006-01-02 15:04:05") + "`\n")
	} else {
		b.WriteString("  - 最近构建：尚未构建\n")
	}
	if err := p.getLastErr(); err != nil {
		b.WriteString("  - 上次错误：`" + err.Error() + "`\n")
	}
	return b.String()
}
