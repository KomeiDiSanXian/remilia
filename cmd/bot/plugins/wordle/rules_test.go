package wordle

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestParseRuleList(t *testing.T) {
	rules, err := ParseRuleList("hard, chain")
	if err != nil {
		t.Fatalf("ParseRuleList: %v", err)
	}
	if !rules.Has(RuleHard) || !rules.Has(RuleChain) || rules.Has(RuleRace) {
		t.Fatalf("解析结果错误: %v", rules)
	}
	if got := rules.String(); got != "hard,chain" {
		t.Fatalf("String() = %q, 期望 hard,chain", got)
	}
	if _, err := ParseRuleList("hard,nope"); err == nil {
		t.Fatal("未知规则应返回错误")
	}
	if rules, err := ParseRuleList("  "); err != nil || rules != 0 {
		t.Fatalf("空串应返回零值且不报错，实际 %v,%v", rules, err)
	}
}

func TestRuleSetLabels(t *testing.T) {
	labels := (RuleHard | RuleRace).Labels()
	if len(labels) != 2 || labels[0] != "HARD" || labels[1] != "RACE" {
		t.Fatalf("Labels() = %v, 期望 [HARD RACE]", labels)
	}
	if got := RuleSet(0).Labels(); len(got) != 0 {
		t.Fatalf("零值不应有徽标，实际 %v", got)
	}
}

func TestModifierRoundTrip(t *testing.T) {
	m := ModBlind | ModDouble
	if got := m.String(); got != "blind,double" {
		t.Fatalf("String() = %q, 期望 blind,double", got)
	}
	if got := ParseModifiers("blind,double"); got != m {
		t.Fatalf("ParseModifiers 往返失败: %v", got)
	}
	if got := ParseModifiers("unknown,  "); got != 0 {
		t.Fatalf("未知修饰符应被忽略，实际 %v", got)
	}
	if got := ModNoHint.Labels(); len(got) != 1 || got[0] != "NOHINT" {
		t.Fatalf("Labels() = %v, 期望 [NOHINT]", got)
	}
}

func TestPickChaos(t *testing.T) {
	for range 200 {
		m := pickChaos(0)
		if m == 0 {
			t.Fatal("混沌模式应至少抽到一个修饰符")
		}
		if n := len(m.Names()); n < 1 || n > 2 {
			t.Fatalf("修饰符数量应为 1-2，实际 %d (%v)", n, m)
		}
	}
	// 启用提示经济时不应再抽到"禁用提示"，避免规则互相打架。
	for range 200 {
		if m := pickChaos(RuleHintCost); m.Has(ModNoHint) {
			t.Fatal("hint-cost 下不应抽到 nohint")
		}
	}
}

func TestCheckHardMode(t *testing.T) {
	base := func() *Game {
		return &Game{
			Length:  5,
			Answers: []string{"crane"},
			Solved:  []bool{false},
			Guesses: []Guess{{Word: "cadre", Marks: Evaluate("crane", "cadre")}},
		}
	}
	// cadre 对 crane：第 0 位绿、第 4 位绿，a/r 为黄。
	if g := base(); !checkHardMode(g, []rune("crane")).OK() {
		t.Fatal("完全复用提示的猜测应合法")
	}
	if v := checkHardMode(base(), []rune("crane")); !v.OK() {
		t.Fatalf("crane 应合法: %+v", v)
	}
	// 第 4 位被换掉 -> 位置违规
	if v := checkHardMode(base(), []rune("crank")); v.Position != 5 {
		t.Fatalf("期望位置 5 违规，实际 %+v", v)
	}
	// 丢掉黄色字母 a -> 字母违规
	if v := checkHardMode(base(), []rune("crone")); v.Letter != 'a' {
		t.Fatalf("期望缺少字母 a，实际 %+v", v)
	}
	// 提示揭示的位置同样必须保持
	g := base()
	g.Revealed = []int{1}
	if v := checkHardMode(g, []rune("cadre")); v.Position != 2 {
		t.Fatalf("期望位置 2 违规，实际 %+v", v)
	}
}

func TestBlindMarks(t *testing.T) {
	marks := []Mark{Correct, Present, Absent}
	got := blindMarks(marks)
	if got[0] != Correct || got[1] != Absent || got[2] != Absent {
		t.Fatalf("盲猜应把黄色降级为灰色，实际 %v", got)
	}
	if marks[1] != Present {
		t.Fatal("不应修改原切片")
	}
}

func TestRaceGain(t *testing.T) {
	before := map[int]bool{0: true}
	marks := []Mark{Correct, Correct, Absent, Present}
	if got := raceGain(before, marks); got != 1 {
		t.Fatalf("raceGain = %d, 期望 1", got)
	}
}

func TestHasDistinctLetters(t *testing.T) {
	if !HasDistinctLetters("crane") {
		t.Fatal("crane 无重复字母")
	}
	if HasDistinctLetters("eaten") {
		t.Fatal("eaten 含重复字母 e")
	}
}

func TestGame_AttemptCostAndRemaining(t *testing.T) {
	g := &Game{Length: 5, MaxAttempts: 6, Modifiers: ModDouble}
	if g.AttemptCost() != 2 {
		t.Fatalf("COSTx2 下次猜测应消耗 2，实际 %d", g.AttemptCost())
	}
	if g.OutOfAttempts() {
		t.Fatal("剩余 6 次不应判为无次数")
	}
	g.Used = 5
	if g.Remaining() != 1 {
		t.Fatalf("剩余应为 1，实际 %d", g.Remaining())
	}
	if !g.OutOfAttempts() {
		t.Fatal("剩余 1 < 消耗 2 应判为无次数")
	}
}

func TestGame_ScoreboardOrder(t *testing.T) {
	g := &Game{
		Score:        map[string]int{"a": 2, "b": 5, "c": 0},
		Participants: map[string]string{"a": "甲", "b": "乙", "c": "丙"},
	}
	sb := g.Scoreboard()
	if len(sb) != 2 {
		t.Fatalf("零分不应上榜，实际 %d 条", len(sb))
	}
	if sb[0].UserID != "b" || sb[0].Name != "乙" || sb[0].Score != 5 {
		t.Fatalf("排序错误: %+v", sb)
	}
}

func TestGame_AdvanceChain(t *testing.T) {
	old := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	defer func() { timeNow = old }()

	g := &Game{
		Length:       5,
		MaxAttempts:  6,
		Answers:      []string{"crane"},
		Solved:       []bool{false},
		Rules:        RuleChain | RuleBlitz,
		BlitzWindow:  time.Minute,
		Finished:     true,
		Won:          true,
		Guesses:      []Guess{{Word: "crane"}},
		Used:         3,
		Participants: map[string]string{"u": "玩家"},
	}
	g.advanceChain([]string{"slate"})

	if g.AnswerAt(0) != "slate" || g.ChainIndex != 1 || g.ChainWins != 1 {
		t.Fatalf("换题状态错误: %+v", g)
	}
	if g.Finished || g.Won || g.Used != 0 || len(g.Guesses) != 0 || len(g.Participants) != 0 {
		t.Fatalf("换题应重置进度: %+v", g)
	}
	if g.Deadline.IsZero() {
		t.Fatal("限时模式换题后应重置截止时间")
	}
}

func TestGame_Expired(t *testing.T) {
	g := &Game{Rules: RuleBlitz, Deadline: time.Unix(1000, 0)}
	if !g.Expired(time.Unix(1001, 0)) {
		t.Fatal("超过截止时间应判定超时")
	}
	if g.Expired(time.Unix(999, 0)) {
		t.Fatal("未到截止时间不应超时")
	}
	if (&Game{}).Expired(time.Now()) {
		t.Fatal("非限时对局不应超时")
	}
}

func TestGame_AdvanceChainGauntlet(t *testing.T) {
	g := &Game{
		Length:      5,
		MaxAttempts: 5,
		Answers:     []string{"crane"},
		Solved:      []bool{true},
		Rules:       RuleChain | RuleGauntlet,
		Finished:    true,
		Won:         true,
	}
	g.advanceChain([]string{"slate", "adore"})

	if g.MaxAttempts != 4 {
		t.Fatalf("车轮战每解一题应减少一次机会，实际 %d", g.MaxAttempts)
	}
	if g.BoardCount() != 2 || g.Solved[0] || g.Solved[1] {
		t.Fatalf("换题后应重置为两块未解谜底: %+v", g)
	}
	// 下限为 2：反复换题不应把机会压到 1。
	for range 10 {
		g.advanceChain([]string{"slate"})
	}
	if g.MaxAttempts != 2 {
		t.Fatalf("车轮战机会下限应为 2，实际 %d", g.MaxAttempts)
	}
}

func TestParseOptions(t *testing.T) {
	p := &Plugin{cfg: config{BlitzWindow: 30 * time.Second}}

	opts, msg := p.parseOptions(nil, map[string]string{
		"hard": "true", "chain": "", "blitz": "45", "chaos": "false",
	})
	if msg != "" {
		t.Fatalf("parseOptions 报错: %s", msg)
	}
	if !opts.Rules.Has(RuleHard) || !opts.Rules.Has(RuleChain) || !opts.Rules.Has(RuleBlitz) {
		t.Fatalf("规则解析错误: %v", opts.Rules)
	}
	if opts.Rules.Has(RuleChaos) {
		t.Fatal("chaos=false 不应启用混沌模式")
	}
	if opts.Blitz != 45*time.Second {
		t.Fatalf("限时窗口 = %v, 期望 45s", opts.Blitz)
	}

	opts, msg = p.parseOptions(nil, map[string]string{"blitz": ""})
	if msg != "" || !opts.Rules.Has(RuleBlitz) || opts.Blitz != 30*time.Second {
		t.Fatalf("无值 --blitz 应使用配置默认值，实际 %v/%v/%q", opts.Rules, opts.Blitz, msg)
	}

	if _, msg := p.parseOptions(nil, map[string]string{"blitz": "5"}); msg == "" {
		t.Fatal("越界的限时秒数应报错")
	}
}

func TestParseOptions_HardModesAndBoards(t *testing.T) {
	p := &Plugin{cfg: config{BlitzWindow: time.Minute}}

	opts, msg := p.parseOptions(nil, map[string]string{
		"blind": "true", "obscure": "", "gauntlet": "true", "fog": "3",
	})
	if msg != "" {
		t.Fatalf("parseOptions 报错: %s", msg)
	}
	if !opts.Rules.Has(RuleBlind) || !opts.Rules.Has(RuleObscure) ||
		!opts.Rules.Has(RuleFog) || !opts.Rules.Has(RuleGauntlet) {
		t.Fatalf("困难玩法解析错误: %v", opts.Rules)
	}
	// gauntlet 隐含 chain。
	if !opts.Rules.Has(RuleChain) {
		t.Fatal("gauntlet 应隐含启用连锁模式")
	}
	if opts.FogRows != 3 {
		t.Fatalf("迷雾行数 = %d, 期望 3", opts.FogRows)
	}

	if opts, msg := p.parseOptions(nil, map[string]string{"duet": ""}); msg != "" || opts.Boards != 2 {
		t.Fatalf("--duet 应得到 2 块棋盘，实际 %d/%q", opts.Boards, msg)
	}
	if opts, msg := p.parseOptions(nil, map[string]string{"quad": ""}); msg != "" || opts.Boards != 4 {
		t.Fatalf("--quad 应得到 4 块棋盘，实际 %d/%q", opts.Boards, msg)
	}
	if opts, msg := p.parseOptions(nil, map[string]string{"boards": "3"}); msg != "" || opts.Boards != 3 {
		t.Fatalf("--boards 3 应得到 3 块棋盘，实际 %d/%q", opts.Boards, msg)
	}
	if _, msg := p.parseOptions(nil, map[string]string{"boards": "9"}); msg == "" {
		t.Fatal("越界的棋盘数量应报错")
	}
	if _, msg := p.parseOptions(nil, map[string]string{"fog": "9"}); msg == "" {
		t.Fatal("越界的迷雾行数应报错")
	}
}

func TestParseOptions_ColorRules(t *testing.T) {
	p := &Plugin{cfg: config{BlitzWindow: time.Minute}}

	opts, msg := p.parseOptions(nil, map[string]string{
		"invert": "true", "hit-only": "", "near": "true", "repeat": "true",
		"swap-meaning": "true", "glitch": "true", "mole": "true",
		"hidden-key": "true", "score-color": "true",
		"colorfog": "3", "decay": "2", "unknown": "1",
	})
	if msg != "" {
		t.Fatalf("parseOptions 报错: %s", msg)
	}
	for name, rule := range map[string]RuleSet{
		"invert": RuleInvert, "hit-only": RuleHitOnly, "near": RuleNear,
		"repeat": RuleRepeat, "swap-meaning": RuleSwapMeaning, "glitch": RuleGlitch,
		"mole": RuleMole, "hidden-key": RuleHiddenKey, "score-color": RuleScoreColor,
		"colorfog": RuleColorFog, "decay": RuleDecay, "unknown": RuleUnknown,
	} {
		if !opts.Rules.Has(rule) {
			t.Errorf("%s 未启用对应规则", name)
		}
	}
	if opts.ColorFog != 3 || opts.Decay != 2 || opts.Unknown != 1 {
		t.Fatalf("颜色参数解析错误: fog=%d decay=%d unknown=%d", opts.ColorFog, opts.Decay, opts.Unknown)
	}

	// 无值开关使用默认值。
	opts, msg = p.parseOptions(nil, map[string]string{"decoy": "", "delayed": ""})
	if msg != "" || opts.Decoy != 1 || opts.Delayed != 1 {
		t.Fatalf("无值 --decoy/--delayed 应使用默认 1，实际 %d/%d/%q", opts.Decoy, opts.Delayed, msg)
	}
	if !opts.Rules.Has(RuleDecoy) || !opts.Rules.Has(RuleDelayed) {
		t.Fatal("无值 --decoy/--delayed 应启用规则")
	}

	if _, msg := p.parseOptions(nil, map[string]string{"colorfog": "0"}); msg == "" {
		t.Fatal("越界的颜色预算应报错")
	}
	if _, msg := p.parseOptions(nil, map[string]string{"decay": "99"}); msg == "" {
		t.Fatal("越界的衰减行数应报错")
	}
}

func TestAttemptsForGame(t *testing.T) {
	cases := []struct{ length, boards, want int }{
		{4, 1, 5},
		{5, 1, 6},
		{6, 1, 7},
		{7, 1, 8},
		{5, 2, 8},  // 双谜底每多一块棋盘 +2
		{5, 4, 12}, // 四谜底会被上限截断
		{7, 4, 12},
	}
	for _, tc := range cases {
		if got := attemptsForGame(tc.length, tc.boards); got != tc.want {
			t.Errorf("attemptsForGame(%d,%d) = %d, 期望 %d", tc.length, tc.boards, got, tc.want)
		}
	}
}

func TestResolveLengthAndTries(t *testing.T) {
	p := &Plugin{}
	// 未指定且未配置时：长度随机 4-7，次数 = 长度 + 1。
	for range 50 {
		l := p.resolveLength(0, ModeRandom, "2026-10-01")
		if !IsSupportedLength(l) {
			t.Fatalf("随机长度应为受支持值，实际 %d", l)
		}
		if got := p.resolveTries(0, l, 1); got != l+1 {
			t.Fatalf("长度 %d 的默认次数 = %d, 期望 %d", l, got, l+1)
		}
	}

	// 每日题长度按日期稳定，且显式长度优先。
	if a, b := p.resolveLength(0, ModeDaily, "2026-10-01"), p.resolveLength(0, ModeDaily, "2026-10-01"); a != b {
		t.Fatalf("每日题长度应稳定: %d != %d", a, b)
	}
	if got := p.resolveLength(6, ModeDaily, "2026-10-01"); got != 6 {
		t.Fatalf("显式长度应优先，实际 %d", got)
	}
	if got := p.resolveTries(3, 7, 4); got != 3 {
		t.Fatalf("显式次数应优先，实际 %d", got)
	}

	// 配置默认值优先于随机/推导。
	p2 := &Plugin{cfg: config{DefaultLength: 4, DefaultTries: 9}}
	if got := p2.resolveLength(0, ModeRandom, ""); got != 4 {
		t.Fatalf("配置默认长度应生效，实际 %d", got)
	}
	if got := p2.resolveTries(0, 4, 1); got != 9 {
		t.Fatalf("配置默认次数应生效，实际 %d", got)
	}
}

func TestPickRandomAndDailyLength(t *testing.T) {
	for range 50 {
		if l := PickRandomLength(); !IsSupportedLength(l) {
			t.Fatalf("PickRandomLength 返回非法长度 %d", l)
		}
	}
	first := DailyLength("2026-10-01")
	second := DailyLength("2026-10-01")
	if first != second {
		t.Fatal("DailyLength 应稳定")
	}
	if l := DailyLength("2026-10-02"); !IsSupportedLength(l) {
		t.Fatalf("DailyLength 返回非法长度 %d", l)
	}
}

func TestSessionStore_MarkExpired(t *testing.T) {
	s := NewSessionStore(time.Minute)
	deadline := time.Unix(1000, 0)
	s.Put("blitz", &Game{Rules: RuleBlitz, Deadline: deadline, UpdatedAt: deadline})
	s.Put("classic", &Game{Rules: RuleHard, UpdatedAt: deadline})
	s.Put("done", &Game{Rules: RuleBlitz, Deadline: deadline, Finished: true, Won: true, UpdatedAt: deadline})

	expired := s.MarkExpired(time.Unix(1001, 0))
	if len(expired) != 1 {
		t.Fatalf("应标记 1 个超时对局，实际 %d", len(expired))
	}
	if !expired[0].Finished || expired[0].Won {
		t.Fatalf("超时应判负: %+v", expired[0])
	}
	if g, _ := s.Get("classic"); g.Finished {
		t.Fatal("经典对局不应被标记超时")
	}
	if g, _ := s.Get("done"); !g.Finished || !g.Won {
		t.Fatal("已结束对局的结果不应被改写")
	}
}

func TestRenderBoard_WithBadges(t *testing.T) {
	v := boardView{
		Title:       "WORDLE 5x6",
		Badges:      []string{"HARD", "CHAIN x2", "BLIND"},
		Length:      5,
		MaxAttempts: 6,
		Guesses:     []Guess{{Word: "crate", Marks: Evaluate("crane", "crate")}},
	}
	data, err := renderBoard(v)
	if err != nil {
		t.Fatalf("renderBoard: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("带徽标的棋盘不是合法 PNG: %v", err)
	}
	if got := renderBoardText(v); !strings.Contains(got, "HARD") {
		t.Fatalf("文本降级应包含徽标，实际:\n%s", got)
	}
}

func TestRuleSummaryAndLine(t *testing.T) {
	p := &Plugin{} // 未接入 i18n 时 t() 回退为 key，足以验证分支

	if got := p.ruleSummary(nil, &Game{}); got != "wordle.rules.classic" {
		t.Fatalf("经典对局应返回经典文案，实际 %q", got)
	}
	if got := p.ruleLine(nil, &Game{}); got != "" {
		t.Fatalf("经典对局不应有规则行，实际 %q", got)
	}

	g := &Game{Rules: RuleHard | RuleChain, ChainWins: 2}
	if got := p.ruleSummary(nil, g); got != "HARD · CHAIN" {
		t.Fatalf("规则摘要 = %q, 期望 HARD · CHAIN", got)
	}
	if got := p.ruleLine(nil, g); got == "" {
		t.Fatal("有规则时应返回规则行")
	}
	blitz := &Game{Rules: RuleBlitz, BlitzWindow: time.Minute, Deadline: timeNow().Add(time.Minute)}
	if got := p.ruleLine(nil, blitz); got == "" {
		t.Fatal("未超时的限时对局应显示倒计时徽标")
	}
}
