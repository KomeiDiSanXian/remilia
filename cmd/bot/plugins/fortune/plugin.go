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
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

var _ health.CheckProvider = (*Plugin)(nil)

// Plugin 占卜插件实例。
type Plugin struct {
	dataDir string             // 数据目录（图片缓存）
	probes  []*health.APIProbe // 健康探针列表
	log     plugin.Logger
	cache   *imageCache // 插件级别的图片缓存
}

// WithDataDir 设置占卜插件的数据目录。
func WithDataDir(path string) Option {
	return func(p *Plugin) { p.dataDir = path }
}

// Option 占卜插件配置函数类型。
type Option func(*Plugin)

// New 创建占卜插件的 Descriptor。
//
// 命令:
//   - /omikuji [番号] — 抽取御神签
//   - /tarot [数量]   — 塔罗牌占卜
//
// AI:
//   - draw_omikuji(number?) → 御神签结果
//   - draw_tarot(count: 1|3) → 塔罗占卜结果
//   - fortune_query — 占卜师技能
func New(opts ...Option) *plugin.Descriptor {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
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

			ghProbe := health.NewAPIProbe("github-raw", "https://raw.githubusercontent.com", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			stProbe := health.NewAPIProbe("sacred-texts", "https://www.sacred-texts.com", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{ghProbe, stProbe}

			for _, pr := range p.probes {
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			p.cache = newImageCache(p.dataDir)

			omikujiDef := command.NewDef("omikuji").Description("抽取御神签占卜运势").
				Arg("number", "签号 1-100（可选，不指定则随机）", false).
				Example("/omikuji").Example("/omikuji 42").Build()
			ctx.OnCommandDefWith("", "/omikuji", omikujiDef, p.handleOmikuji)

			tarotDef := command.NewDef("tarot").Description("塔罗牌占卜").
				Arg("count", "牌数 1 或 3（可选，默认 1）", false).
				Example("/tarot").Example("/tarot 3").Build()
			ctx.OnCommandDefWith("", "/tarot", tarotDef, p.handleTarot)

			return p, nil
		},
	}
}

// handleOmikuji 处理 /omikuji 命令。
// 先发送签文图片卡片，再发送中文解签文本。
//
// 图片渲染失败时只发送纯文本。
func (p *Plugin) handleOmikuji(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		return ctx.ReplyError("用法: /omikuji [番号], /omikuji = 随机")
	}

	number := 0
	if len(parsed.Positional) > 0 {
		n, parseErr := strconv.Atoi(parsed.Positional[0])
		if parseErr != nil || n < 1 || n > 100 {
			return ctx.ReplyError("番号需为 1-100 之间的数字")
		}
		number = n
	}

	slip := drawOmikuji(number)

	variant := pickOmikujiVariant()
	key := sensojiCacheKey(slip.Number, variant)
	url := sensojiImageURL(slip.Number, variant)
	bgImg, _ := p.cache.Get(ctx.Context(), key, url)

	png, renderErr := renderOmikujiCard(slip, bgImg)
	if renderErr != nil {
		return ctx.ReplyText(formatOmikujiText(slip))
	}

	if _, err := ctx.Reply(platform.ImageDataMessage(png, "omikuji.png", "image/png")); err != nil {
		return err
	}

	return ctx.ReplyText(formatOmikujiText(slip))
}

// handleTarot 处理 /tarot 命令。
// 每张牌发送一张图片卡片，最后发送综合文字解读。
func (p *Plugin) handleTarot(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		return ctx.ReplyError("用法: /tarot [数量], /tarot = 1张, /tarot 3 = 三张")
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
		cardImg, _ := p.cache.Get(ctx.Context(), "tarot_"+card.NameShort, card.ImageURL)

		png, renderErr := renderTarotCard(&reading, cardImg)
		if renderErr != nil {
			ctx.ReplyText(formatTarotText(readings[i : i+1]))
			continue
		}

		ctx.Reply(platform.ImageDataMessage(png, fmt.Sprintf("tarot_%d.png", i), "image/png"))
	}

	return ctx.ReplyText(formatTarotText(readings))
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
				slip := drawOmikuji(number)
				return formatOmikujiText(slip), nil
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
- 使用 draw_omikuji 抽取御神签，为用户解读签文含义、吉凶等级和运势建议
- 使用 draw_tarot 抽取塔罗牌，解读正位或逆位的牌意，并结合问题给出指引

以温暖、鼓励的语气回应，并给予实用的建议。`,
			Tools: p.ListTools(),
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
