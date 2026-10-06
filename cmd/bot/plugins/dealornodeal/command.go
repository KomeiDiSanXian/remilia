package dealornodeal

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleDond 是 /dond 命令入口。
func (p *Plugin) handleDond(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyText(p.t(ctx, "dond.help"))
		return nil
	}
	args := parsed.Positional
	sub := "new"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "new", "start":
		return p.cmdNew(ctx, parsed.Flags)
	case "pick", "p":
		return p.cmdPick(ctx, args[1:])
	case "open", "o":
		return p.cmdOpen(ctx, args[1:])
	case "deal", "d":
		return p.cmdDeal(ctx)
	case "nodeal", "no", "n":
		return p.cmdNoDeal(ctx)
	case "resolve", "settle":
		return p.cmdResolve(ctx)
	case "swap", "s":
		return p.cmdSwap(ctx)
	case "keep", "k":
		return p.cmdKeep(ctx)
	case "board", "b":
		return p.cmdBoard(ctx)
	case "rules":
		return p.cmdRules(ctx)
	case "quit", "q":
		return p.cmdQuit(ctx)
	case "lang", "language":
		return p.cmdLang(ctx, args[1:])
	case "help", "h":
		ctx.ReplyText(p.t(ctx, "dond.help"))
		return nil
	default:
		ctx.ReplyError(p.t(ctx, "dond.unknown_sub", map[string]any{"Sub": args[0]}))
		return nil
	}
}

// cmdRules 发送玩法说明。
func (p *Plugin) cmdRules(ctx *eventctx.Context) error {
	ctx.ReplyText(p.t(ctx, "dond.rules"))
	return nil
}

// cmdNew 开始一局；同一维度已有进行中的对局时提示并重绘。
func (p *Plugin) cmdNew(ctx *eventctx.Context, flags map[string]string) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	scope := p.cfg.DefaultScope
	if v := strings.TrimSpace(flags["scope"]); v != "" {
		s, ok := ParseScope(v)
		if !ok {
			ctx.ReplyError(p.t(ctx, "dond.start.bad_scope", map[string]any{"Scope": v}))
			return nil
		}
		scope = s
	}
	cases := p.cfg.Cases
	if v := strings.TrimSpace(flags["cases"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < MinCases || n > MaxCases {
			ctx.ReplyError(p.t(ctx, "dond.start.bad_cases", map[string]any{
				"Min": MinCases,
				"Max": MaxCases,
			}))
			return nil
		}
		cases = n
	}

	key := SessionKey(platformID, chatID, userID, scope)
	if g, ok := p.sessions.Get(key); ok && !g.Finished {
		p.replyStage(ctx, g, p.t(ctx, "dond.start.running"))
		return nil
	}

	g := NewGame(Options{
		Platform:  platformID,
		ChatID:    chatID,
		OwnerID:   userID,
		OwnerName: ctx.GetDisplayName(),
		Scope:     scope,
		Cases:     cases,
		Now:       timeNow(),
	})
	g.AddParticipant(userID, ctx.GetDisplayName())
	p.sessions.Put(key, g)

	notice := p.t(ctx, "dond.start.created_single", map[string]any{"Cases": g.Cases()})
	if scope == ScopeGroup {
		notice = p.t(ctx, "dond.start.created_group", map[string]any{"Cases": g.Cases()})
	}
	p.replyStage(ctx, g, notice)
	return nil
}

// cmdPick 选定自己的箱子。
func (p *Plugin) cmdPick(ctx *eventctx.Context, args []string) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		arg := ""
		if len(args) > 0 {
			arg = strings.TrimSpace(args[0])
		}
		if arg == "" {
			return p.t(ctx, "dond.pick.prompt", map[string]any{"Cases": g.Cases()}), true
		}
		g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())
		if strings.EqualFold(arg, "random") {
			if err := g.PickRandom(); err != nil {
				return p.errorText(ctx, err, g), false
			}
			return p.t(ctx, "dond.pick.random_done", map[string]any{
				"Case": g.OwnCaseNumber(),
				"Left": g.RemainingToOpen(),
			}), true
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			return p.errorText(ctx, ErrBadCase, g), false
		}
		if err := g.Pick(n); err != nil {
			return p.errorText(ctx, err, g), false
		}
		return p.t(ctx, "dond.pick.done", map[string]any{
			"Case": g.OwnCaseNumber(),
			"Left": g.RemainingToOpen(),
		}), true
	})
}

// cmdOpen 打开一个或多个箱子。支持 `/dond open 7` 与批量 `/dond open 1 23 15 16`。
func (p *Plugin) cmdOpen(ctx *eventctx.Context, args []string) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if len(args) == 0 {
			return p.t(ctx, "dond.open.prompt", map[string]any{"Left": g.RemainingToOpen()}), true
		}
		if len(args) == 1 {
			return p.openSingle(ctx, g, args[0])
		}
		return p.openBatch(ctx, g, args)
	})
}

// openSingle 处理单箱开箱，保留更精确的逐项提示。
func (p *Plugin) openSingle(ctx *eventctx.Context, g *Game, raw string) (string, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return p.errorText(ctx, ErrBadCase, g), false
	}
	if err := g.CheckOpen(n); err != nil {
		return p.openSkipNotice(ctx, g, n, err), false
	}
	if err := g.Open(n); err != nil {
		return p.errorText(ctx, err, g), false
	}
	g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())
	if g.Phase == PhaseOffer {
		return p.offerNotice(ctx, g), true
	}
	return p.t(ctx, "dond.open.done", map[string]any{
		"Case":  n,
		"Value": p.money(g.Values[n-1]),
		"Left":  g.RemainingToOpen(),
	}), true
}

// openBatch 依次打开多个箱子：本轮开满后自动停止，剩余箱号按「本轮已开满」报告。
// 非法 / 重复 / 自己的箱子只跳过并列出原因，不影响其余箱子照常打开。
func (p *Plugin) openBatch(ctx *eventctx.Context, g *Game, args []string) (string, bool) {
	var opened, skipped []string
	for _, raw := range args {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		n, err := strconv.Atoi(token)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s(%s)", token, p.t(ctx, "dond.open.skip.notnum")))
			continue
		}
		if err := g.CheckOpen(n); err != nil {
			skipped = append(skipped, fmt.Sprintf("%d(%s)", n, p.skipLabel(ctx, err)))
			continue
		}
		if err := g.Open(n); err != nil {
			skipped = append(skipped, fmt.Sprintf("%d(%s)", n, err.Error()))
			continue
		}
		opened = append(opened, fmt.Sprintf("%d(%s)", n, shortMoney(g.Values[n-1], p.cfg.Currency)))
	}
	if len(opened) == 0 {
		if len(skipped) == 0 {
			return p.t(ctx, "dond.open.phase"), true
		}
		return p.t(ctx, "dond.open.batch_skipped", map[string]any{
			"List": strings.Join(skipped, ", "),
		}), false
	}
	g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())

	key := "dond.open.batch"
	if g.Phase == PhaseOffer {
		key = "dond.open.batch_filled"
	}
	notice := p.t(ctx, key, map[string]any{
		"Count": len(opened),
		"List":  strings.Join(opened, ", "),
		"Left":  g.RemainingToOpen(),
	})
	if len(skipped) > 0 {
		notice += "\n" + p.t(ctx, "dond.open.batch_skipped", map[string]any{
			"List": strings.Join(skipped, ", "),
		})
	}
	if g.Phase == PhaseOffer {
		notice += "\n" + p.offerNotice(ctx, g)
	}
	return notice, true
}

// openSkipNotice 把单箱开箱失败的原因映射为本地化文案。
func (p *Plugin) openSkipNotice(ctx *eventctx.Context, g *Game, n int, err error) string {
	switch {
	case errors.Is(err, ErrCaseOpened):
		return p.t(ctx, "dond.open.opened", map[string]any{"Case": n})
	case errors.Is(err, ErrOwnCase):
		return p.t(ctx, "dond.open.own", map[string]any{"Case": n})
	case errors.Is(err, ErrBadCase):
		return p.t(ctx, "dond.open.bad", map[string]any{"Cases": g.Cases()})
	default:
		return p.t(ctx, "dond.open.phase")
	}
}

// skipLabel 返回批量开箱中被跳过原因的简短标签。
func (p *Plugin) skipLabel(ctx *eventctx.Context, err error) string {
	switch {
	case errors.Is(err, ErrCaseOpened):
		return p.t(ctx, "dond.open.skip.opened")
	case errors.Is(err, ErrOwnCase):
		return p.t(ctx, "dond.open.skip.own")
	case errors.Is(err, ErrBadCase):
		return p.t(ctx, "dond.open.skip.bad")
	case errors.Is(err, ErrPhase):
		return p.t(ctx, "dond.open.skip.round_full")
	default:
		return err.Error()
	}
}

// cmdDeal 单人局直接成交；群投票局记为「同意成交」票。
func (p *Plugin) cmdDeal(ctx *eventctx.Context) error {
	return p.voteOrAct(ctx, true)
}

// cmdNoDeal 单人局直接拒绝；群投票局记为「继续」票。
func (p *Plugin) cmdNoDeal(ctx *eventctx.Context) error {
	return p.voteOrAct(ctx, false)
}

// voteOrAct 统一处理成交/不成交：群维度投票，单人维度直接结算。
func (p *Plugin) voteOrAct(ctx *eventctx.Context, deal bool) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if g.Phase != PhaseOffer || !g.OfferReady {
			return p.t(ctx, "dond.open.phase"), true
		}
		if g.Scope != ScopeGroup {
			if deal {
				if err := g.Deal(); err != nil {
					return p.errorText(ctx, err, g), false
				}
				return p.t(ctx, "dond.deal.done", map[string]any{"Offer": p.money(g.Won)}), true
			}
			if err := g.NoDeal(); err != nil {
				return p.errorText(ctx, err, g), false
			}
			return p.nodealNotice(ctx, g), true
		}

		uid := ctx.GetUserID()
		g.AddParticipant(uid, ctx.GetDisplayName())
		resolved, dealWon := g.Vote(uid, deal)
		if !resolved {
			d, nd := g.VoteCounts()
			return p.t(ctx, "dond.offer.vote_cast", map[string]any{
				"Choice": p.choiceLabel(ctx, deal),
				"Deal":   d,
				"NoDeal": nd,
			}), true
		}
		if err := g.ContinueAfterVote(dealWon); err != nil {
			return p.errorText(ctx, err, g), false
		}
		if dealWon {
			return p.t(ctx, "dond.offer.resolved_deal", map[string]any{"Offer": p.money(g.Won)}), true
		}
		return p.t(ctx, "dond.offer.resolved_nodeal") + "\n" + p.nodealNotice(ctx, g), true
	})
}

// cmdResolve 在群投票局中提前结算当前报价。
func (p *Plugin) cmdResolve(ctx *eventctx.Context) error {
	userID := ctx.GetUserID()
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if g.Scope != ScopeGroup || g.Phase != PhaseOffer || !g.OfferReady {
			return p.t(ctx, "dond.open.phase"), true
		}
		if !canEndGame(userID, g) {
			return p.t(ctx, "dond.resolve.denied"), false
		}
		g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())
		deal := g.Resolve()
		if err := g.ContinueAfterVote(deal); err != nil {
			return p.errorText(ctx, err, g), false
		}
		if deal {
			return p.t(ctx, "dond.offer.resolved_deal", map[string]any{"Offer": p.money(g.Won)}), true
		}
		return p.t(ctx, "dond.offer.resolved_nodeal") + "\n" + p.nodealNotice(ctx, g), true
	})
}

// cmdSwap 最终抉择：交换另一个箱子并揭晓。
func (p *Plugin) cmdSwap(ctx *eventctx.Context) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if err := g.Swap(); err != nil {
			return p.errorText(ctx, err, g), false
		}
		return p.t(ctx, "dond.final.swap") + "\n" + p.resultText(ctx, g), true
	})
}

// cmdKeep 最终抉择：保留自己的箱子并揭晓。
func (p *Plugin) cmdKeep(ctx *eventctx.Context) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if err := g.Keep(); err != nil {
			return p.errorText(ctx, err, g), false
		}
		return p.t(ctx, "dond.final.keep") + "\n" + p.resultText(ctx, g), true
	})
}

// cmdBoard 重新发送当前阶段。
func (p *Plugin) cmdBoard(ctx *eventctx.Context) error {
	return p.mutate(ctx, func(g *Game) (string, bool) {
		return p.statusText(ctx, g), true
	})
}

// cmdQuit 放弃本局（仅发起者）。
func (p *Plugin) cmdQuit(ctx *eventctx.Context) error {
	userID := ctx.GetUserID()
	return p.mutate(ctx, func(g *Game) (string, bool) {
		if !canEndGame(userID, g) {
			return p.t(ctx, "dond.quit.denied"), false
		}
		g.Quit()
		return p.t(ctx, "dond.quit.done"), true
	})
}

// cmdLang 切换本会话语言。
func (p *Plugin) cmdLang(ctx *eventctx.Context, args []string) error {
	if len(args) == 0 {
		ctx.ReplyError(p.t(ctx, "dond.lang.usage", map[string]any{"Locales": localeList()}))
		return nil
	}
	loc := normalizeLocale(args[0])
	if !slices.Contains(SupportedLocales, loc) {
		ctx.ReplyError(p.t(ctx, "dond.lang.usage", map[string]any{"Locales": localeList()}))
		return nil
	}
	if p.i18n != nil {
		p.i18n.SetLocale(ctx, loc)
	}
	ctx.ReplySuccess(p.t(ctx, "dond.lang.done", map[string]any{"Locale": loc}))
	return nil
}

// mutate 在会话锁内查找并修改对局，锁外统一重绘阶段图或回报错误。
//
// fn 返回 (notice, changed)：changed=true 时以 notice 重绘阶段图，否则以
// notice 作为错误提示回复。fn 在持锁状态下执行，不应阻塞。
func (p *Plugin) mutate(ctx *eventctx.Context, fn func(*Game) (string, bool)) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	g, ok := p.sessions.Find(platformID, chatID, userID)
	if !ok {
		ctx.ReplyError(p.t(ctx, "dond.board.none"))
		return nil
	}
	var (
		notice  string
		changed bool
		denied  bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(gg *Game) {
		g = gg
		if !p.canAct(userID, gg) {
			notice = p.t(ctx, "dond.not_owner", map[string]any{"Owner": ownerLabel(gg)})
			denied = true
			return
		}
		gg.UpdatedAt = timeNow()
		notice, changed = fn(gg)
	})
	if !found {
		ctx.ReplyError(p.t(ctx, "dond.board.none"))
		return nil
	}
	if denied || changed {
		p.replyStage(ctx, g, notice)
		return nil
	}
	if notice == "" {
		notice = p.t(ctx, "dond.open.phase")
	}
	ctx.ReplyError(notice)
	return nil
}

// canAct 报告调用者是否有权操作对局：群维度人人可操作，单人维度仅发起者。
func (p *Plugin) canAct(userID string, g *Game) bool {
	if g.Scope == ScopeGroup {
		return true
	}
	return g.OwnerID == "" || g.OwnerID == userID
}

// canEndGame 报告调用者是否有权提前终止或结算：仅发起者。
func canEndGame(userID string, g *Game) bool {
	return g.OwnerID == "" || g.OwnerID == userID
}

// replyStage 渲染并发送当前阶段图片（失败时降级为文本），并按平台能力附加文案与按钮。
func (p *Plugin) replyStage(ctx *eventctx.Context, g *Game, notice string) {
	if notice == "" {
		notice = p.statusText(ctx, g)
	}
	view := p.stageViewFor(g)
	data, err := renderStage(view)
	if err != nil {
		if p.log != nil {
			p.log.Warnf("dealornodeal: 渲染阶段图失败: %v", err)
		}
		ctx.ReplyText(notice + "\n\n" + renderStageText(view))
		return
	}

	msg := platform.ImageDataMessage(data, "dond.png", "image/png")
	if btns := p.buttons(ctx, g); len(btns) > 0 {
		msg = msg.WithButtons(btns...)
	}
	caps := ctx.GetPlatformCapabilities()
	switch {
	case caps.Has(platform.CapMarkdown):
		msg.Markdown = notice
		ctx.Reply(msg)
	case caps.Has(platform.CapCaption):
		msg.Text = notice
		ctx.Reply(msg)
	default:
		ctx.Reply(msg)
		ctx.ReplyText(notice)
	}
}

// stageViewFor 把对局转换为渲染视图。
func (p *Plugin) stageViewFor(g *Game) stageView {
	return stageView{
		Phase:           g.Phase,
		Round:           g.Round + 1,
		Rounds:          len(g.Schedule),
		Values:          g.Values,
		Opened:          g.Opened,
		OwnCase:         g.OwnCase,
		Offer:           g.Offer,
		OfferReady:      g.OfferReady,
		Remaining:       len(g.Remaining()),
		ToOpenThisRound: g.RemainingToOpen(),
		Won:             g.Won,
		Dealt:           g.Dealt,
		Swapped:         g.Swapped,
		Currency:        p.cfg.Currency,
	}
}

// statusText 返回当前阶段的默认提示文案。
func (p *Plugin) statusText(ctx *eventctx.Context, g *Game) string {
	switch g.Phase {
	case PhasePick:
		return p.t(ctx, "dond.pick.prompt", map[string]any{"Cases": g.Cases()})
	case PhaseOpening:
		return p.t(ctx, "dond.open.prompt", map[string]any{"Left": g.RemainingToOpen()})
	case PhaseOffer:
		return p.offerNotice(ctx, g)
	case PhaseFinal:
		return p.t(ctx, "dond.final.prompt")
	default:
		return p.resultText(ctx, g)
	}
}

// offerNotice 组装报价阶段的文案。
func (p *Plugin) offerNotice(ctx *eventctx.Context, g *Game) string {
	base := p.t(ctx, "dond.offer.header", map[string]any{
		"Offer":  p.money(g.Offer),
		"Round":  g.Round + 1,
		"Rounds": len(g.Schedule),
	})
	hint := p.t(ctx, "dond.offer.hint_single")
	if g.Scope == ScopeGroup {
		hint = p.t(ctx, "dond.offer.hint_group")
	}
	if g.FinalOffer() {
		base += "\n" + p.t(ctx, "dond.offer.final")
	}
	return base + "\n" + hint
}

// nodealNotice 返回「不成交」后的进度提示。
func (p *Plugin) nodealNotice(ctx *eventctx.Context, g *Game) string {
	if g.Phase == PhaseFinal {
		return p.t(ctx, "dond.nodeal.final")
	}
	return p.t(ctx, "dond.nodeal.next", map[string]any{
		"Round": g.Round + 1,
		"Left":  g.RemainingToOpen(),
	})
}

// resultText 返回结束阶段的结算文案。
func (p *Plugin) resultText(ctx *eventctx.Context, g *Game) string {
	if g.Dealt {
		return p.t(ctx, "dond.result.dealt", map[string]any{
			"Amount": p.money(g.Won),
			"Own":    p.money(g.OwnValue()),
		})
	}
	return p.t(ctx, "dond.result.reveal", map[string]any{"Amount": p.money(g.Won)})
}

// errorText 把玩法错误映射为本地化文案。
func (p *Plugin) errorText(ctx *eventctx.Context, err error, g *Game) string {
	switch {
	case errors.Is(err, ErrBadCase):
		return p.t(ctx, "dond.open.bad", map[string]any{"Cases": g.Cases()})
	case errors.Is(err, ErrPhase):
		return p.t(ctx, "dond.open.phase")
	default:
		return p.t(ctx, "dond.error.generic", map[string]any{"Err": err.Error()})
	}
}

// money 用插件配置的货币前缀格式化金额。
func (p *Plugin) money(v int64) string {
	return money(v, p.cfg.Currency)
}

// choiceLabel 返回投票选项的展示名。
func (p *Plugin) choiceLabel(ctx *eventctx.Context, deal bool) string {
	if deal {
		return p.t(ctx, "dond.button.deal")
	}
	return p.t(ctx, "dond.button.nodeal")
}

// ownerLabel 返回发起者的展示名，缺名时回退短 ID。
func ownerLabel(g *Game) string {
	if g.OwnerName != "" {
		return g.OwnerName
	}
	return shortID(g.OwnerID)
}

// shortID 截短长 ID，避免展示过长。
func shortID(id string) string {
	if id == "" {
		return "?"
	}
	r := []rune(id)
	if len(r) <= 8 {
		return id
	}
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}

// sessionContextKeys 取平台/会话/用户标识，私聊回退用用户 ID 作为会话 ID。
func sessionContextKeys(ctx *eventctx.Context) (platformID, chatID, userID string) {
	chat := ctx.GetChatInfo()
	platformID = ctx.GetEventPlatform()
	userID = ctx.GetUserID()
	chatID = chat.ID
	if chatID == "" {
		chatID = userID
	}
	return platformID, chatID, userID
}

// normalizeLocale 把常见语言写法归一化到内置 locale。
func normalizeLocale(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "zh-cn", "cn", "zh-hans":
		return localeZH
	case "en", "en-us", "en-gb":
		return localeEN
	default:
		return strings.TrimSpace(s)
	}
}
