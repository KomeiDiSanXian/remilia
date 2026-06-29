// Package dice 骰子引擎插件。
//
// 提供通用掷骰能力，作为 coc 和 dnd 插件的底层依赖。
// Setup 返回 *Service，通过 plugin.Service/dice.Servicer 供其他插件使用。
package dice

import (
	"context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// Plugin 骰子引擎插件实例。
type Plugin struct {
	svc *Service
	log plugin.Logger
}

func New() *plugin.Descriptor {
	p := &Plugin{svc: &Service{}}
	return &plugin.Descriptor{
		Name:    "dice",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "通用骰子引擎 — 支持 D100、D20、D6、暗骰、多重骰、取高取低等",
			Category:    "跑团",
			Tags:        []string{"跑团", "TRPG", "骰子", "Dice", "COC", "DND"},
			HelpText: `骰子引擎插件 — 支持多种掷骰语法

用法：
  /r <表达式>         通用掷骰，如 /r 2d20+5、/r 3d6^2（取高2）、/r 2d8v1（取低1）
  /d <面数> [数量]    简写掷骰，如 /d 100、/d 20 2
  /rh <表达式>        暗骰（结果仅自己可见）
  /roll               随机掷 D100`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			rDef := command.NewDef("r").Description("通用掷骰").
				Arg("expression", "骰子表达式，如 2d20+5、3d6^2（取高2）", true).
				Example("/r 2d20+5").Example("/r 3d6^2").Example("/r d100").Build()
	ctx.OnCommandDefWith("", "/r", rDef, p.handleRoll, eventctx.OnMentionedBotOrNoMentions())

	rollDef := command.NewDef("roll").Description("随机掷 1 颗 D100").Build()
	ctx.OnCommandDefWith("", "/roll", rollDef, p.handleSimpleRoll, eventctx.OnMentionedBotOrNoMentions())

	dDef := command.NewDef("d").Description("简写掷骰").
		Arg("sides", "骰子面数（可选，默认 100）", false).
		Arg("count", "骰子数量（可选，默认 1）", false).
		Example("/d").Example("/d 20").Example("/d 6 3").Build()
	ctx.OnCommandDefWith("", "/d", dDef, p.handleD, eventctx.OnMentionedBotOrNoMentions())

	rhDef := command.NewDef("rh").Description("暗骰（结果仅自己可见）").
		Arg("expression", "骰子表达式", true).
		Example("/rh 1d100").Example("/rh 3d6").Build()
	ctx.OnCommandDefWith("", "/rh", rhDef, p.handleHiddenRoll, eventctx.OnMentionedBotOrNoMentions())

			return p.svc, nil
		},
	}
}

// handleRoll 处理 /r 命令：解析表达式并执行掷骰。
func (p *Plugin) handleRoll(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("用法: /r <表达式>，如 /r 2d20+5"); return nil
	}
	expr := strings.Join(parsed.Positional, " ")
	return p.replyRoll(ctx, expr, false)
}

// handleSimpleRoll 处理 /roll 命令：掷 1 颗 D100。
func (p *Plugin) handleSimpleRoll(ctx *eventctx.Context) error {
	return p.replyRoll(ctx, "1d100", false)
}

// handleD 处理 /d 命令：按面数简写掷骰，默认 D100。
func (p *Plugin) handleD(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		return p.replyRoll(ctx, "1d100", false)
	}

	sides := 100
	count := 1
	if len(parsed.Positional) > 0 {
		fmt.Sscanf(parsed.Positional[0], "%d", &sides)
	}
	if len(parsed.Positional) > 1 {
		fmt.Sscanf(parsed.Positional[1], "%d", &count)
	}
	expr := fmt.Sprintf("%dd%d", count, sides)
	return p.replyRoll(ctx, expr, false)
}

// handleHiddenRoll 处理 /rh 命令：暗骰，结果仅掷骰者可见。
func (p *Plugin) handleHiddenRoll(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("用法: /rh <表达式>"); return nil
	}
	expr := strings.Join(parsed.Positional, " ")
	return p.replyRoll(ctx, expr, true)
}

// replyRoll 执行掷骰并回复消息。hidden=true 时添加暗骰标识。
func (p *Plugin) replyRoll(ctx *eventctx.Context, expr string, hidden bool) error {
	result, err := p.svc.Roll(expr)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("掷骰失败: %v", err); return nil)
	}

	prefix := "🎲 "
	if hidden {
		prefix = "🤫 暗骰: "
	}

	ctx.ReplyText(prefix + result.Raw); return nil
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "roll_dice",
			Categories:  []string{"dice", "rpg"},
			Description: "掷骰子。支持标准骰子表达式，如 2d20+5（2个20面骰+5）、3d6^2（3个6面骰取最高2个）、d100、2d8v1（2个8面骰取最低1个）。返回掷骰结果。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"expression": {
						Type:        "string",
						Description: "骰子表达式，如 \"2d20+5\"、\"d100\"、\"3d6^2\"",
					},
				},
				Required: []string{"expression"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				expr, _ := args["expression"].(string)
				if expr == "" {
					return "", fmt.Errorf("请提供骰子表达式")
				}
				result, err := p.svc.Roll(expr)
				if err != nil {
					return fmt.Sprintf("掷骰失败: %v", err), nil
				}
				return result.Raw, nil
			},
		},
	}
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "dice_master",
			Description: "骰子大师 — 解析和执行各种骰子表达式",
			Prompt: `你是一个跑团骰子大师。
当用户需要掷骰时，使用 roll_dice 工具。
支持的表达式格式：
  - NdM: N 个 M 面骰子（如 2d20）
  - NdM+K / NdM-K: 带加值/减值（如 1d20+5）
  - NdM^K: N 个中取最高 K 个（如 4d6^3）
  - NdMvK: N 个中取最低 K 个（如 2d8v1）
  - 可组合: 2d20+1d6+3

根据用户需求选择合适的表达式，并用流畅的中文告知结果。`,
			Tools: p.ListTools(),
		},
	}
}
