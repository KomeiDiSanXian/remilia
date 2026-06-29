// Package starrail 提供星穹铁道玩家角色展柜查询功能。
//
// 命令: /starrail showcase <uid>
// AI 工具: query_hsr_showcase
// AI 技能: hsr_query
package starrail

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

const cardWidthHSR = 600

var (
	hsrTextPrimary   = color.RGBA{R: 230, G: 225, B: 240, A: 255}
	hsrTextSecondary = color.RGBA{R: 175, G: 170, B: 190, A: 255}
	hsrAccent        = color.RGBA{R: 200, G: 170, B: 100, A: 255}
	hsrBgDark        = color.RGBA{R: 22, G: 20, B: 35, A: 255}
	hsrBgCard        = color.RGBA{R: 38, G: 35, B: 52, A: 255} //nolint:unused
	hsrDivider       = color.RGBA{R: 50, G: 45, B: 65, A: 255}
)

type hsrPlugin struct {
	probes []*health.APIProbe
	log    plugin.Logger
}

// New 创建星穹铁道角色展柜查询插件的 Descriptor。
//
// 命令:
//   - /starrail showcase <uid>
//
// AI:
//   - query_hsr_showcase(uid) → 展柜文本
//   - hsr_query — 星铁查询技能
func New() *plugin.Descriptor {
	p := &hsrPlugin{}
	return &plugin.Descriptor{
		Name:    "starrail",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "星穹铁道角色展柜查询（通过 mihomo.me）",
			Category:    "工具",
			Tags:        []string{"星穹铁道", "星铁", "HSR", "mihomo", "展柜"},
			HelpText: `星穹铁道角色展柜查询插件

用法：
  /starrail showcase <uid>  — 查看开拓者角色展柜`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			probe := health.NewAPIProbe("mihomo-me", "https://api.mihomo.me", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{probe}
			ctx.Spawn(func(runCtx context.Context) {
				probe.StartBackground(runCtx, 1*time.Minute)
			})

			hsrDef := command.NewDef("starrail").Description("星穹铁道角色展柜查询").
				SubCommand(command.NewDef("showcase").Description("查看角色展柜").Build()).
				Example("/starrail showcase 123456789").Build()
			ctx.OnCommandDefWith("", "/starrail", hsrDef, p.handleHSR, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

func (p *hsrPlugin) handleHSR(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) < 2 {
		ctx.ReplyError("用法: /starrail showcase <uid>"); return nil
	}

	switch parsed.Positional[0] {
	case "showcase":
		return p.showcase(ctx, parsed.Positional[1])
	default:
		ctx.ReplyError("未知子命令，可用: showcase"); return nil
	}
}

func (p *hsrPlugin) showcase(ctx *eventctx.Context, uid string) error {
	showcase, err := FetchShowcase(ctx.Context(), uid)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("查询失败: %v", err); return nil)
	}

	png, imgErr := renderHSRShowcase(showcase)
	if imgErr != nil {
		ctx.ReplyText(formatHSRText(showcase); return nil)
	}

	if ctx.Reply(platform.ImageDataMessage(png, "starrail.png", "image/png")); err != nil {
		return err
	}
	return nil
}

func formatHSRText(s *HSRShowcase) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("★ %s  Lv.%d 均衡%d\n", s.Player.Nickname, s.Player.Level, s.Player.WorldLevel))
	if s.Player.Signature != "" {
		b.WriteString(fmt.Sprintf("  \"%s\"\n", s.Player.Signature))
	}

	for _, c := range s.Chars {
		b.WriteString(fmt.Sprintf("\n[%s] %s Lv.%d 星魂%d", c.Element, c.Name, c.Level, c.Eidolon))
		b.WriteString(fmt.Sprintf(" | %s", c.Path))
		if c.LightCone != nil {
			b.WriteString(fmt.Sprintf("\n  光锥: %s Lv.%d 叠影%d", c.LightCone.Name, c.LightCone.Level, c.LightCone.Rank))
		}
		if len(c.Skills) > 0 {
			ts := make([]string, len(c.Skills))
			for i, t := range c.Skills {
				ts[i] = fmt.Sprintf("%d", t)
			}
			b.WriteString(fmt.Sprintf("\n  行迹: %s", strings.Join(ts, "/")))
		}
		if len(c.RelicSets) > 0 {
			var sets []string
			for sn, cnt := range c.RelicSets {
				sets = append(sets, fmt.Sprintf("%s %d件", sn, cnt))
			}
			b.WriteString(fmt.Sprintf("\n  遗器: %s", strings.Join(sets, " | ")))
		}
	}
	return b.String()
}

func renderHSRShowcase(s *HSRShowcase) ([]byte, error) {
	canvas, err := textimage.NewCanvas(cardWidthHSR,
		textimage.WithCJKFont(),
		textimage.WithFontColor(hsrTextPrimary),
		textimage.WithLineHeight(1.6),
		textimage.WithBgColor(hsrBgDark),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText(fmt.Sprintf("★ %s  Lv.%d  均衡%d", s.Player.Nickname, s.Player.Level, s.Player.WorldLevel),
		textimage.WithFontSize(20),
		textimage.WithFontColor(hsrAccent),
		textimage.WithAlign(textimage.AlignCenter),
	)

	if s.Player.Signature != "" {
		canvas.AddSpacer(2)
		canvas.AddText(fmt.Sprintf("\"%s\"", s.Player.Signature),
			textimage.WithFontSize(12),
			textimage.WithFontColor(hsrTextSecondary),
			textimage.WithAlign(textimage.AlignCenter),
		)
	}

	canvas.AddSpacer(12)
	canvas.AddDivider(textimage.WithDividerColor(hsrDivider))

	for i, c := range s.Chars {
		if i > 0 {
			canvas.AddSpacer(8)
			canvas.AddDivider(textimage.WithDividerColor(hsrDivider))
		}
		canvas.AddSpacer(6)

		if c.IconImage != nil {
			canvas.AddImage(c.IconImage,
				textimage.WithImgWidth(48),
				textimage.WithImgAlign(textimage.AlignLeft),
			)
		}

		canvas.AddText(fmt.Sprintf("%s  %s  Lv.%d  星魂%d", c.Element, c.Name, c.Level, c.Eidolon),
			textimage.WithFontSize(16),
		)

		canvas.AddText(c.Path,
			textimage.WithFontSize(12),
			textimage.WithFontColor(hsrTextSecondary),
		)

		if c.LightCone != nil {
			canvas.AddText(fmt.Sprintf("光锥: %s Lv.%d 叠影%d", c.LightCone.Name, c.LightCone.Level, c.LightCone.Rank),
				textimage.WithFontSize(13),
				textimage.WithFontColor(hsrTextSecondary),
			)
		}

		if len(c.Skills) > 0 {
			ts := make([]string, len(c.Skills))
			for i, t := range c.Skills {
				ts[i] = fmt.Sprintf("%d", t)
			}
			canvas.AddText("行迹: "+strings.Join(ts, " / "),
				textimage.WithFontSize(13),
				textimage.WithFontColor(hsrTextSecondary),
			)
		}

		if len(c.RelicSets) > 0 {
			var sets []string
			for sn, cnt := range c.RelicSets {
				sets = append(sets, fmt.Sprintf("%s %d件", sn, cnt))
			}
			canvas.AddText("遗器: "+strings.Join(sets, " | "),
				textimage.WithFontSize(12),
				textimage.WithFontColor(hsrTextSecondary),
			)
		}
	}

	return canvas.ResultPNG()
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *hsrPlugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "query_hsr_showcase",
			Categories:  []string{"starrail", "hsr"},
			Description: "查询星穹铁道玩家的角色展柜，输入 UID 获取角色信息",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {
						Type:        "string",
						Description: "玩家的 UID（9 位数字）",
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
				return formatHSRText(showcase), nil
			},
		},
	}
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *hsrPlugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "hsr_query",
			Description: "星穹铁道角色展柜查询",
			Prompt: `你是一个星穹铁道信息查询助手。
使用 query_hsr_showcase 工具查询开拓者的角色展柜。
返回的信息包括：开拓者信息、角色列表、等级、星魂、光锥和行迹。请用简洁的中文回复。`,
			Tools: p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的健康探针。实现 health.CheckProvider。
func (p *hsrPlugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}
