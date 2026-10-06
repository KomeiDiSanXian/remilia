package dealornodeal

import (
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// buttonPrefix 是按钮回调 ID 前缀，用于与其他插件的按钮区分。
const buttonPrefix = "dond:"

// buttons 构建当前阶段的控制按钮。
//
// 每个按钮同时带 ID 与 Command：
//   - 支持回调的平台（Discord/Telegram 等）用 ID 触发 EventKindInteraction；
//   - QQ 等平台使用指令按钮（Command），点击后把命令填入输入框，不产生回调。
//
// "选箱" / "开箱" 只填充命令（末尾留空格便于用户补箱号），不会自动发送。
func (p *Plugin) buttons(ctx *eventctx.Context, g *Game) []platform.Button {
	if !ctx.GetPlatformCapabilities().Has(platform.CapButtons) {
		return nil
	}
	board := platform.Button{
		ID: buttonPrefix + "board", Label: p.t(ctx, "dond.button.board"),
		Command: "/dond board", Style: platform.ButtonStyleSecondary, Row: 2,
	}
	rules := platform.Button{
		ID: buttonPrefix + "rules", Label: p.t(ctx, "dond.button.rules"),
		Command: "/dond rules", Style: platform.ButtonStyleSecondary, Row: 2,
	}

	switch g.Phase {
	case PhasePick:
		return []platform.Button{
			{
				ID: buttonPrefix + "pick", Label: p.t(ctx, "dond.button.pick"),
				Command: "/dond pick ", Style: platform.ButtonStylePrimary, Row: 1,
			},
			{
				ID: buttonPrefix + "pickrandom", Label: p.t(ctx, "dond.button.pick_random"),
				Command: "/dond pick random", Style: platform.ButtonStyleSecondary, Row: 1,
			},
			board, rules,
		}
	case PhaseOpening:
		return []platform.Button{
			{
				ID: buttonPrefix + "open", Label: p.t(ctx, "dond.button.open"),
				Command: "/dond open ", Style: platform.ButtonStylePrimary, Row: 1,
			},
			board, rules,
		}
	case PhaseOffer:
		btns := []platform.Button{
			{
				ID: buttonPrefix + "deal", Label: p.t(ctx, "dond.button.deal"),
				Command: "/dond deal", Style: platform.ButtonStylePrimary, Row: 1,
			},
			{
				ID: buttonPrefix + "nodeal", Label: p.t(ctx, "dond.button.nodeal"),
				Command: "/dond nodeal", Style: platform.ButtonStyleSecondary, Row: 1,
			},
			board, rules,
		}
		if g.Scope == ScopeGroup {
			btns = append(btns, platform.Button{
				ID: buttonPrefix + "resolve", Label: p.t(ctx, "dond.button.resolve"),
				Command: "/dond resolve", Style: platform.ButtonStyleSecondary, Row: 2,
			})
		}
		return btns
	case PhaseFinal:
		return []platform.Button{
			{
				ID: buttonPrefix + "swap", Label: p.t(ctx, "dond.button.swap"),
				Command: "/dond swap", Style: platform.ButtonStylePrimary, Row: 1,
			},
			{
				ID: buttonPrefix + "keep", Label: p.t(ctx, "dond.button.keep"),
				Command: "/dond keep", Style: platform.ButtonStyleSecondary, Row: 1,
			},
			board, rules,
		}
	default:
		return []platform.Button{
			{
				ID: buttonPrefix + "new", Label: p.t(ctx, "dond.button.new"),
				Command: "/dond", Style: platform.ButtonStylePrimary, Row: 1,
			},
			board, rules,
		}
	}
}

// handleInteraction 处理按钮回调事件（指令按钮不产生回调，直接走命令路径）。
func (p *Plugin) handleInteraction(ctx *eventctx.Context) error {
	ev := ctx.GetPlatformEvent()
	if ev == nil {
		return nil
	}
	id := platform.Content(ev)
	if !strings.HasPrefix(id, buttonPrefix) {
		return nil
	}
	switch strings.TrimPrefix(id, buttonPrefix) {
	case "new":
		return p.cmdNew(ctx, nil)
	case "pickrandom":
		return p.cmdPick(ctx, []string{"random"})
	case "pick":
		ctx.ReplyText(p.t(ctx, "dond.pick.prompt", map[string]any{"Cases": len(DefaultValues)}))
		return nil
	case "open":
		ctx.ReplyText(p.t(ctx, "dond.button.open_tip"))
		return nil
	case "deal":
		return p.cmdDeal(ctx)
	case "nodeal":
		return p.cmdNoDeal(ctx)
	case "resolve":
		return p.cmdResolve(ctx)
	case "swap":
		return p.cmdSwap(ctx)
	case "keep":
		return p.cmdKeep(ctx)
	case "board":
		return p.cmdBoard(ctx)
	case "rules":
		return p.cmdRules(ctx)
	}
	return nil
}
