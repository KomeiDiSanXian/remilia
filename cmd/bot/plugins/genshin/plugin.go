// Package genshin 提供原神玩家角色展柜查询功能。
//
// 命令: /genshin showcase <uid>
// AI 工具: query_genshin_showcase
// AI 技能: genshin_query
package genshin

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

const cardWidthGS = 600

var (
	gsTextPrimary   = color.RGBA{R: 230, G: 225, B: 235, A: 255}
	gsTextSecondary = color.RGBA{R: 180, G: 175, B: 190, A: 255}
	gsAccent        = color.RGBA{R: 200, G: 160, B: 80, A: 255}
	gsBgDark        = color.RGBA{R: 28, G: 25, B: 35, A: 255}
	gsBgCard        = color.RGBA{R: 42, G: 38, B: 50, A: 255}
	gsDivider       = color.RGBA{R: 55, G: 50, B: 65, A: 255}
)

// gsPlugin 原神展柜查询插件实例。
type gsPlugin struct {
	probes []*health.APIProbe
	log    plugin.Logger
}

// New 创建原神角色展柜查询插件的 Descriptor。
//
// 命令:
//   - /genshin showcase <uid>
//
// AI:
//   - query_genshin_showcase(uid) → 展柜文本
//   - genshin_query — 原神查询技能
func New() *plugin.Descriptor {
	p := &gsPlugin{}
	return &plugin.Descriptor{
		Name:    "genshin",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "原神角色展柜查询（通过 Enka Network）",
			Category:    "工具",
			Tags:        []string{"原神", "Genshin", "Enka", "展柜"},
			HelpText: `原神角色展柜查询插件

用法：
  /genshin showcase <uid>  — 查看玩家角色展柜`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			probe := health.NewAPIProbe("enka-network", "https://enka.network", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{probe}
			ctx.Spawn(func(runCtx context.Context) {
				probe.StartBackground(runCtx, 1*time.Minute)
			})

			ctx.OnCommand("", "/genshin", p.handleGS)

			if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
				aiSvc.RegisterToolProvider(p)
				aiSvc.RegisterSkillProvider(p)
			}

			return p, nil
		},
	}
}

func (p *gsPlugin) handleGS(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) < 2 {
		return ctx.ReplyError("用法: /genshin showcase <uid>")
	}

	switch parsed.Positional[0] {
	case "showcase":
		return p.showcase(ctx, parsed.Positional[1])
	default:
		return ctx.ReplyError("未知子命令，可用: showcase")
	}
}

func (p *gsPlugin) showcase(ctx *eventctx.Context, uid string) error {
	showcase, err := FetchShowcase(ctx.Context(), uid)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("查询失败: %v", err))
	}

	var wg sync.WaitGroup
	for i := range showcase.Chars {
		if showcase.Chars[i].IconURL == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			img, e := fetchImage(ctx.Context(), url)
			if e == nil {
				showcase.Chars[idx].IconImage = img
			}
		}(i, showcase.Chars[i].IconURL)
	}
	wg.Wait()

	png, imgErr := renderGSShowcase(showcase)
	if imgErr != nil {
		return ctx.ReplyText(formatGSText(showcase))
	}

	if _, err := ctx.Reply(platform.ImageDataMessage(png, "genshin.png", "image/png")); err != nil {
		return err
	}
	return nil
}

func formatGSText(s *GenshinShowcase) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("★ %s  Lv.%d\n", s.Player.Nickname, s.Player.Level))
	if s.Player.Signature != "" {
		b.WriteString(fmt.Sprintf("  \"%s\"\n", s.Player.Signature))
	}
	b.WriteString(fmt.Sprintf("世界等级 %d | 成就 %d", s.Player.WorldLevel, s.Player.FinishAchievementNum))

	for _, c := range s.Chars {
		b.WriteString(fmt.Sprintf("\n\n[%s] %s Lv.%d C%d", c.Element, c.Name, c.Level, c.Constellation))
		if c.Weapon != nil {
			b.WriteString(fmt.Sprintf("\n  武器: %s Lv.%d 精炼%d", c.Weapon.Name, c.Weapon.Level, c.Weapon.Refinement))
		}
		if len(c.Talents) > 0 {
			ts := make([]string, len(c.Talents))
			for i, t := range c.Talents {
				ts[i] = fmt.Sprint(t)
			}
			b.WriteString(fmt.Sprintf("\n  天赋: %s", strings.Join(ts, "/")))
		}
		if len(c.Artifacts) > 0 {
			setCounts := map[string]int{}
			for _, a := range c.Artifacts {
				if a.SetName != "" {
					setCounts[a.SetName]++
				}
			}
			var sets []string
			for sn, cnt := range setCounts {
				sets = append(sets, fmt.Sprintf("%s %d件", sn, cnt))
			}
			if len(sets) > 0 {
				b.WriteString(fmt.Sprintf("\n  圣遗物: %s", strings.Join(sets, " | ")))
			}
		}
	}
	return b.String()
}

func renderGSShowcase(s *GenshinShowcase) ([]byte, error) {
	canvas, err := textimage.NewCanvas(cardWidthGS,
		textimage.WithCJKFont(),
		textimage.WithFontColor(gsTextPrimary),
		textimage.WithLineHeight(1.6),
		textimage.WithBgColor(gsBgDark),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText(fmt.Sprintf("★ %s  Lv.%d", s.Player.Nickname, s.Player.Level),
		textimage.WithFontSize(20),
		textimage.WithFontColor(gsAccent),
		textimage.WithAlign(textimage.AlignCenter),
	)

	if s.Player.Signature != "" {
		canvas.AddSpacer(2)
		canvas.AddText(fmt.Sprintf("\"%s\"", s.Player.Signature),
			textimage.WithFontSize(12),
			textimage.WithFontColor(gsTextSecondary),
			textimage.WithAlign(textimage.AlignCenter),
		)
	}

	canvas.AddSpacer(4)
	canvas.AddText(fmt.Sprintf("世界 %d  |  成就 %d", s.Player.WorldLevel, s.Player.FinishAchievementNum),
		textimage.WithFontSize(12),
		textimage.WithFontColor(gsTextSecondary),
		textimage.WithAlign(textimage.AlignCenter),
	)

	canvas.AddSpacer(12)
	canvas.AddDivider(textimage.WithDividerColor(gsDivider))

	for i, c := range s.Chars {
		if i > 0 {
			canvas.AddSpacer(8)
			canvas.AddDivider(textimage.WithDividerColor(gsDivider))
		}
		canvas.AddSpacer(6)

		if c.IconImage != nil {
			canvas.AddImage(c.IconImage,
				textimage.WithImgWidth(48),
				textimage.WithImgAlign(textimage.AlignLeft),
			)
		}

		canvas.AddText(fmt.Sprintf("%s  %s  Lv.%d  C%d", c.Element, c.Name, c.Level, c.Constellation),
			textimage.WithFontSize(16),
		)

		if c.Weapon != nil {
			canvas.AddText(fmt.Sprintf("⚔ %s Lv.%d 精炼%d", c.Weapon.Name, c.Weapon.Level, c.Weapon.Refinement),
				textimage.WithFontSize(13),
				textimage.WithFontColor(gsTextSecondary),
			)
		}

		if len(c.Talents) > 0 {
			ts := make([]string, len(c.Talents))
			for i, t := range c.Talents {
				ts[i] = fmt.Sprintf("%d", t)
			}
			canvas.AddText("天赋: "+strings.Join(ts, " / "),
				textimage.WithFontSize(13),
				textimage.WithFontColor(gsTextSecondary),
			)
		}

		if len(c.Artifacts) > 0 {
			setCounts := map[string]int{}
			for _, a := range c.Artifacts {
				if a.SetName != "" {
					setCounts[a.SetName]++
				}
			}
			var sets []string
			for sn, cnt := range setCounts {
				sets = append(sets, fmt.Sprintf("%s %d件", sn, cnt))
			}
			if len(sets) > 0 {
				canvas.AddText("圣遗物: "+strings.Join(sets, " | "),
					textimage.WithFontSize(12),
					textimage.WithFontColor(gsTextSecondary),
				)
			}
		}
	}

	return canvas.ResultPNG()
}

func fetchImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *gsPlugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "query_genshin_showcase",
			Categories:  []string{"genshin"},
			Description: "查询原神玩家的角色展柜，输入 UID 获取角色信息",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {
						Type:        "string",
						Description: "玩家的 UID（6-9 位数字）",
					},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("请提供 UID")
				}
				showcase, err := FetchShowcase(gctx, uid)
				if err != nil {
					return fmt.Sprintf("查询失败: %v", err), nil
				}
				return formatGSText(showcase), nil
			},
		},
	}
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *gsPlugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "genshin_query",
			Description: "原神角色展柜查询",
			Prompt: `你是一个原神信息查询助手。
使用 query_genshin_showcase 工具查询玩家的角色展柜。
返回玩家信息、角色列表、等级、命之座、武器和天赋。请用简洁的中文回复。`,
			Tools: p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的健康探针。实现 health.CheckProvider。
func (p *gsPlugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}
