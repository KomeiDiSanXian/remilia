// Package websearch 提供通用网页搜索与网页抓取能力。
//
// 为 AI 补齐实时信息查询：模型知识有截止日期，"今天的热点新闻""某产品
// 最新版本"这类问题此前无工具可用。本插件提供：
//
//	/search <关键词>  — 通用网页搜索（标题+链接+摘要）
//	/fetch <URL>      — 抓取网页正文（SSRF 防护 + 大小限制）
//	AI 工具            — web_search(query, max_results?) / fetch_url(url)
//
// 搜索数据源：
//   - SearXNG（默认）：自建实例，免费无 key，JSON 输出（/search?format=json）
//   - Serper：Google 搜索 API（需 serper_api_key）
//
// 安全：fetch 仅允许 https 公网地址（含重定向逐跳校验）、响应大小上限、
// 正文截断；搜索为只读请求，无权限要求。
package websearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config websearch 插件配置（plugins.websearch 节）。
type Config struct {
	// Engine 搜索引擎："searxng"（默认，自建实例）或 "serper"（Google API）。
	Engine string
	// SearxngURL SearXNG 实例地址（如 http://127.0.0.1:8080）。
	SearxngURL string
	// SerperAPIKey Serper API Key（engine=serper 时必填）。
	SerperAPIKey string
	// Proxy 代理地址（如 http://127.0.0.1:7890）；空值沿用环境变量或直连。
	Proxy string
	// Timeout 单次搜索/抓取超时（默认 15s）。
	Timeout time.Duration
	// MaxResults 单次搜索返回的结果条数（默认 5）。
	MaxResults int
	// MaxFetchBytes 网页抓取响应大小上限（默认 2MB）。
	MaxFetchBytes int64
	// MaxFetchRunes 提取正文的最大字符数（默认 4000）。
	MaxFetchRunes int
}

// DefaultConfig websearch 默认配置。
var DefaultConfig = Config{
	Engine:        "searxng",
	SearxngURL:    "http://127.0.0.1:8080",
	Timeout:       15 * time.Second,
	MaxResults:    5,
	MaxFetchBytes: 2 << 20,
	MaxFetchRunes: 4000,
}

// Plugin websearch 插件实例。
type Plugin struct {
	cfg    *Config
	engine searchEngine
	client *http.Client
}

// New 创建 websearch 插件的 Descriptor。
func New() *plugin.Descriptor {
	p := &Plugin{cfg: &DefaultConfig}
	return &plugin.Descriptor{
		Name:    "websearch",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "通用网页搜索与网页抓取（为 AI 提供实时信息查询）",
			Category:    "工具",
			Tags:        []string{"搜索", "网页", "资讯", "AI"},
			HelpText: `通用网页搜索与网页抓取 — 为 AI 与用户提供实时信息

用法：
  /search <关键词>        — 网页搜索（标题+链接+摘要）
  /fetch <URL>            — 抓取网页正文（仅 https 公网地址）

示例：
  /search 今日热点新闻
  /fetch https://example.com/article

搜索数据源：
  - SearXNG（默认）：自建实例，免费无 key
  - Serper：Google 搜索 API（需配置 serper_api_key）

AI 可自动调用 web_search / fetch_url 工具。`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			cfg := loadConfig(ctx)

			if err := initWebTransport(cfg.Proxy); err != nil {
				return nil, fmt.Errorf("websearch: %w", err)
			}
			p.cfg = cfg
			p.client = newWebClient(cfg.Timeout)
			p.engine = newSearchEngine(cfg, p.client)

			searchDef := command.NewDef("search").Description("通用网页搜索").
				Arg("query", "搜索关键词", true).
				Example("/search 今日热点").Build()
			ctx.OnCommandDefWith("", "/search", searchDef, p.handleSearch, eventctx.OnMentionedBotOrNoMentions())

			fetchDef := command.NewDef("fetch").Description("抓取网页正文").
				Arg("url", "网页链接（https）", true).
				Example("/fetch https://example.com").Build()
			ctx.OnCommandDefWith("", "/fetch", fetchDef, p.handleFetch, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// loadConfig 从配置中读取设置，未配置时使用默认值。
func loadConfig(ctx *plugin.SetupContext) *Config {
	cfg := DefaultConfig
	if v := ctx.Config.GetString("engine", ""); v != "" {
		switch v {
		case "searxng", "serper":
			cfg.Engine = v
		default:
			ctx.Log.Warnf("无效的 websearch engine %q，使用 searxng", v)
		}
	}
	if v := ctx.Config.GetString("searxng_url", ""); v != "" {
		cfg.SearxngURL = strings.TrimRight(v, "/")
	}
	if v := ctx.Config.GetString("serper_api_key", ""); v != "" {
		cfg.SerperAPIKey = v
	}
	if v := ctx.Config.GetString("proxy", ""); v != "" {
		cfg.Proxy = v
	}
	if v := ctx.Config.GetDuration("timeout", 0); v > 0 {
		cfg.Timeout = v
	}
	if v := ctx.Config.GetInt("max_results", 0); v > 0 {
		cfg.MaxResults = v
	}
	if v := ctx.Config.GetInt("max_fetch_bytes", 0); v > 0 {
		cfg.MaxFetchBytes = int64(v)
	}
	if v := ctx.Config.GetInt("max_fetch_runes", 0); v > 0 {
		cfg.MaxFetchRunes = v
	}
	return &cfg
}

// handleSearch 处理 /search 命令。
func (p *Plugin) handleSearch(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("请输入搜索关键词，例如：/search 今日热点")
		return nil
	}
	query := strings.TrimSpace(strings.Join(parsed.Positional, " "))
	if query == "" {
		ctx.ReplyError("请输入搜索关键词，例如：/search 今日热点")
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.Timeout)
	defer cancel()
	results, err := p.engine.Search(reqCtx, query, p.cfg.MaxResults)
	if err != nil {
		ctx.ReplyError("搜索失败: " + err.Error())
		return nil
	}
	if len(results) == 0 {
		ctx.ReplyText("没有找到相关结果。")
		return nil
	}
	ctx.ReplyText(formatResults(query, results))
	return nil
}

// handleFetch 处理 /fetch 命令。
func (p *Plugin) handleFetch(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("请输入网页链接，例如：/fetch https://example.com")
		return nil
	}
	rawURL := strings.TrimSpace(parsed.Positional[0])

	reqCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.Timeout)
	defer cancel()
	text, err := fetchPage(reqCtx, p.client, rawURL, p.cfg.MaxFetchBytes, p.cfg.MaxFetchRunes)
	if err != nil {
		ctx.ReplyError("抓取失败: " + err.Error())
		return nil
	}
	if strings.TrimSpace(text) == "" {
		ctx.ReplyText("页面没有可提取的文本内容。")
		return nil
	}
	ctx.ReplyText("📄 **" + rawURL + "**\n\n" + text)
	return nil
}

// formatResults 将搜索结果格式化为文本。
func formatResults(query string, results []Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 **%s** 的搜索结果（%d 条）：\n\n", query, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. **%s**\n", i+1, r.Title)
		if r.URL != "" {
			fmt.Fprintf(&b, "   %s\n", r.URL)
		}
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", truncateRunes(r.Snippet, 150))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "web_search",
			Categories:  []string{"general"},
			Description: "通用网页搜索。查询实时信息、新闻、最新版本、人物事件等模型知识截止日期之后的内容时使用。返回标题、链接与摘要列表",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"query":       {Type: "string", Description: "搜索关键词，如 今日热点新闻"},
					"max_results": {Type: "integer", Description: "返回结果条数上限（默认 5，最大 10）"},
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
				if v, ok := args["max_results"].(float64); ok && v > 0 {
					limit = int(v)
				}
				if limit > 10 {
					limit = 10
				}
				reqCtx, cancel := context.WithTimeout(gctx, p.cfg.Timeout)
				defer cancel()
				results, err := p.engine.Search(reqCtx, query, limit)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return "没有找到相关结果。", nil
				}
				return formatResults(query, results), nil
			},
		},
		{
			Name:        "fetch_url",
			Categories:  []string{"general"},
			Description: "抓取指定网页（https 公网地址）的正文纯文本。读取文章、文档、公告全文时使用；对搜索结果中的链接补充阅读",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"url": {Type: "string", Description: "网页链接，必须是 https 公网地址"},
				},
				Required: []string{"url"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				rawURL, _ := args["url"].(string)
				rawURL = strings.TrimSpace(rawURL)
				if rawURL == "" {
					return "", fmt.Errorf("url 不能为空")
				}
				reqCtx, cancel := context.WithTimeout(gctx, p.cfg.Timeout)
				defer cancel()
				text, err := fetchPage(reqCtx, p.client, rawURL, p.cfg.MaxFetchBytes, p.cfg.MaxFetchRunes)
				if err != nil {
					return "", err
				}
				if strings.TrimSpace(text) == "" {
					return "页面没有可提取的文本内容。", nil
				}
				return "页面正文：" + text, nil
			},
		},
	}
}
