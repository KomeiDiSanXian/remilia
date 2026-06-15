// Package anime 提供番剧时间表查询和番剧信息搜索功能。
//
// 命令: /anime season, /anime search <关键词>, /anime info <id>
// AI 工具: search_anime, get_anime_info
package anime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

var _ health.CheckProvider = (*Plugin)(nil)

// Plugin 番剧查询插件实例。
type Plugin struct {
	client *bangumiClient
	probes []*health.APIProbe
	log    plugin.Logger
}

// New 创建番剧查询插件的 Descriptor。
//
// 命令:
//   - /anime season         查看当季番剧时间表
//   - /anime search <关键词> 搜索番剧
//   - /anime info <id>      查看番剧详细信息
//
// AI:
//   - search_anime(keyword) → 搜索番剧
//   - get_anime_info(id)    → 番剧详情
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "anime",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "番剧时间表查询，当季放送日历和番剧信息搜索",
			Category:    "工具",
			Tags:        []string{"动漫", "番剧", "Bangumi", "查询"},
			HelpText: `番剧查询 — 当季番剧时间表和信息搜索

用法：
  /anime season        查看当季番剧时间表
  /anime search <关键词> 搜索番剧
  /anime info <id>     查看番剧详细信息

示例：
  /anime season
  /anime search 间谍过家家
  /anime info 123456`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.client = newBangumiClient("")

			bgmProbe := health.NewAPIProbe("api.bgm.tv", "https://api.bgm.tv/calendar", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{bgmProbe}

			for _, pr := range p.probes {
				pr := pr
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			ctx.OnCommand("", "/anime", p.handleAnime)

			return p, nil
		},
	}
}

// handleAnime 处理 /anime 命令。
func (p *Plugin) handleAnime(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法：/anime season 或 /anime search <关键词> 或 /anime info <id>")
	}

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
	defer cancel()

	subCmd := parsed.Positional[0]

	switch strings.ToLower(subCmd) {
	case "season", "s":
		entries, err := p.client.FetchCalendar(reqCtx)
		if err != nil {
			return ctx.ReplyError(fmt.Sprintf("获取番剧时间表失败: %v", err))
		}
		png, imgErr := renderCalendar(entries)
		if imgErr != nil {
			return ctx.ReplyError(fmt.Sprintf("渲染图片失败: %v", imgErr))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "anime_season.png", "image/png"))
		return err

	case "search", "q":
		if len(parsed.Positional) < 2 {
			return ctx.ReplyError("请输入搜索关键词，例如：/anime search 间谍过家家")
		}
		keyword := strings.Join(parsed.Positional[1:], " ")
		results, err := p.client.SearchSubjects(reqCtx, keyword, 8)
		if err != nil {
			return ctx.ReplyError(fmt.Sprintf("搜索失败: %v", err))
		}
		if len(results) == 0 {
			return ctx.ReplyText(fmt.Sprintf("未找到与「%s」相关的番剧", keyword))
		}
		png, imgErr := renderSearchResults(results, keyword)
		if imgErr != nil {
			return ctx.ReplyText(formatSearchText(results))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "anime_search.png", "image/png"))
		return err

	case "info", "i":
		if len(parsed.Positional) < 2 {
			return ctx.ReplyError("请输入番剧 ID，例如：/anime info 123456")
		}
		id, err := strconv.ParseInt(parsed.Positional[1], 10, 64)
		if err != nil {
			return ctx.ReplyError("ID 格式不正确")
		}
		sub, err := p.client.FetchSubject(reqCtx, id)
		if err != nil {
			return ctx.ReplyError(fmt.Sprintf("获取番剧信息失败: %v", err))
		}
		png, imgErr := renderAnimeCard(sub)
		if imgErr != nil {
			return ctx.ReplyText(formatAnimeText(sub))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "anime_info.png", "image/png"))
		return err

	default:
		return ctx.ReplyError("未知子命令，支持：season, search, info")
	}
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "search_anime",
			Categories:  []string{"anime"},
			Description: "搜索番剧信息，通过关键词查找动漫番剧",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"keyword": {Type: "string", Description: "搜索关键词，如 间谍过家家、鬼灭之刃"},
				},
				Required: []string{"keyword"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				keyword, _ := args["keyword"].(string)
				if keyword == "" {
					return "", fmt.Errorf("keyword is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				results, err := p.client.SearchSubjects(reqCtx, keyword, 5)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return fmt.Sprintf("未找到与「%s」相关的番剧", keyword), nil
				}
				return formatSearchText(results), nil
			},
		},
		{
			Name:        "get_anime_info",
			Categories:  []string{"anime"},
			Description: "获取指定番剧的详细信息，包含评分、集数、放送日期、简介等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"id": {Type: "string", Description: "番剧的 Bangumi ID，如 123456"},
				},
				Required: []string{"id"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				idStr, _ := args["id"].(string)
				if idStr == "" {
					return "", fmt.Errorf("id is required")
				}
				id, err := strconv.ParseInt(idStr, 10, 64)
				if err != nil {
					return "", fmt.Errorf("invalid id: %s", idStr)
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				sub, err := p.client.FetchSubject(reqCtx, id)
				if err != nil {
					return "", err
				}
				return formatAnimeText(sub), nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "anime_query",
			Description: "查询番剧信息、当季放送表",
			Prompt:      "你是一个动漫助手。当用户询问番剧信息时，使用 search_anime 搜索番剧或 get_anime_info 获取详情。当季番剧时间表可以通过 /anime season 命令查看。用自然语言回答用户的问题。",
			Tools:       p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的 API 健康探针。实现 health.CheckProvider。
func (p *Plugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}
