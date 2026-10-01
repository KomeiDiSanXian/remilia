package wordle

import (
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// buttonPrefix 是按钮回调 ID 前缀，用于与其他插件的按钮区分。
const buttonPrefix = "wordle:"

// buttons 构建控制按钮。
//
// 每个按钮同时带 ID 与 Command：
//   - 支持回调的平台（Discord/Telegram 等）用 ID 触发 EventKindInteraction；
//   - QQ 等平台使用指令按钮（Command），点击后把命令填入输入框，不产生回调。
//
// "提交猜测"只填充命令（"/wordle guess "，末尾空格便于用户直接补单词），
// 不会自动发送，避免发出空猜测。
func (p *Plugin) buttons(ctx *eventctx.Context, g *Game) []platform.Button {
	if !ctx.GetPlatformCapabilities().Has(platform.CapButtons) {
		return nil
	}
	if g.Finished {
		return []platform.Button{
			{
				ID:      buttonPrefix + "new",
				Label:   p.t(ctx, "wordle.button.new"),
				Command: "/wordle",
				Style:   platform.ButtonStylePrimary,
				Row:     1,
			},
			{
				ID:      buttonPrefix + "board",
				Label:   p.t(ctx, "wordle.button.board"),
				Command: "/wordle board",
				Style:   platform.ButtonStyleSecondary,
				Row:     1,
			},
		}
	}
	return []platform.Button{
		{
			ID:      buttonPrefix + "guess",
			Label:   p.t(ctx, "wordle.button.guess"),
			Command: "/wordle guess ",
			Style:   platform.ButtonStylePrimary,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "hint",
			Label:   p.t(ctx, "wordle.button.hint"),
			Command: "/wordle hint ",
			Style:   platform.ButtonStyleSecondary,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "giveup",
			Label:   p.t(ctx, "wordle.button.giveup"),
			Command: "/wordle giveup",
			Style:   platform.ButtonStyleDanger,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "board",
			Label:   p.t(ctx, "wordle.button.board"),
			Command: "/wordle board",
			Style:   platform.ButtonStyleSecondary,
			Row:     2,
		},
		{
			ID:      buttonPrefix + "rules",
			Label:   p.t(ctx, "wordle.button.rules"),
			Command: "/wordle rules",
			Style:   platform.ButtonStyleSecondary,
			Row:     2,
		},
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
	case "guess":
		length := p.cfg.DefaultLength
		platformID, chatID, userID := sessionContextKeys(ctx)
		if g, ok := p.sessions.Find(platformID, chatID, userID); ok {
			length = g.Length
		}
		ctx.ReplyText(p.t(ctx, "wordle.button.guess_tip", map[string]any{"Length": length}))
		return nil
	case "hint":
		return p.cmdHint(ctx, nil)
	case "giveup":
		return p.cmdGiveup(ctx)
	case "board":
		return p.cmdBoard(ctx)
	case "rules":
		return p.cmdRules(ctx)
	}
	return nil
}
