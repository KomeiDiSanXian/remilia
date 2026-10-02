package songdle

import (
	"fmt"
	"strconv"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// 排行榜规模上限。
const (
	topLimit            = 10
	minLeaderboardGames = 1
)

// buttonPrefix 是按钮回调 ID 前缀，用于与其他插件的按钮区分。
const buttonPrefix = "songdle:"

// handleSongdle 是 /songdle 的总入口，按第一个位置参数路由到各子命令。
//
// 谜底是曲名（最终答案），玩家先用元数据探测缩小范围，再提交曲名。路由规则：
//   - 已知子命令（new / board / giveup / pool / stats / top / lang / help）按名字分发；
//   - 「探测」子命令或未识别但首词是属性名（曲师 / bpm / 流派 …）→ 属性探测；
//   - 其余情况把整段输入当作曲名直猜，方便玩家少打一个 guess。
func (p *Plugin) handleSongdle(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyText(p.t(ctx, "songdle.help"))
		return nil
	}
	args := parsed.Positional
	if len(args) == 0 {
		return p.cmdNew(ctx, parsed.Flags)
	}
	head := strings.ToLower(strings.TrimSpace(args[0]))
	switch head {
	case "new", "start", "开局", "开始":
		return p.cmdNew(ctx, parsed.Flags)
	case "guess", "g", "answer", "title", "猜", "猜曲", "歌名", "曲名", "标题":
		return p.cmdGuess(ctx, args[1:])
	case "probe", "p", "ask", "探测", "查":
		return p.cmdProbe(ctx, args[1:])
	case "board", "b", "面板", "提示", "提示板":
		return p.cmdBoard(ctx)
	case "giveup", "surrender", "放弃":
		return p.cmdGiveup(ctx)
	case "pool", "data", "info", "曲库":
		return p.cmdPool(ctx)
	case "stats", "stat", "战绩":
		return p.cmdStats(ctx)
	case "top", "rank", "排行", "排行榜":
		return p.cmdTop(ctx)
	case "lang", "language", "语言":
		return p.cmdLang(ctx, args[1:])
	case "help", "?", "帮助":
		ctx.ReplyText(p.t(ctx, "songdle.help"))
		return nil
	}
	if attr, ok := ParseAttribute(args[0]); ok {
		if attr == AttrTitle {
			return p.cmdGuess(ctx, args[1:])
		}
		return p.cmdProbeAttr(ctx, attr, args[1:])
	}
	return p.cmdGuess(ctx, args)
}

// cmdNew 开始新的一局。
func (p *Plugin) cmdNew(ctx *eventctx.Context, flags map[string]string) error {
	platformID, chatID, userID := sessionContextKeys(ctx)

	tries := p.cfg.DefaultTries
	if v := strings.TrimSpace(flags["tries"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < minTries || n > p.cfg.MaxTries {
			ctx.ReplyError(p.t(ctx, "songdle.start.bad_tries",
				map[string]any{"Min": minTries, "Max": p.cfg.MaxTries}))
			return nil
		}
		tries = n
	}

	scope := p.cfg.DefaultScope
	if v := strings.TrimSpace(flags["scope"]); v != "" {
		s, ok := ParseScope(v)
		if !ok {
			ctx.ReplyError(p.t(ctx, "songdle.start.bad_scope"))
			return nil
		}
		scope = s
	}

	filter := Filter{Type: p.cfg.DefaultType, Genre: p.cfg.DefaultGenre}
	if v := strings.TrimSpace(flags["type"]); v != "" {
		ty, ok := normalizeType(v)
		if !ok {
			ctx.ReplyError(p.t(ctx, "songdle.start.bad_type"))
			return nil
		}
		filter.Type = ty
	}
	if v := strings.TrimSpace(flags["genre"]); v != "" {
		filter.Genre = v
	}

	mode := ModeRandom
	_, dailySet := flags["daily"]
	if flagOn(flags, "daily") || (!dailySet && p.cfg.DefaultDaily) {
		mode = ModeDaily
	}

	revealArtist := p.cfg.RevealArtist
	if _, ok := flags["artist"]; ok {
		revealArtist = flagOn(flags, "artist")
	}

	key := SessionKey(platformID, chatID, userID, scope)
	if g, ok := p.sessions.Get(key); ok && !g.Finished {
		p.replyBoard(ctx, g, p.t(ctx, "songdle.start.already_running"))
		return nil
	}

	// 每日题：同一维度同一天只能开一次，避免反复重开刷战绩。
	day := p.today()
	lockKey := dailyLockKey(platformID, chatID, userID, scope)
	if mode == ModeDaily && p.store != nil {
		locked, err := p.store.dailyLockDay(lockKey)
		if err != nil {
			p.warnf("songdle: 读取每日锁失败: %v", err)
		} else if locked == day {
			ctx.ReplyText(p.t(ctx, "songdle.start.daily_done"))
			return nil
		}
	}

	var (
		target Track
		err    error
	)
	if mode == ModeDaily {
		target, err = p.pool.PickDaily(filter, day+"|"+filter.key())
	} else {
		target, err = p.pool.Pick(filter)
	}
	if err != nil {
		ctx.ReplyError(p.t(ctx, "songdle.start.empty_pool",
			map[string]any{"Type": filter.Type, "Genre": filter.Genre}))
		return nil
	}

	now := timeNow()
	g := &Game{
		ID:           strconv.FormatInt(now.UnixNano(), 36),
		Platform:     platformID,
		ChatID:       chatID,
		OwnerID:      userID,
		OwnerName:    ctx.GetDisplayName(),
		Scope:        scope,
		Mode:         mode,
		Daily:        mode == ModeDaily,
		Target:       target,
		MaxAttempts:  tries,
		RevealArtist: revealArtist,
		Filter:       filter,
		Participants: make(map[string]string, 4),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	p.sessions.Put(key, g)
	if mode == ModeDaily && p.store != nil {
		if err := p.store.setDailyLock(lockKey, day); err != nil {
			p.warnf("songdle: 写入每日锁失败: %v", err)
		}
	}

	p.replyBoard(ctx, g, p.t(ctx, "songdle.start.created", map[string]any{
		"Mode":  p.modeLabel(ctx, mode),
		"Tries": tries,
		"Type":  filter.Type,
		"Genre": filter.Genre,
		"Attrs": p.attrList(ctx),
	}))
	return nil
}

// cmdProbe 处理 /songdle probe <属性> <取值>（也接受 /songdle <属性> <取值>）。
func (p *Plugin) cmdProbe(ctx *eventctx.Context, args []string) error {
	if len(args) == 0 {
		ctx.ReplyError(p.t(ctx, "songdle.probe.usage", map[string]any{"Attrs": p.attrList(ctx)}))
		return nil
	}
	attr, ok := ParseAttribute(args[0])
	if !ok {
		ctx.ReplyError(p.t(ctx, "songdle.probe.bad_attr",
			map[string]any{"Value": args[0], "Attrs": p.attrList(ctx)}))
		return nil
	}
	if attr == AttrTitle {
		return p.cmdGuess(ctx, args[1:])
	}
	return p.cmdProbeAttr(ctx, attr, args[1:])
}

// cmdProbeAttr 处理已确定属性的探测；value 为剩余参数拼成的取值。
func (p *Plugin) cmdProbeAttr(ctx *eventctx.Context, attr Attribute, args []string) error {
	value := strings.TrimSpace(strings.Join(args, " "))
	if value == "" {
		ctx.ReplyError(p.t(ctx, "songdle.probe.usage", map[string]any{"Attrs": p.attrList(ctx)}))
		return nil
	}
	return p.submitProbe(ctx, attr, value)
}

// cmdGuess 猜一首曲目：把输入（曲名 / 别名 / 曲目 ID）解析成曲库里的一首，
// 与谜底逐列对比后回一整行提示；猜中谜底即获胜。
//
// 解析不到、或匹配到多首不同曲目时不消耗次数，只给出候选，方便玩家改用 ID。
func (p *Plugin) cmdGuess(ctx *eventctx.Context, args []string) error {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		ctx.ReplyError(p.t(ctx, "songdle.error.usage_guess"))
		return nil
	}
	platformID, chatID, userID := sessionContextKeys(ctx)
	var (
		game      *Game
		notice    string
		attempted bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			notice = p.t(ctx, "songdle.error.already_finished")
			return
		}
		res := p.pool.Match(query)
		if len(res.Candidates) == 0 {
			notice = p.t(ctx, "songdle.guess.not_found", map[string]any{"Query": query})
			return
		}
		// 只有精确匹配（ID / 完全同名 / 完全同别名）才算一次猜测；
		// 包含匹配只作为「你是不是想找」提示，不判胜负也不消耗次数。
		if !res.Exact {
			notice = p.t(ctx, "songdle.guess.suggest", map[string]any{
				"Query":      query,
				"Candidates": p.candidateList(ctx, res.Candidates),
			})
			return
		}
		guess, ambiguous := resolveGuess(res.Candidates, g.Target, g.Filter)
		if len(ambiguous) > 0 {
			notice = p.t(ctx, "songdle.guess.ambiguous", map[string]any{
				"Query":      query,
				"Candidates": p.candidateList(ctx, ambiguous),
			})
			return
		}
		if g.HasGuessed(guess) {
			notice = p.t(ctx, "songdle.guess.repeat", map[string]any{"Title": guess.Title})
			return
		}
		tg := TrackGuess{Track: guess, Cells: Compare(g.Target, guess)}
		g.AddParticipant(userID, ctx.GetDisplayName())
		finished := g.SubmitGuess(tg, timeNow())
		attempted = true
		notice = p.guessNotice(ctx, g, finished)
	})
	if !found {
		ctx.ReplyError(p.t(ctx, "songdle.error.not_started"))
		return nil
	}
	if !attempted {
		ctx.ReplyText(notice)
		return nil
	}
	if game.Finished {
		p.finalize(game)
	}
	p.replyBoard(ctx, game, notice)
	return nil
}

// resolveGuess 决定玩家到底猜的是哪一首曲目。
//
// 谜底本身出现在候选里时直接判中（曲名相同即算猜对，不必区分 SD / DX 谱面）；
// 候选只有一首、或都只是同一首歌的不同谱面时取其中一首；否则返回候选列表，
// 由调用方提示玩家改用 ID 指定。
func resolveGuess(cands []Track, target Track, filter Filter) (Track, []Track) {
	for _, t := range cands {
		if t.ID != "" && t.ID == target.ID {
			return t, nil
		}
	}
	if len(cands) == 1 {
		return cands[0], nil
	}
	key := cands[0].Key()
	for _, t := range cands[1:] {
		if t.Key() != key {
			return Track{}, cands
		}
	}
	return preferTrack(cands, filter), nil
}

// preferTrack 在同一首歌的多个谱面（SD / DX）中挑一个代表：
// 优先本局筛选的谱面类型，其次 DX，最后取第一首。
func preferTrack(cands []Track, filter Filter) Track {
	if filter.Type != "" {
		for _, t := range cands {
			if strings.EqualFold(t.Type, filter.Type) {
				return t
			}
		}
	}
	for _, t := range cands {
		if strings.EqualFold(t.Type, "DX") {
			return t
		}
	}
	return cands[0]
}

// candidateList 把候选曲目渲染成多行提示。
func (p *Plugin) candidateList(ctx *eventctx.Context, cands []Track) string {
	lines := make([]string, 0, len(cands))
	for _, t := range cands {
		lines = append(lines, p.t(ctx, "songdle.guess.candidate", map[string]any{
			"ID": t.ID, "Title": t.Title, "Type": t.Type, "Version": t.Version,
		}))
	}
	return strings.Join(lines, "\n")
}

// guessNotice 生成本次猜曲目的结果提示语。
func (p *Plugin) guessNotice(ctx *eventctx.Context, g *Game, finished bool) string {
	switch {
	case g.Won:
		return p.t(ctx, "songdle.probe.win", map[string]any{"Attempts": g.Attempts()})
	case finished:
		return p.t(ctx, "songdle.probe.lose")
	default:
		return p.t(ctx, "songdle.guess.miss", map[string]any{"Remaining": g.Remaining()})
	}
}

// submitProbe 把一次属性 / 曲名猜测提交进当前对局，并发送更新后的提示板。
func (p *Plugin) submitProbe(ctx *eventctx.Context, attr Attribute, raw string) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	var (
		game     *Game
		notice   string
		finished bool
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			notice = p.t(ctx, "songdle.error.already_finished")
			return
		}
		pr, err := MakeProbe(g.Target, attr, raw)
		if err != nil {
			notice = p.t(ctx, "songdle.probe.bad_value", map[string]any{
				"Attr":  p.t(ctx, attr.LabelKey()),
				"Value": raw,
			})
			return
		}
		if g.HasProbed(pr) {
			notice = p.t(ctx, "songdle.probe.repeat", map[string]any{
				"Attr":  p.t(ctx, attr.LabelKey()),
				"Value": pr.Value,
			})
			return
		}
		g.AddParticipant(ctx.GetUserID(), ctx.GetDisplayName())
		finished = g.Submit(pr, timeNow())
		notice = p.probeNotice(ctx, g, pr, finished)
	})
	if !found {
		ctx.ReplyError(p.t(ctx, "songdle.error.not_started"))
		return nil
	}
	if finished {
		p.finalize(game)
	}
	p.replyBoard(ctx, game, notice)
	return nil
}

// probeNotice 生成本次猜测的结果提示语。
func (p *Plugin) probeNotice(ctx *eventctx.Context, g *Game, pr Probe, finished bool) string {
	switch {
	case g.Won:
		return p.t(ctx, "songdle.probe.win", map[string]any{"Attempts": g.Attempts()})
	case finished:
		return p.t(ctx, "songdle.probe.lose")
	default:
		return p.t(ctx, "songdle.probe.recorded", map[string]any{"Remaining": g.Remaining()})
	}
}

// cmdBoard 重新发送当前提示板。
func (p *Plugin) cmdBoard(ctx *eventctx.Context) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	g, ok := p.sessions.Find(platformID, chatID, userID)
	if !ok {
		ctx.ReplyText(p.t(ctx, "songdle.error.not_started"))
		return nil
	}
	p.replyBoard(ctx, g, "")
	return nil
}

// cmdGiveup 放弃当前对局并公布答案。
func (p *Plugin) cmdGiveup(ctx *eventctx.Context) error {
	platformID, chatID, userID := sessionContextKeys(ctx)
	var (
		game   *Game
		notice string
	)
	found := p.sessions.UpdateFound(platformID, chatID, userID, func(g *Game) {
		game = g
		if g.Finished {
			notice = p.t(ctx, "songdle.error.already_finished")
			return
		}
		if !canEndGame(ctx, g) {
			notice = p.t(ctx, "songdle.giveup.denied")
			return
		}
		g.Finished, g.Won = true, false
		g.UpdatedAt = timeNow()
		notice = p.t(ctx, "songdle.giveup.done")
	})
	if !found {
		ctx.ReplyError(p.t(ctx, "songdle.error.not_started"))
		return nil
	}
	if game != nil && game.Finished {
		p.finalize(game)
	}
	p.replyBoard(ctx, game, notice)
	return nil
}

// cmdPool 展示曲库规模与分类统计。
func (p *Plugin) cmdPool(ctx *eventctx.Context) error {
	pool := p.pool
	if pool == nil || pool.Len() == 0 {
		ctx.ReplyText(p.t(ctx, "songdle.pool.empty"))
		return nil
	}
	var b strings.Builder
	b.WriteString(p.t(ctx, "songdle.pool.total", map[string]any{
		"Total": pool.Len(),
		"Attrs": p.attrList(ctx),
	}))
	for _, tc := range pool.TypeCounts() {
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.pool.row", map[string]any{"Name": tc.Tag, "Count": tc.Count}))
	}
	for i, tc := range pool.GenreCounts() {
		if i >= topLimit {
			break
		}
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.pool.row", map[string]any{"Name": tc.Tag, "Count": tc.Count}))
	}
	ctx.ReplyText(b.String())
	return nil
}

// cmdStats 展示个人 / @用户 的战绩。
func (p *Plugin) cmdStats(ctx *eventctx.Context) error {
	if p.store == nil {
		ctx.ReplyText(p.t(ctx, "songdle.stats.unavailable"))
		return nil
	}
	uid := targetUserID(ctx)
	st, err := p.store.loadStat(uid)
	if err != nil {
		ctx.ReplyError(p.t(ctx, "songdle.stats.error", map[string]any{"Error": err.Error()}))
		return nil
	}
	var name string
	switch {
	case strings.TrimSpace(st.Name) != "":
		name = st.Name
	case uid == ctx.GetUserID() && ctx.GetDisplayName() != "":
		name = ctx.GetDisplayName()
	default:
		name = shortID(uid)
	}

	var b strings.Builder
	b.WriteString(p.t(ctx, "songdle.stats.title", map[string]any{"User": name}))
	b.WriteByte('\n')
	b.WriteString(p.t(ctx, "songdle.stats.body", map[string]any{
		"Played": st.Played,
		"Won":    st.Won,
		"Rate":   fmt.Sprintf("%.1f", st.WinRate()),
		"Streak": st.Streak,
		"Best":   st.BestStreak,
		"Fewest": st.BestAttempts,
	}))
	b.WriteByte('\n')
	b.WriteString(p.t(ctx, "songdle.stats.dist.title"))
	b.WriteByte('\n')
	b.WriteString(p.t(ctx, "songdle.stats.dist.fail", map[string]any{"Count": st.GuessDist[0]}))
	for i := 1; i < len(st.GuessDist); i++ {
		if st.GuessDist[i] == 0 {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.stats.dist.row", map[string]any{
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
		ctx.ReplyText(p.t(ctx, "songdle.top.unavailable"))
		return nil
	}
	recs, err := p.store.leaderboard(topLimit, minLeaderboardGames)
	if err != nil {
		ctx.ReplyError(p.t(ctx, "songdle.top.error", map[string]any{"Error": err.Error()}))
		return nil
	}
	if len(recs) == 0 {
		ctx.ReplyText(p.t(ctx, "songdle.top.empty", map[string]any{"Min": minLeaderboardGames}))
		return nil
	}
	var b strings.Builder
	b.WriteString(p.t(ctx, "songdle.top.title", map[string]any{"Min": minLeaderboardGames}))
	for i, r := range recs {
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.top.row", map[string]any{
			"Rank":   i + 1,
			"User":   displayUser(r.Name, r.UserID),
			"Won":    r.Won,
			"Played": r.Played,
			"Rate":   fmt.Sprintf("%.0f", r.WinRate()),
			"Best":   r.BestAttempts,
		}))
	}
	ctx.ReplyText(b.String())
	return nil
}

// cmdLang 切换当前用户的语言偏好。
func (p *Plugin) cmdLang(ctx *eventctx.Context, args []string) error {
	if len(args) == 0 {
		ctx.ReplyError(p.t(ctx, "songdle.lang.usage", map[string]any{"Locales": localeList()}))
		return nil
	}
	loc := normalizeLocale(args[0])
	if !isSupportedLocale(loc) {
		ctx.ReplyError(p.t(ctx, "songdle.lang.unknown", map[string]any{
			"Locale":  args[0],
			"Locales": localeList(),
		}))
		return nil
	}
	if p.i18n != nil {
		p.i18n.SetLocale(ctx, loc)
	}
	ctx.ReplySuccess(p.t(ctx, "songdle.lang.set", map[string]any{"Locale": loc}))
	return nil
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
	case "board":
		return p.cmdBoard(ctx)
	case "giveup":
		return p.cmdGiveup(ctx)
	case "title":
		ctx.ReplyText(p.t(ctx, "songdle.button.title_tip"))
		return nil
	case "bpm":
		ctx.ReplyText(p.t(ctx, "songdle.button.bpm_tip"))
		return nil
	case "artist":
		ctx.ReplyText(p.t(ctx, "songdle.button.artist_tip"))
		return nil
	}
	return nil
}

// finalize 结算一局：为每位参与者各记一次战绩，并写入一条对局历史。
//
// 群维度共享谜底时参与者可能有多人，胜负共享，因此每人各记一次统计。
// recorded 保证只结算一次。
func (p *Plugin) finalize(g *Game) {
	if g == nil || g.recorded {
		return
	}
	g.recorded = true
	if p.store == nil {
		return
	}
	owner := g.OwnerID
	if owner == "" {
		owner = "unknown"
	}
	if len(g.Participants) == 0 {
		g.AddParticipant(owner, "")
	}
	for uid := range g.Participants {
		st, err := p.store.loadStat(uid)
		if err != nil {
			p.warnf("songdle: 读取统计失败: %v", err)
			continue
		}
		if name := strings.TrimSpace(g.Participants[uid]); name != "" {
			st.Name = name
		}
		st.Record(g.Won, g.Attempts())
		if err := p.store.saveStat(st); err != nil {
			p.warnf("songdle: 保存统计失败: %v", err)
		}
	}

	dur := max(g.UpdatedAt.Sub(g.CreatedAt).Milliseconds(), 0)
	if err := p.store.recordGame(g, owner, dur); err != nil {
		p.warnf("songdle: 写入对局记录失败: %v", err)
	}
}

// modeLabel 返回模式展示名。
func (p *Plugin) modeLabel(ctx *eventctx.Context, m Mode) string {
	if m == ModeDaily {
		return p.t(ctx, "songdle.mode.daily")
	}
	return p.t(ctx, "songdle.mode.random")
}

// attrList 返回可探测属性的展示名列表，用于用法提示。
func (p *Plugin) attrList(ctx *eventctx.Context) string {
	names := make([]string, 0, len(ClueAttributes))
	for _, attr := range ClueAttributes {
		names = append(names, p.t(ctx, attr.LabelKey()))
	}
	return strings.Join(names, " / ")
}

// normalizeType 归一化曲目类型筛选；all/空 表示不限。
func normalizeType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "any", "*":
		return "", true
	case "sd":
		return "SD", true
	case "dx":
		return "DX", true
	default:
		return "", false
	}
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

// dailyLockKey 返回每日锁的键，按隔离维度区分。
func dailyLockKey(platformID, chatID, userID string, scope Scope) string {
	return "songdle|" + SessionKey(platformID, chatID, userID, scope)
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

// canEndGame 报告调用者是否有权终止对局。
//
// 用户维度各玩各的，谁都能放弃自己的对局；群维度是共享谜底，放弃会让所有参与者
// 一起记负，因此只允许发起者终止。
func canEndGame(ctx *eventctx.Context, g *Game) bool {
	return g.Scope != ScopeGroup || ctx.GetUserID() == g.OwnerID
}

// flagOn 报告布尔标志是否被显式开启。
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
