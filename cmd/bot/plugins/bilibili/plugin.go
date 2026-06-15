// Package bilibili 提供 Bilibili UP 主信息查询、直播状态查询和用户搜索功能。
//
// 命令: /bili user <uid/用户名>, /bili live <uid/用户名>, /bili search <关键词>
// AI 工具: search_bilibili_user, get_bilibili_user_info, get_bilibili_live_status
package bilibili

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

var _ health.CheckProvider = (*Plugin)(nil)

// Plugin Bilibili 查询插件实例。
type Plugin struct {
	client *biliClient
	probes []*health.APIProbe
	log    plugin.Logger
}

// New 创建 Bilibili 查询插件的 Descriptor。
//
// 命令:
//   - /bili user <uid/用户名>
//   - /bili live <uid/用户名>
//   - /bili search <关键词>
//
// AI:
//   - search_bilibili_user(keyword) → 搜索用户
//   - get_bilibili_user_info(uid)   → UP 主详情
//   - get_bilibili_live_status(uid) → 直播状态
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "bilibili",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "Bilibili UP 主信息、直播状态、用户搜索",
			Category:    "工具",
			Tags:        []string{"B站", "bilibili", "直播", "查询", "搜索"},
			HelpText: `B站查询 — 查询 UP 主信息、直播状态、按用户名搜索

用法：
  /bili user <uid/用户名>   查询 UP 主信息（支持直接输入用户名自动解析）
  /bili live <uid/用户名>   查询直播状态（支持直接输入用户名自动解析）
  /bili search <关键词>      按用户名搜索 UP 主

示例：
  /bili user 泠鸢yousa
  /bili live 282994
  /bili search 泠鸢`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.client = newBiliClient()

			biliProbe := health.NewAPIProbe("api.bilibili.com", "https://api.bilibili.com/x/web-interface/nav", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			liveProbe := health.NewAPIProbe("api.live.bilibili.com", "https://api.live.bilibili.com/room/v1/Room/getRoomInfoOld?mid=1", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			searchProbe := health.NewAPIProbe("search.bilibili.com", "https://api.bilibili.com/x/web-interface/search/type?search_type=bili_user&keyword=test", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{biliProbe, liveProbe, searchProbe}

			for _, pr := range p.probes {
				pr := pr
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			biliDef := command.NewDef("bili").Description("Bilibili UP 主信息、直播状态、用户搜索").
				SubCommand(command.NewDef("user").Description("查询 UP 主信息").Build()).
				SubCommand(command.NewDef("live").Description("查询直播状态").Build()).
				SubCommand(command.NewDef("search").Description("搜索 UP 主").Build()).
				Example("/bili user 泠鸢yousa").Example("/bili live 282994").Example("/bili search 泠鸢").Build()
			ctx.OnCommandDefWith("", "/bili", biliDef, p.handleBili)

			return p, nil
		},
	}
}

// handleBili 处理 /bili 命令。
func (p *Plugin) handleBili(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法：/bili user <uid/用户名> 或 /bili live <uid/用户名> 或 /bili search <关键词>")
	}

	subCmd := parsed.Positional[0]

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
	defer cancel()

	switch strings.ToLower(subCmd) {
	case "search", "s":
		if len(parsed.Positional) < 2 {
			return ctx.ReplyError("请输入搜索关键词，例如：/bili search 泠鸢")
		}
		keyword := strings.Join(parsed.Positional[1:], " ")
		results, _, err := p.client.SearchUser(reqCtx, keyword, 1)
		if err != nil {
			return ctx.ReplyText(fmt.Sprintf("[B站] 搜索失败: %v", err))
		}
		if len(results) == 0 {
			return ctx.ReplyText(fmt.Sprintf("未找到与「%s」相关的用户", keyword))
		}
		png, imgErr := renderUserSearchResults(results, keyword)
		if imgErr != nil {
			return ctx.ReplyText(formatSearchUserText(results))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "bilibili_search.png", "image/png"))
		return err

	case "user", "u":
		if len(parsed.Positional) < 2 {
			return ctx.ReplyError("请输入 UID 或用户名，例如：/bili user 泠鸢yousa")
		}
		input := parsed.Positional[1]
		mid, resolvedName, err := p.client.ResolveUID(reqCtx, input)
		if err != nil {
			return ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
		}
		user, rel, err := p.client.FetchUserInfo(reqCtx, mid)
		if err != nil {
			return ctx.ReplyText(fmt.Sprintf("[B站] %v\nUID: %d\n(数据获取失败: %v)", resolvedName, mid, err))
		}
		png, imgErr := renderUserCard(user, rel)
		if imgErr != nil {
			return ctx.ReplyText(formatBiliUserText(user, rel))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "bilibili_user.png", "image/png"))
		return err

	case "live", "l":
		if len(parsed.Positional) < 2 {
			return ctx.ReplyError("请输入 UID 或用户名，例如：/bili live 泠鸢yousa")
		}
		input := parsed.Positional[1]
		mid, _, err := p.client.ResolveUID(reqCtx, input)
		if err != nil {
			return ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
		}
		live, err := p.client.FetchLiveInfo(reqCtx, mid)
		if err != nil {
			return ctx.ReplyText(fmt.Sprintf("[B站] 查询直播状态失败: %v", err))
		}
		png, imgErr := renderLiveCard(live)
		if imgErr != nil {
			return ctx.ReplyText(formatLiveText(live))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "bilibili_live.png", "image/png"))
		return err

	default:
		return ctx.ReplyError("未知子命令，支持：user, live, search")
	}
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "search_bilibili_user",
			Categories:  []string{"bilibili"},
			Description: "搜索 Bilibili UP 主，通过用户名关键词查找，返回用户名、UID、粉丝数、等级等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"keyword": {Type: "string", Description: "搜索关键词，如 泠鸢、老番茄、LexBurner"},
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

				results, _, err := p.client.SearchUser(reqCtx, keyword, 1)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return fmt.Sprintf("未找到与「%s」相关的用户", keyword), nil
				}
				return formatSearchUserText(results), nil
			},
		},
		{
			Name:        "get_bilibili_user_info",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili UP 主信息，包含用户名、等级、粉丝数、签名等。支持 UID 数字或用户名搜索",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {Type: "string", Description: "UP 主的 UID 数字或用户名，如 282994 或 泠鸢yousa"},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("uid is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				mid, _, err := p.client.ResolveUID(reqCtx, uid)
				if err != nil {
					return "", fmt.Errorf("无法解析 UID: %w", err)
				}
				user, rel, err := p.client.FetchUserInfo(reqCtx, mid)
				if err != nil {
					return "", err
				}
				return formatBiliUserText(user, rel), nil
			},
		},
		{
			Name:        "get_bilibili_live_status",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili UP 主的直播状态，是否在直播、直播标题、观看人数等。支持 UID 数字或用户名搜索",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {Type: "string", Description: "UP 主的 UID 数字或用户名，如 282994 或 泠鸢yousa"},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("uid is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				mid, _, err := p.client.ResolveUID(reqCtx, uid)
				if err != nil {
					return "", fmt.Errorf("无法解析 UID: %w", err)
				}
				live, err := p.client.FetchLiveInfo(reqCtx, mid)
				if err != nil {
					return "", err
				}
				return formatLiveText(live), nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "bilibili_query",
			Description: "查询 Bilibili UP 主信息、直播状态、搜索用户",
			Prompt:      "你是一个 B站 助手。当用户询问 B 站 UP 主信息或直播状态时，使用 search_bilibili_user 搜索用户，再用 get_bilibili_user_info 或 get_bilibili_live_status 获取详情。三个工具都支持直接输入用户名而非 UID。然后以自然语言总结。",
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
