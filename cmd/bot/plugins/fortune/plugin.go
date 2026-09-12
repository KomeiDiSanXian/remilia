// Package fortune 提供浅草寺御神签和塔罗牌占卜功能。
//
// 命令: /omikuji [番号], /tarot [数量]
// AI 工具: draw_omikuji, draw_tarot
// AI 技能: fortune_query
package fortune

import (
	"context"
	"fmt"
	"strconv"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// Plugin 占卜插件实例。
//
// 签纸扫描与塔罗牌面均编译进二进制，运行时不依赖任何外部图床。
type Plugin struct {
	log plugin.Logger
}

// New 创建占卜插件的 Descriptor。
//
// 命令:
//   - /omikuji [番号] — 抽取御神签
//   - /tarot [数量]   — 塔罗牌占卜
//
// AI:
//   - draw_omikuji(number?) → 御神签签号
//   - draw_tarot(count: 1|3) → 塔罗占卜结果
//   - fortune_query — 占卜师技能
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "fortune",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "御神签占卜与塔罗牌占卜",
			Category:    "娱乐",
			Tags:        []string{"占卜", "御神签", "塔罗", "运势"},
			HelpText: `占卜插件 — 浅草寺御神签与塔罗牌占卜

用法：
  /omikuji         随机抽一张御神签
  /omikuji <番号>  指定番号 (1-100)
  /tarot           抽一张塔罗牌
  /tarot 3         抽三张塔罗牌（过去·现在·未来）`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			omikujiDef := command.NewDef("omikuji").Description("抽取御神签占卜运势").
				Arg("number", "签号 1-100（可选，不指定则随机）", false).
				Example("/omikuji").Example("/omikuji 42").Build()
			ctx.OnCommandDefWith("", "/omikuji", omikujiDef, p.handleOmikuji, eventctx.OnMentionedBotOrNoMentions())

			tarotDef := command.NewDef("tarot").Description("塔罗牌占卜").
				Arg("count", "牌数 1 或 3（可选，默认 1）", false).
				Example("/tarot").Example("/tarot 3").Build()
			ctx.OnCommandDefWith("", "/tarot", tarotDef, p.handleTarot, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// handleOmikuji 处理 /omikuji 命令。
//
// 发一条消息：签纸两页（签文页与解签页）合成的图片，下方附中文解签
// （汉诗、签意、各项运势，以及出处说明与免责声明）。平台不支持
// Markdown 时降级为纯文本；图文无法同发时拆成两条。
func (p *Plugin) handleOmikuji(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyError("用法: /omikuji [番号], /omikuji = 随机")
		return nil
	}

	number := 0
	if len(parsed.Positional) > 0 {
		n, parseErr := strconv.Atoi(parsed.Positional[0])
		if parseErr != nil || n < 1 || n > 100 {
			ctx.ReplyError("番号需为 1-100 之间的数字")
			return nil
		}
		number = n
	}

	number = drawOmikuji(number)
	slip := omikujiSlip(number)

	card, renderErr := renderOmikujiCard(p.omikujiPages(number)...)
	if renderErr != nil {
		ctx.ReplyError("御神签素材不可用，请稍后重试")
		return nil
	}

	msg := platform.ImageDataMessage(card, fmt.Sprintf("omikuji_%03d.jpg", number), "image/jpeg")
	if slip == nil {
		ctx.Reply(msg)
		return nil
	}

	caps := ctx.GetPlatformCapabilities()
	switch {
	case caps.Has(platform.CapMarkdown):
		msg.Markdown = formatOmikujiMD(slip)
		ctx.Reply(msg)
	case caps.Has(platform.CapCaption):
		msg.Text = formatOmikujiText(slip)
		ctx.Reply(msg)
	default:
		ctx.Reply(msg)
		ctx.ReplyText(formatOmikujiText(slip))
	}
	return nil
}

// handleTarot 处理 /tarot 命令。
//
// 发一条消息：整副牌阵合成的图片，下方附位置含义、牌意与逐张解读，
// 以及牌面出处与免责声明。平台不支持 Markdown 时降级为纯文本；
// 牌阵图片渲染失败时只发文字。
func (p *Plugin) handleTarot(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyError(tarotUsage)
		return nil
	}

	count := 1
	if len(parsed.Positional) > 0 {
		n, parseErr := strconv.Atoi(parsed.Positional[0])
		if parseErr != nil || (n != 1 && n != 3) {
			ctx.ReplyError(tarotUsage)
			return nil
		}
		count = n
	}

	readings := drawTarot(count)
	positions := tarotPositionsFor(count)

	cols := make([]tarotColumn, len(readings))
	for i := range readings {
		cols[i] = tarotColumn{
			Reading:  readings[i],
			Face:     p.tarotImage(readings[i].Card.NameShort),
			Position: positionName(positions, i),
		}
	}

	text := formatTarotText(readings)
	card, renderErr := renderTarotSpread(cols)
	if renderErr != nil {
		p.errf("fortune: 塔罗牌阵渲染失败: %v", renderErr)
		ctx.ReplyText(text)
		return nil
	}

	msg := platform.ImageDataMessage(card, fmt.Sprintf("tarot_%d.jpg", count), "image/jpeg")
	caps := ctx.GetPlatformCapabilities()
	switch {
	case caps.Has(platform.CapMarkdown):
		msg.Markdown = formatTarotMD(readings)
		ctx.Reply(msg)
	case caps.Has(platform.CapCaption):
		msg.Text = text
		ctx.Reply(msg)
	default:
		ctx.Reply(msg)
		ctx.ReplyText(text)
	}
	return nil
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "draw_omikuji",
			Categories:  []string{"fortune"},
			Description: "抽取御神签（浅草寺观音签，共 100 番），返回该签实际的吉凶、漢詩与解签。可指定签号 1-100",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"number": {
						Type:        "integer",
						Description: "指定签号 1-100（可选，不指定则随机）",
					},
				},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				number := 0
				if n, ok := args["number"].(float64); ok {
					number = int(n)
				}
				slip := omikujiSlip(drawOmikuji(number))
				if slip == nil {
					return "", fmt.Errorf("fortune: 签号无效: %d", number)
				}
				return formatOmikujiText(slip) +
					"\n\n（以上为该签的固定签文，非随机生成；用户发送 /omikuji 可获取对应签纸图片。）", nil
			},
		},
		{
			Name:        "draw_tarot",
			Categories:  []string{"fortune"},
			Description: "抽取塔罗牌占卜（韦特塔罗 78 张，含正位与逆位）。返回牌阵中每张牌的位置含义、正逆位、牌意关键词与逐张解读",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"count": {
						Type:        "integer",
						Description: "牌数：1 或 3",
					},
				},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				count := 1
				if n, ok := args["count"].(float64); ok {
					count = int(n)
				}
				if count != 1 && count != 3 {
					count = 1
				}
				readings := drawTarot(count)
				return formatTarotText(readings) +
					"\n\n（以上牌面由内置牌库随机抽取；用户发送 /tarot 可获取对应牌阵图片。）", nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "fortune_query",
			Description: "运势占卜与解读",
			Prompt: `你是一个精通日本浅草寺御神签和塔罗牌的占卜师。
当用户询问运势或占卜时：
- 使用 draw_omikuji 抽取御神签。工具会返回该签的实际吉凶、漢詩与各项运势，
  请依据返回值解读，不要自行增减或杜撰签文内容
- 使用 draw_tarot 抽取塔罗牌。抽 3 张时位置依次是过去・现在・未来，
  请结合位置含义解读，不要只逐张复述牌意；逆位也不等于单纯的不吉，
  多表示这股力量受阻或尚未成熟

以温暖、鼓励的语气回应，并给予实用的建议。`,
			Tools: p.ListTools(),
		},
	}
}
