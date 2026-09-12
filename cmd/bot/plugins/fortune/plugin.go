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
// 只发一条消息：签纸两页（签文页与解签页）合成的图片。
// 番号、吉凶、漢詩与解签都由签纸本身承载，不再附加生成的解读文本。
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

	png, renderErr := renderOmikujiCard(p.omikujiPages(number)...)
	if renderErr != nil {
		ctx.ReplyError("御神签素材不可用，请稍后重试")
		return nil
	}

	ctx.Reply(platform.ImageDataMessage(png, fmt.Sprintf("omikuji_%03d.png", number), "image/png"))
	return nil
}

// handleTarot 处理 /tarot 命令。
// 每张牌发送一张图片卡片，最后发送综合文字解读。
func (p *Plugin) handleTarot(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyError("用法: /tarot [数量], /tarot = 1张, /tarot 3 = 三张")
		return nil
	}

	count := 1
	if len(parsed.Positional) > 0 {
		n, parseErr := strconv.Atoi(parsed.Positional[0])
		if parseErr == nil && (n == 1 || n == 3) {
			count = n
		}
	}

	readings := drawTarot(count)

	for i, reading := range readings {
		card := reading.Card
		cardImg := p.tarotImage(card.NameShort)

		png, renderErr := renderTarotCard(&reading, cardImg)
		if renderErr != nil {
			ctx.ReplyText(formatTarotText(readings[i : i+1]))
			continue
		}

		ctx.Reply(platform.ImageDataMessage(png, fmt.Sprintf("tarot_%d.png", i), "image/png"))
	}

	ctx.ReplyText(formatTarotText(readings))
	return nil
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "draw_omikuji",
			Categories:  []string{"fortune"},
			Description: "抽取御神签（浅草寺风）来占卜运势",
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
				drawn := drawOmikuji(number)
				return fmt.Sprintf("已抽取御神签第 %d 番（浅草寺百番观音签）。"+
					"签纸的番号、吉凶、漢詩与解签均印在签纸图片上；"+
					"本工具不返回签文内容，请勿自行杜撰签文或吉凶。", drawn), nil
			},
		},
		{
			Name:        "draw_tarot",
			Categories:  []string{"fortune"},
			Description: "抽取塔罗牌进行占卜，可抽 1 张或 3 张（过去·现在·未来）",
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
				return formatTarotText(readings), nil
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
- 使用 draw_omikuji 抽取御神签。该工具只返回签号，签文、吉凶与解签都印在签纸
  图片上，请引导用户查看签纸图片，不要自行杜撰签文或吉凶
- 使用 draw_tarot 抽取塔罗牌，解读正位或逆位的牌意，并结合问题给出指引

以温暖、鼓励的语气回应，并给予实用的建议。`,
			Tools: p.ListTools(),
		},
	}
}
