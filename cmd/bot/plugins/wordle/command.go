package wordle

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleWordle 是 /wordle 的总入口，按第一个位置参数路由到各子命令。
func (p *Plugin) handleWordle(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyText(p.t(ctx, "wordle.help"))
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
	case "guess", "g":
		return p.cmdGuess(ctx, args[1:])
	case "giveup", "surrender":
		return p.cmdGiveup(ctx)
	case "hint":
		return p.cmdHint(ctx, args[1:])
	case "board", "show":
		return p.cmdBoard(ctx)
	case "rules", "modes":
		return p.cmdRules(ctx)
	case "stats", "stat":
		return p.cmdStats(ctx)
	case "top", "rank":
		return p.cmdTop(ctx)
	case "lang", "language":
		return p.cmdLang(ctx, args[1:])
	case "help", "?":
		ctx.ReplyText(p.t(ctx, "wordle.help"))
		return nil
	default:
		ctx.ReplyError(p.t(ctx, "wordle.error.unknown_subcommand", map[string]any{"Sub": sub}))
		return nil
	}
}

// cmdNew 开始新局。
//
// flags 覆盖 --length/--tries/--daily/--scope 以及玩法开关
// --hard/--blitz/--chain/--hint-cost/--chaos/--race/--blind/--fog/--obscure/--gauntlet，
// 以及多谜底开关 --duet/--quad/--boards。
func (p *Plugin) cmdNew(ctx *eventctx.Context, flags map[string]string) error {
	platformID, chatID, userID := sessionContextKeys(ctx)

	// 显式指定的长度/次数优先；未指定时长度默认随机 4-7，次数按长度推导。
	explicitLength := 0
	if v := strings.TrimSpace(flags["length"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || !IsSupportedLength(n) {
			ctx.ReplyError(p.t(ctx, "wordle.start.bad_length",
				map[string]any{"Lengths": joinInts(SupportedLengths, "/")}))
			return nil
		}
		explicitLength = n
	}
	explicitTries := 0
	if v := strings.TrimSpace(flags["tries"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < minAttempts || n > maxAttempts {
			ctx.ReplyError(p.t(ctx, "wordle.start.bad_tries",
				map[string]any{"Min": minAttempts, "Max": maxAttempts}))
			return nil
		}
		explicitTries = n
	}
	scope := p.cfg.DefaultScope
	if v := strings.TrimSpace(flags["scope"]); v != "" {
		s, ok := ParseScope(v)
		if !ok {
			ctx.ReplyError(p.t(ctx, "wordle.start.bad_scope"))
			return nil
		}
		scope = s
	}
	mode := ModeRandom
	if flags["daily"] == "true" || (flags["daily"] == "" && p.cfg.DefaultDaily) {
		mode = ModeDaily
	}

	opts, errMsg := p.parseOptions(ctx, flags)
	if errMsg != "" {
		ctx.ReplyError(errMsg)
		return nil
	}
	rules := opts.Rules
	unlimited := flagOn(flags, "unlimited")
	if unlimited {
		if bad := unlimitedConflict(flags); bad != "" {
			ctx.ReplyError(p.t(ctx, "wordle.start.unlimited_conflict",
				map[string]any{"Flag": "--" + bad}))
			return nil
		}
		// 来自 default_rules 的冲突静默降级：无限机会下 gauntlet 与 hint-cost 失去意义。
		rules &^= RuleGauntlet | RuleHintCost
	}
	if mode == ModeDaily {
		// 每日题的词长由日期稳定派生、词库固定为常用池，保证所有玩家拿到同一道题；
		// 显式指定会破坏"可对答案"的前提，直接报错；来自 default_rules 的冲突静默降级。
		if explicitLength > 0 || flagOn(flags, "obscure") {
			ctx.ReplyError(p.t(ctx, "wordle.error.daily_fixed"))
			return nil
		}
		rules &^= RuleObscure
	}
	if rules.Has(RuleChain) && mode == ModeDaily {
		// 只有用户显式要求时才报错；来自 default_rules 的冲突静默降级。
		if flagOn(flags, "chain") {
			ctx.ReplyError(p.t(ctx, "wordle.error.chain_daily"))
			return nil
		}
		rules &^= RuleChain
	}
	if rules.Has(RuleRace) && scope != ScopeGroup {
		if flagOn(flags, "race") {
			ctx.ReplyError(p.t(ctx, "wordle.error.race_needs_group"))
			return nil
		}
		rules &^= RuleRace
	}

	var mods Modifier
	if rules.Has(RuleChaos) {
		mods = pickChaos(rules)
	}

	day := p.today()
	length := p.resolveLength(explicitLength, mode, day)
	tries := p.resolveTries(explicitTries, length, opts.Boards)
	if mods.Has(ModDouble) {
		// COSTx2 下每次猜测消耗 2 次机会：至少保留一次可猜，并把奇数上限补成偶数，
		// 否则最后 1 次机会永远凑不满一次猜测、白白浪费。
		tries = max(tries, 2)
		if tries%2 != 0 {
			tries = min(tries+1, maxAttempts)
		}
	}

	key := SessionKey(platformID, chatID, userID, scope)
	if g, ok := p.sessions.Get(key); ok && !g.Finished {
		p.replyBoard(ctx, g, p.t(ctx, "wordle.start.already_running"))
		return nil
	}

	lockKey := dailyLockKey(platformID, chatID, userID, scope)
	if mode == ModeDaily && p.store != nil {
		if d, err := p.store.dailyLockDay(lockKey); err == nil && d == day {
			ctx.ReplyError(p.t(ctx, "wordle.start.daily_used", map[string]any{"Day": day}))
			return nil
		}
	}

	bank, err := loadWordBank(length)
	if err != nil {
		ctx.ReplyError(p.t(ctx, "wordle.start.bank_unavailable", map[string]any{"Length": length}))
		return nil
	}
	now := timeNow()
	g := &Game{
		ID:            shortID(fmt.Sprintf("%d", now.UnixNano())),
		Platform:      platformID,
		ChatID:        chatID,
		OwnerID:       userID,
		OwnerName:     ctx.GetDisplayName(),
		Scope:         scope,
		Mode:          mode,
		Rules:         rules,
		Modifiers:     mods,
		BlitzWindow:   opts.Blitz,
		Length:        length,
		MaxAttempts:   tries,
		Unlimited:     unlimited,
		FogRows:       opts.FogRows,
		ColorFog:      clampCells(opts.ColorFog, length),
		DecayRows:     clampRows(opts.Decay, tries),
		UnknownCells:  clampCells(opts.Unknown, length),
		DecoyCells:    clampCells(opts.Decoy, length),
		DelayedRows:   clampRows(opts.Delayed, tries),
		CreatedAt:     now,
		UpdatedAt:     now,
		LinkStartedAt: now,
	}
	if rules.Has(RuleBlitz) {
		g.Deadline = now.Add(opts.Blitz)
	}
	if mode == ModeDaily {
		g.Answers = pickDailyAnswers(bank, rules, opts.Boards, day)
	} else {
		g.Answers = pickAnswers(bank, rules, mods, opts.Boards)
	}
	g.Solved = make([]bool, len(g.Answers))
	p.sessions.Put(key, g)
	if mode == ModeDaily && p.store != nil {
		if err := p.store.setDailyLock(lockKey, day); err != nil {
			p.log.Warnf("wordle: 写入每日锁失败: %v", err)
		}
	}

	startKey := "wordle.start.created"
	if unlimited {
		startKey = "wordle.start.created_unlimited"
	}
	p.replyBoard(ctx, g, p.t(ctx, startKey, map[string]any{
		"Length": length,
		"Tries":  triesLabel(unlimited, tries),
		"Mode":   p.modeLabel(ctx, mode),
		"Rules":  p.ruleSummary(ctx, g),
	}))
	return nil
}

// cmdGuess 提交一次猜测。words 为子命令之后的位置参数。
func (p *Plugin) cmdGuess(ctx *eventctx.Context, words []string) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	if len(words) == 0 {
		if g, ok := p.sessions.Find(platformID, chatID, userID); ok {
			ctx.ReplyError(p.t(ctx, "wordle.error.usage_guess", map[string]any{"Length": g.Length}))
		} else {
			ctx.ReplyError(p.t(ctx, "wordle.error.not_started"))
		}
		return nil
	}
	word := strings.ToLower(strings.Join(words, ""))

	var (
		game     *Game
		notice   string
		finished bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			return
		}
		now := timeNow()
		if g.Expired(now) {
			g.Finished, g.Won, finished = true, false, true
			g.UpdatedAt = now
			notice = p.t(ctx, "wordle.result.timeout", map[string]any{"Answer": g.AnswerList()})
			return
		}
		if g.OutOfAttempts() {
			// 提示经济可能先把次数耗尽：补一次失败结算。
			g.Finished, g.Won, finished = true, false, true
			g.UpdatedAt = now
			notice = p.t(ctx, "wordle.result.lose", map[string]any{"Answer": g.AnswerList()})
			return
		}
		if !isAlpha(word) {
			notice = p.t(ctx, "wordle.error.not_alpha")
			return
		}
		if len(word) != g.Length {
			notice = p.t(ctx, "wordle.error.length",
				map[string]any{"Length": g.Length, "Got": len([]rune(word))})
			return
		}
		bank, err := loadWordBank(g.Length)
		if err != nil {
			notice = p.t(ctx, "wordle.start.bank_unavailable", map[string]any{"Length": g.Length})
			return
		}
		if !bank.IsAllowed(word) {
			notice = p.t(ctx, "wordle.error.not_in_dict", map[string]any{"Word": word})
			return
		}
		if g.HasGuessed(word) {
			notice = p.t(ctx, "wordle.error.repeat", map[string]any{"Word": word})
			return
		}
		if g.Rules.Has(RuleHard) {
			if v := checkHardMode(g, []rune(word)); !v.OK() {
				if v.Position > 0 {
					notice = p.t(ctx, "wordle.error.hard_position", map[string]any{"Position": v.Position})
				} else {
					notice = p.t(ctx, "wordle.error.hard_letter",
						map[string]any{"Letter": strings.ToUpper(string(v.Letter))})
				}
				return
			}
		}

		// 记录各棋盘在本猜测前已解锁的位置，供抢分模式统计新增贡献。
		beforeBoards := make([]map[int]bool, g.BoardCount())
		for bi := range beforeBoards {
			beforeBoards[bi] = g.SolvedPositionsOf(bi)
		}
		// 多谜底模式下同一次猜测对每块棋盘各判定一次；已解出的棋盘也继续判定，
		// 让棋盘保留后续行（渲染时以 SOLVED 标注），无需为每块棋盘单独存序列。
		boards := make([][]Mark, g.BoardCount())
		for bi := range boards {
			// 盲猜只在渲染层降级黄色（见 displayStyles），这里保留真实判定，
			// 让胜负判定与困难模式校验都基于真值。
			marks := Evaluate(g.AnswerAt(bi), word)
			boards[bi] = marks
			if allCorrect(marks) && bi < len(g.Solved) {
				g.Solved[bi] = true
			}
		}
		g.Guesses = append(g.Guesses, Guess{Word: word, Marks: boards[0], Boards: boards})
		// 群维度共享棋盘时，每个合法猜测者都算作本局参与者，结算时各记一次。
		g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())
		g.Used += g.AttemptCost()
		g.UpdatedAt = now
		if g.Rules.Has(RuleBlitz) {
			g.Deadline = now.Add(g.BlitzWindow)
		}
		if g.Rules.Has(RuleRace) {
			// 多谜底时累加每块棋盘新解锁的绿色位置，避免只统计第一块棋盘。
			gain := 0
			for bi := range boards {
				gain += raceGain(beforeBoards[bi], boards[bi])
			}
			if g.AllSolved() {
				gain += raceWinBonus
			}
			g.AddScore(ctx.GetUserID(), gain)
		}

		switch {
		case g.AllSolved():
			g.Finished, g.Won, finished = true, true, true
			notice = p.winNotice(ctx, g)
		case g.OutOfAttempts() || g.Used >= g.MaxAttempts:
			g.Finished, g.Won, finished = true, false, true
			notice = p.t(ctx, "wordle.result.lose", map[string]any{"Answer": g.AnswerList()})
		}
	})

	if !found || game == nil {
		ctx.ReplyError(p.t(ctx, "wordle.error.not_started"))
		return nil
	}
	if !finished {
		switch {
		case notice != "":
			ctx.ReplyError(notice)
			return nil
		case game.Finished:
			ctx.ReplyError(p.t(ctx, "wordle.error.finished"))
			return nil
		}
	}
	if finished {
		p.finalize(game)
		if game.Won && game.Rules.Has(RuleChain) {
			if p.startChainLink(game) {
				notice = p.t(ctx, "wordle.chain.advanced", map[string]any{
					"Chain": game.ChainWins,
					"Tries": triesLabel(game.Unlimited, game.MaxAttempts),
				})
			}
		}
	}
	p.replyBoard(ctx, game, notice)
	return nil
}

// cmdGiveup 放弃当前对局并公布答案。
func (p *Plugin) cmdGiveup(ctx *eventctx.Context) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	var (
		game   *Game
		gaveUp bool
		denied bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			return
		}
		// 群维度是共享棋盘：放弃会让全群一起记负，因此只允许发起者终止，
		// 避免路人一句话把别人的对局与战绩一起毁掉。
		if !canEndGame(ctx, g) {
			denied = true
			return
		}
		g.Finished = true
		g.Won = false
		g.UpdatedAt = timeNow()
		gaveUp = true
	})
	if !found || game == nil {
		ctx.ReplyError(p.t(ctx, "wordle.error.not_started"))
		return nil
	}
	if denied {
		ctx.ReplyError(p.t(ctx, "wordle.error.giveup_owner_only"))
		return nil
	}
	if !gaveUp {
		ctx.ReplyError(p.t(ctx, "wordle.error.finished"))
		return nil
	}
	p.finalize(game)
	p.replyBoard(ctx, game, p.t(ctx, "wordle.result.giveup",
		map[string]any{"Answer": game.AnswerList()}))
	return nil
}

// cmdHint 使用一次提示。
//
// 支持的提示类型（args[0]）：letter（默认，揭示一个字母位置）、
// exclude（排除一个不在谜底中的字母）、vowels（元音数量）、repeat（是否含重复字母）。
func (p *Plugin) cmdHint(ctx *eventctx.Context, args []string) error {
	kind := "letter"
	if len(args) > 0 {
		kind = strings.ToLower(strings.TrimSpace(args[0]))
	}
	if !validHintKind(kind) {
		ctx.ReplyError(p.t(ctx, "wordle.hint.unknown", map[string]any{"Kinds": hintKinds}))
		return nil
	}

	platformID, chatID, userID := sessionContextKeys(ctx)
	var (
		game   *Game
		notice string
		ended  bool
		denied bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			return
		}
		// 群维度共享提示额度：只允许发起者或已参与者使用，避免路人随手烧光额度。
		if !canUseGroupGame(ctx, g) {
			denied = true
			return
		}
		now := timeNow()
		if g.Expired(now) {
			g.Finished, g.Won, ended = true, false, true
			g.UpdatedAt = now
			notice = p.t(ctx, "wordle.result.timeout", map[string]any{"Answer": g.AnswerList()})
			return
		}
		if !g.HintsAllowed() {
			notice = p.t(ctx, "wordle.hint.disabled")
			return
		}
		// 未启用 --hint-cost 时限制免费提示次数，避免逐格揭示直接白嫖胜利；
		// 启用后每次提示都消耗机会，便不再设上限。
		hintCap := freeHintCap(g.Length, p.cfg.MaxFreeHints)
		if !g.CanUseHint(hintCap) {
			notice = p.t(ctx, "wordle.hint.limit", map[string]any{"Max": hintCap})
			return
		}
		text := p.hintOf(ctx, g, kind)
		if text == "" {
			notice = p.t(ctx, "wordle.hint.none")
			return
		}
		notice = text
		g.HintsUsed++
		g.UpdatedAt = now
		if g.Rules.Has(RuleBlitz) {
			g.Deadline = now.Add(g.BlitzWindow)
		}
		if g.Rules.Has(RuleHintCost) {
			g.Used++
			if g.OutOfAttempts() {
				g.Finished, g.Won, ended = true, false, true
				notice += "\n" + p.t(ctx, "wordle.result.lose",
					map[string]any{"Answer": g.AnswerList()})
			}
		}
	})
	if !found || game == nil {
		ctx.ReplyError(p.t(ctx, "wordle.error.not_started"))
		return nil
	}
	if denied {
		ctx.ReplyError(p.t(ctx, "wordle.error.hint_participant_only"))
		return nil
	}
	if game.Finished && !ended {
		ctx.ReplyError(p.t(ctx, "wordle.error.finished"))
		return nil
	}
	if ended {
		p.finalize(game)
	}
	p.replyBoard(ctx, game, notice)
	return nil
}

// hintOf 执行一次具体提示并返回文案；没有可用提示时返回空串（不消耗次数）。
func (p *Plugin) hintOf(ctx *eventctx.Context, g *Game, kind string) string {
	// 提示针对第一块棋盘：Revealed 的“位置”语义与 checkHardMode 的校验
	// 都建立在 board 0 之上，多谜底时保持这一约定最不容易出错。
	answer := []rune(g.AnswerAt(0))
	switch kind {
	case "exclude":
		used := make(map[rune]bool, len(g.Excluded)+len(answer))
		for _, r := range g.Excluded {
			used[r] = true
		}
		for _, r := range answer {
			used[r] = true
		}
		for r := 'a'; r <= 'z'; r++ {
			if used[r] {
				continue
			}
			g.Excluded = append(g.Excluded, r)
			return p.t(ctx, "wordle.hint.exclude", map[string]any{"Letter": strings.ToUpper(string(r))})
		}
		return ""
	case "vowels":
		n := 0
		for _, r := range answer {
			if strings.ContainsRune("aeiou", r) {
				n++
			}
		}
		return p.t(ctx, "wordle.hint.vowels", map[string]any{"Count": n})
	case "repeat":
		counts := make(map[rune]int, len(answer))
		for _, r := range answer {
			counts[r]++
		}
		extra := 0
		for _, c := range counts {
			if c > 1 {
				extra += c - 1
			}
		}
		if extra == 0 {
			return p.t(ctx, "wordle.hint.repeat.none")
		}
		return p.t(ctx, "wordle.hint.repeat.some", map[string]any{"Count": extra})
	default:
		return p.revealLetter(ctx, g)
	}
}

// revealLetter 揭示一个尚未猜中的字母位置；无可用位置时返回空串。
func (p *Plugin) revealLetter(ctx *eventctx.Context, g *Game) string {
	revealed := make(map[int]bool, len(g.Revealed))
	for _, i := range g.Revealed {
		revealed[i] = true
	}
	solved := g.SolvedPositions()

	pick := -1
	for i := 0; i < g.Length; i++ {
		if !revealed[i] && !solved[i] {
			pick = i
			break
		}
	}
	if pick < 0 {
		for i := 0; i < g.Length; i++ {
			if !revealed[i] {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return ""
	}

	g.Revealed = append(g.Revealed, pick)
	r := []rune(g.AnswerAt(0))
	return p.t(ctx, "wordle.hint.revealed", map[string]any{
		"Position": pick + 1,
		"Letter":   strings.ToUpper(string(r[pick])),
	})
}

// cmdBoard 重新发送当前棋盘。
func (p *Plugin) cmdBoard(ctx *eventctx.Context) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	g, ok := p.sessions.Find(platformID, chatID, userID)
	if !ok {
		ctx.ReplyError(p.t(ctx, "wordle.board.none"))
		return nil
	}
	timedOut := false
	p.sessions.UpdateFound(platformID, chatID, userID, func(gg *Game) {
		if gg.Finished || !gg.Expired(timeNow()) {
			return
		}
		gg.Finished, gg.Won, gg.UpdatedAt = true, false, timeNow()
		timedOut = true
	})
	if timedOut {
		p.finalize(g)
		p.replyBoard(ctx, g, p.t(ctx, "wordle.result.timeout",
			map[string]any{"Answer": g.AnswerList()}))
		return nil
	}
	p.replyBoard(ctx, g, "")
	return nil
}

// cmdRules 展示当前对局的玩法、修饰符与进度。
func (p *Plugin) cmdRules(ctx *eventctx.Context) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	g, ok := p.sessions.Find(platformID, chatID, userID)
	if !ok {
		ctx.ReplyText(p.t(ctx, "wordle.rules.none"))
		return nil
	}
	var b strings.Builder
	b.WriteString(p.t(ctx, "wordle.rules.title", map[string]any{"Rules": p.ruleSummary(ctx, g)}))
	b.WriteString("\n")
	b.WriteString(p.statusLine(ctx, g))
	if g.Rules.Has(RuleChain) {
		b.WriteString("\n")
		b.WriteString(p.t(ctx, "wordle.rules.chain", map[string]any{
			"Wins": g.ChainWins, "Index": g.ChainIndex + 1,
		}))
	}
	if sb := p.scoreboardLine(ctx, g); sb != "" {
		b.WriteString("\n")
		b.WriteString(sb)
	}
	b.WriteString("\n")
	b.WriteString(p.t(ctx, "wordle.rules.list", map[string]any{"List": strings.Join(p.ruleHelpLines(ctx), "\n")}))
	b.WriteString("\n")
	b.WriteString(p.t(ctx, "wordle.rules.modifier_list",
		map[string]any{"List": strings.Join(p.modifierHelpLines(ctx), "\n")}))
	ctx.ReplyText(b.String())
	return nil
}

// ruleHelpLines 返回可本地化的规则说明行。
func (p *Plugin) ruleHelpLines(ctx *eventctx.Context) []string {
	names := allRuleNames()
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("--%s   %s", name, p.t(ctx, "wordle.rule."+name)))
	}
	return out
}

// modifierHelpLines 返回可本地化的混沌修饰符说明行。
func (p *Plugin) modifierHelpLines(ctx *eventctx.Context) []string {
	names := allModNames()
	labels := allModLabels()
	out := make([]string, 0, len(names))
	for i, name := range names {
		out = append(out, fmt.Sprintf("%s   %s", labels[i], p.t(ctx, "wordle.mod."+name)))
	}
	return out
}

// cmdStats 展示统计；第一个 @ 提及（非机器人自身）会切换目标用户。
func (p *Plugin) cmdStats(ctx *eventctx.Context) error {
	if p.store == nil {
		ctx.ReplyError(p.t(ctx, "wordle.board.none"))
		return nil
	}
	uid := targetUserID(ctx)
	st, err := p.store.loadStat(uid)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("wordle: 读取统计失败: %v", err))
		return nil
	}
	var name string
	if n := strings.TrimSpace(st.Name); n != "" {
		name = n
	} else if uid == ctx.GetUserID() && ctx.GetDisplayName() != "" {
		name = ctx.GetDisplayName()
	} else {
		name = shortID(uid)
	}

	var b strings.Builder
	b.WriteString(p.t(ctx, "wordle.stats.title", map[string]any{"User": name}))
	b.WriteString("\n")
	b.WriteString(p.t(ctx, "wordle.stats.body", map[string]any{
		"Played": st.Played,
		"Won":    st.Won,
		"Rate":   fmt.Sprintf("%.1f", st.WinRate()),
		"Streak": st.Streak,
		"Best":   st.BestStreak,
	}))
	b.WriteString("\n")
	b.WriteString(p.t(ctx, "wordle.stats.dist.title"))
	b.WriteString("\n")
	b.WriteString(p.t(ctx, "wordle.stats.dist.fail", map[string]any{"Count": st.GuessDist[0]}))
	for i := 1; i < len(st.GuessDist); i++ {
		if st.GuessDist[i] == 0 {
			continue
		}
		b.WriteString("\n")
		b.WriteString(p.t(ctx, "wordle.stats.dist.row", map[string]any{
			"Attempts": i,
			"Count":    st.GuessDist[i],
		}))
	}
	ctx.ReplyText(b.String())
	return nil
}

// cmdTop 展示全局排行榜（按胜率，仅统计达到最少对局数的玩家）。
func (p *Plugin) cmdTop(ctx *eventctx.Context) error {
	if p.store == nil {
		ctx.ReplyText(p.t(ctx, "wordle.top.empty"))
		return nil
	}
	recs, err := p.store.leaderboard(topLimit, minLeaderboardGames)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("wordle: 读取排行榜失败: %v", err))
		return nil
	}
	if len(recs) == 0 {
		ctx.ReplyText(p.t(ctx, "wordle.top.empty", map[string]any{"Min": minLeaderboardGames}))
		return nil
	}
	var b strings.Builder
	b.WriteString(p.t(ctx, "wordle.top.title", map[string]any{"Min": minLeaderboardGames}))
	for i, r := range recs {
		b.WriteString("\n")
		b.WriteString(p.t(ctx, "wordle.top.row", map[string]any{
			"Rank":   i + 1,
			"User":   displayUser(r.Name, r.UserID),
			"Won":    r.Won,
			"Played": r.Played,
			"Rate":   fmt.Sprintf("%.0f", r.WinRate()),
		}))
	}
	ctx.ReplyText(b.String())
	return nil
}

// cmdLang 切换当前用户的语言偏好。
func (p *Plugin) cmdLang(ctx *eventctx.Context, args []string) error {
	if len(args) == 0 {
		ctx.ReplyError(p.t(ctx, "wordle.lang.usage", map[string]any{"Locales": localeList()}))
		return nil
	}
	loc := normalizeLocale(args[0])
	if !isSupportedLocale(loc) {
		ctx.ReplyError(p.t(ctx, "wordle.lang.unknown", map[string]any{
			"Locale":  args[0],
			"Locales": localeList(),
		}))
		return nil
	}
	if p.i18n != nil {
		p.i18n.SetLocale(ctx, loc)
	}
	ctx.ReplySuccess(p.t(ctx, "wordle.lang.set", map[string]any{"Locale": loc}))
	return nil
}

// finalize 结算一题：为每位参与者各记一次战绩，并写入一条对局历史。
//
// 群维度共享棋盘时参与者可能有多人（每个提交过合法猜测的人），胜负共享，
// 因此每人各记一次统计；只发起/只提示/只放弃的人不记战绩。历史表仍只写一条
// （归属发起者 OwnerID），避免把同一份猜测序列按参与人数重复存储。
//
// 连锁模式下每一题都单独结算，因此本函数可能在同一个 Game 上被多次调用。
func (p *Plugin) finalize(g *Game) {
	if p.store == nil {
		return
	}
	owner := g.OwnerID
	if owner == "" {
		owner = "unknown"
	}
	p.recordParticipantStats(g, owner, p.today())
	dur := max(g.UpdatedAt.Sub(g.LinkStart()).Milliseconds(), 0)
	if err := p.store.recordGame(g, owner, dur); err != nil {
		p.warnf("wordle: 写入对局记录失败: %v", err)
	}
}

// recordParticipantStats 为每位参与者各记一次战绩（胜负共享）。
//
// Participants 只包含提交过合法猜测的人；ownerID 是兜底参与者，参与者集合为
// 空时（例如发起后直接放弃、或历史数据）至少记一次，避免该局完全不入账。
func (p *Plugin) recordParticipantStats(g *Game, ownerID, dailyDay string) {
	// 无限机会对局默认只作为练习局：仍写历史记录，但不计入战绩与排行榜，
	// 否则会显著抬高胜率（可通过 count_unlimited 配置放开）。
	if g.Unlimited && !p.cfg.CountUnlimited {
		return
	}
	if len(g.Participants) == 0 {
		g.AddParticipant(ownerID, "")
	}
	for uid := range g.Participants {
		st, err := p.store.loadStat(uid)
		if err != nil {
			p.warnf("wordle: 读取统计失败: %v", err)
			continue
		}
		// 记录/刷新昵称，让排行榜与统计展示可读名字而非平台 ID。
		if name := strings.TrimSpace(g.Participants[uid]); name != "" {
			st.Name = name
		}
		st.Record(g.Won, len(g.Guesses))
		if g.Mode == ModeDaily {
			st.LastDailyDay = dailyDay
		}
		if err := p.store.saveStat(st); err != nil {
			p.warnf("wordle: 保存统计失败: %v", err)
		}
	}
}

// warnf 在 logger 可用时输出告警（测试环境可能未注入 logger）。
func (p *Plugin) warnf(format string, args ...any) {
	if p.log != nil {
		p.log.Warnf(format, args...)
	}
}

// replyBoard 渲染并发送棋盘（图片优先，失败时降级为 emoji 文本），并按平台能力附加文案与按钮。
func (p *Plugin) replyBoard(ctx *eventctx.Context, g *Game, notice string) {
	view := gameBoardView(g)
	text := p.statusText(ctx, g, notice)
	caps := ctx.GetPlatformCapabilities()

	img, err := renderBoard(view)
	if err != nil {
		if p.log != nil {
			p.log.Warnf("wordle: 渲染棋盘失败: %v", err)
		}
		ctx.ReplyText(text + "\n\n" + renderBoardText(view))
		return
	}

	msg := platform.ImageDataMessage(img, "wordle.png", "image/png")
	if btns := p.buttons(ctx, g); len(btns) > 0 {
		msg = msg.WithButtons(btns...)
	}
	switch {
	case caps.Has(platform.CapMarkdown):
		msg.Markdown = text
		ctx.Reply(msg)
	case caps.Has(platform.CapCaption):
		msg.Text = text
		ctx.Reply(msg)
	default:
		ctx.Reply(msg)
		ctx.ReplyText(text)
	}
}

// statusLine 返回本局状态行；无限机会用专门文案（不显示剩余次数）。
func (p *Plugin) statusLine(ctx *eventctx.Context, g *Game) string {
	if g.Unlimited {
		return p.t(ctx, "wordle.status.line_unlimited", map[string]any{
			"Length": g.Length,
			"Mode":   p.modeLabel(ctx, g.Mode),
		})
	}
	return p.t(ctx, "wordle.status.line", map[string]any{
		"Length":    g.Length,
		"Remaining": g.RemainingGuesses(),
		"Tries":     g.MaxGuesses(),
		"Mode":      p.modeLabel(ctx, g.Mode),
	})
}

// statusText 组合提示语、规则行与状态行。
func (p *Plugin) statusText(ctx *eventctx.Context, g *Game, notice string) string {
	status := p.statusLine(ctx, g)
	if extra := p.ruleLine(ctx, g); extra != "" {
		status = extra + "\n" + status
	}
	if sb := p.scoreboardLine(ctx, g); sb != "" {
		status = status + "\n" + sb
	}
	if notice == "" {
		return status
	}
	return notice + "\n" + status
}

// ruleLine 返回当前对局的规则/修饰符/连锁/倒计时一行摘要；经典对局返回空串。
func (p *Plugin) ruleLine(ctx *eventctx.Context, g *Game) string {
	// BLITZ 用带倒计时的动态徽标，避免与静态标签重复。
	parts := make([]string, 0, 4)
	for _, label := range g.Rules.Labels() {
		if label != "BLITZ" {
			parts = append(parts, label)
		}
	}
	if g.Rules.Has(RuleChain) {
		parts = append(parts, p.t(ctx, "wordle.chain.badge", map[string]any{"Wins": g.ChainWins}))
	}
	if g.Unlimited {
		parts = append(parts, "∞")
	}
	if g.Rules.Has(RuleBlitz) && !g.Finished && !g.Deadline.IsZero() {
		secs := max(int(g.Deadline.Sub(timeNow()).Seconds()), 0)
		parts = append(parts, p.t(ctx, "wordle.blitz.badge", map[string]any{"Secs": secs}))
	}
	parts = append(parts, g.Modifiers.Labels()...)
	if len(parts) == 0 {
		return ""
	}
	return p.t(ctx, "wordle.rules.line", map[string]any{"Rules": strings.Join(parts, " · ")})
}

// ruleSummary 返回简短的规则摘要（用于开局提示）。
func (p *Plugin) ruleSummary(ctx *eventctx.Context, g *Game) string {
	parts := g.Rules.Labels()
	parts = append(parts, g.Modifiers.Labels()...)
	if len(parts) == 0 {
		return p.t(ctx, "wordle.rules.classic")
	}
	return strings.Join(parts, " · ")
}

// scoreboardLine 返回抢分模式的贡献榜；未启用或无人得分时返回空串。
func (p *Plugin) scoreboardLine(ctx *eventctx.Context, g *Game) string {
	if !g.Rules.Has(RuleRace) {
		return ""
	}
	board := g.Scoreboard()
	if len(board) == 0 {
		return ""
	}
	if len(board) > 3 {
		board = board[:3]
	}
	parts := make([]string, 0, len(board))
	for _, e := range board {
		name := e.Name
		if name == "" {
			name = shortID(e.UserID)
		}
		parts = append(parts, p.t(ctx, "wordle.race.entry", map[string]any{
			"User": name, "Score": e.Score,
		}))
	}
	return p.t(ctx, "wordle.race.line", map[string]any{"Entries": strings.Join(parts, "  ")})
}

func (p *Plugin) modeLabel(ctx *eventctx.Context, m Mode) string {
	if m == ModeDaily {
		return p.t(ctx, "wordle.mode.daily")
	}
	return p.t(ctx, "wordle.mode.random")
}

// sessionContextKeys 返回会话隔离所需的三元组，chatID 缺失时回退为用户 ID。
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

// dailyLockKey 每日题的锁定维度：用户维度按用户，群维度按会话。
func dailyLockKey(platformID, chatID, userID string, scope Scope) string {
	if scope == ScopeUser {
		return "user:" + platformID + ":" + userID
	}
	return "chat:" + platformID + ":" + chatID
}

// canEndGame 报告调用者是否有权终止对局。
//
// 用户维度各玩各的，谁都能放弃自己的对局；群维度是共享棋盘，放弃会让所有
// 参与者一起记负，因此只允许发起者终止。
func canEndGame(ctx *eventctx.Context, g *Game) bool {
	return g.Scope != ScopeGroup || ctx.GetUserID() == g.OwnerID
}

// canUseGroupGame 报告调用者是否有权操作共享对局（主要是提示额度）。
//
// 群维度下提示额度是全群共享的，允许发起者与已参与者使用即可，避免路人
// 白嫖或恶意把额度烧光；用户维度不设限制。
func canUseGroupGame(ctx *eventctx.Context, g *Game) bool {
	if g.Scope != ScopeGroup {
		return true
	}
	uid := ctx.GetUserID()
	if uid == "" {
		return false
	}
	if uid == g.OwnerID {
		return true
	}
	_, ok := g.Participants[uid]
	return ok
}

// targetUserID 返回统计目标：优先消息中第一个非机器人自身的 @ 提及。
func targetUserID(ctx *eventctx.Context) string {
	if ev := ctx.GetPlatformEvent(); ev != nil {
		for _, m := range platform.GetMentions(ev) {
			if m.IsSelf || m.IsBot || m.ID == "" {
				continue
			}
			return m.ID
		}
	}
	return ctx.GetUserID()
}

func joinInts(vals []int, sep string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}

const (
	// minBlitzSeconds / maxBlitzSeconds 限时模式允许的单步时限范围。
	minBlitzSeconds = 10
	maxBlitzSeconds = 600
	// minFogRows / maxFogRows 迷雾模式可见的最近行数范围。
	minFogRows = 1
	maxFogRows = 4
	// minBoards / maxBoards 多谜底同屏的棋盘数量范围。
	minBoards = 2
	maxBoards = 4
	// minColorCells / maxColorCells 颜色预算/未知格/诱饵的每行格子数范围。
	minColorCells = 1
	maxColorCells = 7
	// minDecayRows / maxDecayRows 颜色衰减保留的行数范围。
	minDecayRows = 1
	maxDecayRows = 8
	// minDelayRows / maxDelayRows 延迟着色的行数范围。
	minDelayRows = 1
	maxDelayRows = 8
	// raceWinBonus 抢分模式下提交制胜一猜的额外加分。
	raceWinBonus = 3
	// topLimit 排行榜展示条数。
	topLimit = 10
	// minLeaderboardGames 上榜所需的最少对局数，避免"只打一两局全胜"霸榜。
	minLeaderboardGames = 10
	// hintKinds 是 /wordle hint 支持的提示类型（用于提示文案）。
	hintKinds = "letter / exclude / vowels / repeat"
)

// validHintKind 报告提示类型是否受支持。
func validHintKind(kind string) bool {
	switch kind {
	case "letter", "exclude", "vowels", "repeat":
		return true
	default:
		return false
	}
}

// flagOn 判断解析出的布尔开关是否开启（无值/true/1/yes/on 都视为开启）。
func flagOn(flags map[string]string, name string) bool {
	v, ok := flags[name]
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// gameOptions 是从命令行开关解析出的开局选项。
type gameOptions struct {
	Rules   RuleSet
	Blitz   time.Duration
	FogRows int
	Boards  int
	// 颜色玩法参数（0 = 未启用）。
	ColorFog int
	Decay    int
	Unknown  int
	Decoy    int
	Delayed  int
}

// parseOptions 解析玩法开关与棋盘数量；出错时第二个返回值为错误文案（非空）。
func (p *Plugin) parseOptions(ctx *eventctx.Context, flags map[string]string) (gameOptions, string) {
	opts := gameOptions{Rules: p.cfg.DefaultRules, Blitz: p.cfg.BlitzWindow, Boards: 1}
	rules := opts.Rules
	for flag, rule := range map[string]RuleSet{
		"hard":         RuleHard,
		"chain":        RuleChain,
		"hint-cost":    RuleHintCost,
		"chaos":        RuleChaos,
		"race":         RuleRace,
		"blind":        RuleBlind,
		"obscure":      RuleObscure,
		"invert":       RuleInvert,
		"hit-only":     RuleHitOnly,
		"hidden-key":   RuleHiddenKey,
		"glitch":       RuleGlitch,
		"mole":         RuleMole,
		"swap-meaning": RuleSwapMeaning,
		"near":         RuleNear,
		"repeat":       RuleRepeat,
		"score-color":  RuleScoreColor,
	} {
		if flagOn(flags, flag) {
			rules |= rule
		}
	}
	if flagOn(flags, "gauntlet") {
		rules |= RuleChain | RuleGauntlet
	}

	window := opts.Blitz
	if v, ok := flags["blitz"]; ok {
		rules |= RuleBlitz
		if s := strings.TrimSpace(v); s != "" && !strings.EqualFold(s, "true") {
			n, err := strconv.Atoi(s)
			if err != nil || n < minBlitzSeconds || n > maxBlitzSeconds {
				return opts, p.t(ctx, "wordle.start.bad_blitz",
					map[string]any{"Min": minBlitzSeconds, "Max": maxBlitzSeconds})
			}
			window = time.Duration(n) * time.Second
		}
	}
	if window <= 0 {
		window = time.Minute
	}

	fog := 0
	if v, ok := flags["fog"]; ok {
		rules |= RuleFog
		fog = 1
		if s := strings.TrimSpace(v); s != "" && !strings.EqualFold(s, "true") {
			n, err := strconv.Atoi(s)
			if err != nil || n < minFogRows || n > maxFogRows {
				return opts, p.t(ctx, "wordle.start.bad_fog",
					map[string]any{"Min": minFogRows, "Max": maxFogRows})
			}
			fog = n
		}
	}

	boards := 1
	if flagOn(flags, "duet") {
		boards = 2
	}
	if flagOn(flags, "quad") {
		boards = 4
	}
	if v, ok := flags["boards"]; ok {
		s := strings.TrimSpace(v)
		switch {
		case s == "" || strings.EqualFold(s, "true"):
			if boards == 1 {
				boards = 2
			}
		default:
			n, err := strconv.Atoi(s)
			if err != nil || n < minBoards || n > maxBoards {
				return opts, p.t(ctx, "wordle.start.bad_boards",
					map[string]any{"Min": minBoards, "Max": maxBoards})
			}
			boards = n
		}
	}

	opts.Rules = rules
	opts.Blitz = window
	opts.FogRows = fog
	opts.Boards = boards
	var errMsg string
	if opts.ColorFog, _, errMsg = p.parseCountFlag(ctx, flags, "colorfog", 2, minColorCells, maxColorCells); errMsg != "" {
		return opts, errMsg
	}
	if opts.ColorFog > 0 {
		rules |= RuleColorFog
	}
	if opts.Decay, _, errMsg = p.parseCountFlag(ctx, flags, "decay", 2, minDecayRows, maxDecayRows); errMsg != "" {
		return opts, errMsg
	}
	if opts.Decay > 0 {
		rules |= RuleDecay
	}
	if opts.Unknown, _, errMsg = p.parseCountFlag(ctx, flags, "unknown", 1, minColorCells, maxColorCells); errMsg != "" {
		return opts, errMsg
	}
	if opts.Unknown > 0 {
		rules |= RuleUnknown
	}
	if opts.Decoy, _, errMsg = p.parseCountFlag(ctx, flags, "decoy", 1, minColorCells, maxColorCells); errMsg != "" {
		return opts, errMsg
	}
	if opts.Decoy > 0 {
		rules |= RuleDecoy
	}
	if opts.Delayed, _, errMsg = p.parseCountFlag(ctx, flags, "delayed", 1, minDelayRows, maxDelayRows); errMsg != "" {
		return opts, errMsg
	}
	if opts.Delayed > 0 {
		rules |= RuleDelayed
	}
	opts.Rules = rules
	return opts, ""
}

// parseCountFlag 解析形如 --name[=N] 的计数开关。
// 未出现时返回 (0,false,"")；出现但越界时返回错误文案。
func (p *Plugin) parseCountFlag(ctx *eventctx.Context, flags map[string]string, name string, def, minV, maxV int) (int, bool, string) {
	v, ok := flags[name]
	if !ok {
		return 0, false, ""
	}
	n := def
	if s := strings.TrimSpace(v); s != "" && !strings.EqualFold(s, "true") {
		parsed, err := strconv.Atoi(s)
		if err != nil || parsed < minV || parsed > maxV {
			return 0, true, p.t(ctx, "wordle.start.bad_range", map[string]any{
				"Flag": name, "Min": minV, "Max": maxV,
			})
		}
		n = parsed
	}
	return n, true, ""
}

// resolveLength 决定本局单词长度：显式指定 > 每日题按日期稳定选取 > 配置默认 > 随机 4-7。
func (p *Plugin) resolveLength(explicit int, mode Mode, day string) int {
	if explicit > 0 {
		return explicit
	}
	if mode == ModeDaily {
		return DailyLength(day)
	}
	if IsSupportedLength(p.cfg.DefaultLength) {
		return p.cfg.DefaultLength
	}
	return PickRandomLength()
}

// resolveTries 决定本局机会数：显式指定 > 配置默认 > 依据长度与棋盘数推导。
func (p *Plugin) resolveTries(explicit, length, boards int) int {
	if explicit > 0 {
		return explicit
	}
	if p.cfg.DefaultTries >= minAttempts && p.cfg.DefaultTries <= maxAttempts {
		return p.cfg.DefaultTries
	}
	return attemptsForGame(length, boards)
}

// unlimitedConflict 返回与 --unlimited 冲突的开关名（无冲突时返回空串）。
//
// 无限机会会让"每解一题少一次机会"（gauntlet）与"提示消耗一次机会"（hint-cost）
// 失去意义，后者更会让提示变成无限免费，因此三者互斥。
func unlimitedConflict(flags map[string]string) string {
	if !flagOn(flags, "unlimited") {
		return ""
	}
	if _, ok := flags["tries"]; ok {
		return "tries"
	}
	if flagOn(flags, "gauntlet") {
		return "gauntlet"
	}
	if flagOn(flags, "hint-cost") {
		return "hint-cost"
	}
	return ""
}

// triesLabel 返回机会数的展示文本；无限机会统一显示为 "∞"。
func triesLabel(unlimited bool, tries int) any {
	if unlimited {
		return "∞"
	}
	return tries
}

// attemptsForGame 是默认机会数的推导规则：以经典 Wordle 的 6 次为下限，字母每多一个再 +1，
// 每多一块棋盘再 +2，并夹在允许范围内。
//
// 4 字母 = 6 次、5 字母 = 6 次（经典 Wordle）、6 字母 = 7 次、7 字母 = 8 次。
// 字母越少，每次猜测提供的信息越少，所以短词也保底 6 次，避免短词反而更难。
func attemptsForGame(length, boards int) int {
	if boards < 1 {
		boards = 1
	}
	base := max(length+1, DefaultMaxAttempts)
	return min(max(base+(boards-1)*2, minAttempts), maxAttempts)
}

// clampCells 把"每行作用格数"收紧到词长范围内。
//
// 参数范围是 1..7，但 4 字母时 --colorfog=4 等于无效果、--unknown=4 会遮住整行；
// 收紧到 length-1 后保证参数始终有实际作用且不会整行失效。
func clampCells(v, length int) int {
	if v <= 0 {
		return 0
	}
	return min(v, max(1, length-1))
}

// clampRows 把"作用于最近 N 行"的参数收紧到本局可猜次数内（至少保留一行可读）。
func clampRows(v, tries int) int {
	if v <= 0 {
		return 0
	}
	return min(v, max(1, tries-1))
}

// freeHintCap 按词长收紧免费提示上限。
//
// 一次提示直接揭示一个字母位置，固定 2 次在 4 字母上等于白送一半答案，
// 而 7 字母只有 2/7；收紧后各长度的免费收益大致相当（4→1，5 及以上沿用配置值）。
func freeHintCap(length, configured int) int {
	if configured <= 0 {
		return 0
	}
	return min(configured, max(1, length-3))
}

// pickAnswers 为多谜底模式挑选 n 个互不相同的谜底。
//
// 冷门词库（--obscure）从合法输入减去常用谜底后的池中取词；
// NOREPEAT 修饰符在可用时优先只取不含重复字母的词。
func pickAnswers(bank *WordBank, rules RuleSet, mods Modifier, n int) []string {
	if n < 1 {
		n = 1
	}
	pool := bank.Pool(rules.Has(RuleObscure))
	if mods.Has(ModNoRepeat) {
		distinct := make([]string, 0, len(pool))
		for _, w := range pool {
			if HasDistinctLetters(w) {
				distinct = append(distinct, w)
			}
		}
		if len(distinct) >= n {
			pool = distinct
		}
	}
	out := PickManyFrom(pool, n)
	if len(out) == 0 {
		out = []string{bank.Pick()}
	}
	return out
}

// pickDailyAnswers 按日期为多谜底模式派生 n 个稳定且互不相同的谜底。
func pickDailyAnswers(bank *WordBank, rules RuleSet, n int, day string) []string {
	if n < 1 {
		n = 1
	}
	pool := bank.Pool(rules.Has(RuleObscure))
	if len(pool) == 0 {
		pool = bank.answers
	}
	if n > len(pool) {
		n = len(pool)
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		// 同一日期同一棋盘编号必须稳定；哈希撞词时用 salt 再散列避免重复。
		for salt := range 64 {
			key := fmt.Sprintf("%s#%d#%d", day, i, salt)
			w := pool[DailyIndexIn(pool, key)]
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			out = append(out, w)
			break
		}
	}
	if len(out) == 0 {
		out = []string{pool[DailyIndexIn(pool, day)]}
	}
	return out
}

// startChainLink 在连锁模式下换到下一题（长度与次数沿用本局设置）。
// 换题失败时把对局置为结束并返回 false。
func (p *Plugin) startChainLink(g *Game) bool {
	bank, err := loadWordBank(g.Length)
	if err != nil {
		g.Finished = true
		p.warnf("wordle: 连锁换题失败: %v", err)
		return false
	}
	g.advanceChain(pickAnswers(bank, g.Rules, g.Modifiers, g.BoardCount()))
	return true
}

// winNotice 生成胜利文案：多谜底时用专门的文案点出棋盘数量。
func (p *Plugin) winNotice(ctx *eventctx.Context, g *Game) string {
	if g.BoardCount() > 1 {
		return p.t(ctx, "wordle.result.win_multi", map[string]any{
			"Attempts": len(g.Guesses),
			"Boards":   g.BoardCount(),
		})
	}
	return p.t(ctx, "wordle.result.win", map[string]any{"Attempts": len(g.Guesses)})
}

func allCorrect(marks []Mark) bool {
	if len(marks) == 0 {
		return false
	}
	for _, m := range marks {
		if m != Correct {
			return false
		}
	}
	return true
}

// shortID 把长平台 ID 压缩为可读的短标识（保留尾部 6 位）。
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return "…" + id[len(id)-6:]
}

// displayUser 返回可读的用户标识：优先昵称，缺失时回退到短 ID。
func displayUser(name, userID string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return shortID(userID)
}

// normalizeLocale 把常见简写归一化为内置 locale。
func normalizeLocale(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "zh-cn", "cn", "zh-hans":
		return localeZH
	case "en", "en-us", "en-gb":
		return localeEN
	default:
		return s
	}
}

func isSupportedLocale(loc string) bool {
	return slices.Contains(SupportedLocales, loc)
}
