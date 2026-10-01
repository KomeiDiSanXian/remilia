package wordle

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

// RuleSet 是一局附加玩法的位集合；零值表示经典规则。
//
// 设计取舍：所有玩法都实现为可叠加的"修饰符"，而不是新增模式枚举。
// 这样任意几条规则都能自由组合，命令解析与结算流程保持单一入口，
// 避免"玩法数 × 参数数"的分支爆炸。
type RuleSet uint32

const (
	// RuleHard 困难模式：已揭示的绿/黄字母必须复用。
	RuleHard RuleSet = 1 << iota
	// RuleBlitz 限时模式：每次作答有截止时间，超时判负。
	RuleBlitz
	// RuleChain 连锁模式：猜中后自动进入下一题，直到失败或放弃。
	RuleChain
	// RuleHintCost 提示经济：每次提示消耗一次机会。
	RuleHintCost
	// RuleChaos 混沌模式：开局随机附加 1-2 个修饰符。
	RuleChaos
	// RuleRace 抢分模式：群内按解谜贡献计分。
	RuleRace
	// RuleBlind 盲猜：黄色（存在但位置不对）按灰色显示。
	RuleBlind
	// RuleFog 迷雾：只保留最近若干行判定，之前的行不再显示。
	RuleFog
	// RuleObscure 冷门词库：谜底只从低频词中抽取，显著提高难度。
	RuleObscure
	// RuleGauntlet 车轮战：连锁模式下每解出一题就少一次机会（下限 2）。
	RuleGauntlet
	// ── 颜色玩法：显示层（真值仍保留在 Guess.Marks，仅影响观感） ──
	//
	// RuleInvert 反转语义：绿（位置正确）与灰（不存在）互换显示。
	RuleInvert
	// RuleHitOnly 只留命中：绿+黄合并为黄，位置信息全部丢失（比盲猜更狠）。
	RuleHitOnly
	// RuleColorFog 颜色预算：每行最多保留 ColorFog 个着色格子，其余变灰。
	RuleColorFog
	// RuleDecay 颜色衰减：只有最近 DecayRows 行保留颜色，更早的行变灰（字母仍在）。
	RuleDecay
	// RuleUnknown 未知格：每行随机 UnknownCells 格显示为中性「?」。
	RuleUnknown
	// RuleHiddenKey 隐藏键盘颜色（键盘仍绘制，但不随判定着色）。
	RuleHiddenKey
	// RuleDelayed 延迟着色：最新的 DelayedRows 行暂不显示颜色。
	RuleDelayed
	// ── 颜色玩法：对抗层（显示层随机说谎） ──
	//
	// RuleDecoy 诱饵：每行随机 DecoyCells 个灰色谎报为黄色。
	RuleDecoy
	// RuleGlitch 故障：每行随机翻转一格的显示颜色。
	RuleGlitch
	// RuleMole 内鬼：每局随机一个位置永远显示为绿色。
	RuleMole
	// RuleSwapMeaning 语义互换：每局随机决定绿/黄含义互换。
	RuleSwapMeaning
	// ── 颜色玩法：新增颜色（逻辑层，改变判定语义） ──
	//
	// RuleNear 邻近色（青）：字母在谜底中且与真实位置相差 ±1。
	RuleNear
	// RuleRepeat 重复色（紫）：谜底含该字母多个副本时，黄色用紫色替代。
	RuleRepeat
	// RuleScoreColor 计分色：整行同色表示绿色总数，位置信息全部丢失。
	RuleScoreColor
)

// ruleList 是规则的稳定顺序表，解析/展示/帮助共用同一份数据。
// 说明文案由 i18n 提供（key: wordle.rule.<name>），避免在此硬编码中文。
var ruleList = []struct {
	Rule  RuleSet
	Name  string
	Label string
}{
	{RuleHard, "hard", "HARD"},
	{RuleBlitz, "blitz", "BLITZ"},
	{RuleChain, "chain", "CHAIN"},
	{RuleHintCost, "hint-cost", "HINT-COST"},
	{RuleChaos, "chaos", "CHAOS"},
	{RuleRace, "race", "RACE"},
	{RuleBlind, "blind", "BLIND"},
	{RuleFog, "fog", "FOG"},
	{RuleObscure, "obscure", "OBSCURE"},
	{RuleGauntlet, "gauntlet", "GAUNTLET"},
	{RuleInvert, "invert", "INVERT"},
	{RuleHitOnly, "hit-only", "HIT-ONLY"},
	{RuleColorFog, "colorfog", "COLORFOG"},
	{RuleDecay, "decay", "DECAY"},
	{RuleUnknown, "unknown", "UNKNOWN"},
	{RuleHiddenKey, "hidden-key", "HIDDEN-KEY"},
	{RuleDelayed, "delayed", "DELAYED"},
	{RuleDecoy, "decoy", "DECOY"},
	{RuleGlitch, "glitch", "GLITCH"},
	{RuleMole, "mole", "MOLE"},
	{RuleSwapMeaning, "swap-meaning", "SWAP"},
	{RuleNear, "near", "NEAR"},
	{RuleRepeat, "repeat", "REPEAT"},
	{RuleScoreColor, "score-color", "SCORE"},
}

// Has 报告是否启用了 f 中的任意一条规则。
func (r RuleSet) Has(f RuleSet) bool { return r&f != 0 }

// Names 返回已启用规则的标识（稳定顺序），用于存储与日志。
func (r RuleSet) Names() []string {
	out := make([]string, 0, len(ruleList))
	for _, it := range ruleList {
		if r.Has(it.Rule) {
			out = append(out, it.Name)
		}
	}
	return out
}

// Labels 返回已启用规则的 ASCII 徽标（稳定顺序），用于图片头部。
func (r RuleSet) Labels() []string {
	out := make([]string, 0, len(ruleList))
	for _, it := range ruleList {
		if r.Has(it.Rule) {
			out = append(out, it.Label)
		}
	}
	return out
}

// String 返回逗号分隔的规则标识（零值为空串）。
func (r RuleSet) String() string { return strings.Join(r.Names(), ",") }

// ruleByName 按标识查找规则。
func ruleByName(name string) (RuleSet, bool) {
	for _, it := range ruleList {
		if it.Name == name {
			return it.Rule, true
		}
	}
	return 0, false
}

// ParseRuleList 解析逗号/空白/竖线分隔的规则标识；未知项返回错误。
//
// 空串返回零值（经典规则），不报错。
func ParseRuleList(s string) (RuleSet, error) {
	var out RuleSet
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '|' || r == '\t' || r == '\n'
	}) {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		rule, ok := ruleByName(f)
		if !ok {
			return 0, fmt.Errorf("wordle: 未知规则 %q", f)
		}
		out |= rule
	}
	return out, nil
}

// allRuleNames 返回全部规则标识（稳定顺序），供帮助文案拼装。
func allRuleNames() []string {
	out := make([]string, 0, len(ruleList))
	for _, it := range ruleList {
		out = append(out, it.Name)
	}
	return out
}

// ─── 混沌修饰符 ────────────────────────────────────────────────────────────────

// Modifier 是混沌模式附加的单个修饰符，可叠加。
type Modifier uint8

const (
	// ModBlind 盲猜：黄色（存在但位置不对）按灰色显示。
	ModBlind Modifier = 1 << iota
	// ModNoRepeat 谜底不含重复字母。
	ModNoRepeat
	// ModDouble 每次猜测消耗 2 次机会。
	ModDouble
	// ModNoHint 本局禁用提示。
	ModNoHint
)

// modList 是修饰符的稳定顺序表。
// 说明文案由 i18n 提供（key: wordle.mod.<name>）。
var modList = []struct {
	Mod   Modifier
	Name  string
	Label string
}{
	{ModBlind, "blind", "BLIND"},
	{ModNoRepeat, "norepeat", "NOREPEAT"},
	{ModDouble, "double", "COSTx2"},
	{ModNoHint, "nohint", "NOHINT"},
}

// Has 报告是否带有某个修饰符。
func (m Modifier) Has(x Modifier) bool { return m&x != 0 }

// Names 返回修饰符标识列表（稳定顺序）。
func (m Modifier) Names() []string {
	out := make([]string, 0, len(modList))
	for _, it := range modList {
		if m.Has(it.Mod) {
			out = append(out, it.Name)
		}
	}
	return out
}

// Labels 返回修饰符徽标列表（稳定顺序）。
func (m Modifier) Labels() []string {
	out := make([]string, 0, len(modList))
	for _, it := range modList {
		if m.Has(it.Mod) {
			out = append(out, it.Label)
		}
	}
	return out
}

// String 返回逗号分隔的修饰符标识。
func (m Modifier) String() string { return strings.Join(m.Names(), ",") }

// modByName 按标识查找修饰符。
func modByName(name string) (Modifier, bool) {
	for _, it := range modList {
		if it.Name == name {
			return it.Mod, true
		}
	}
	return 0, false
}

// ParseModifiers 解析逗号分隔的修饰符标识（用于从存储还原）。
func ParseModifiers(s string) Modifier {
	var out Modifier
	for f := range strings.SplitSeq(s, ",") {
		if mod, ok := modByName(strings.ToLower(strings.TrimSpace(f))); ok {
			out |= mod
		}
	}
	return out
}

// clearableModifiers 是混沌模式会抽到的修饰符（避免与已选规则语义重复）。
func clearableModifiers(rules RuleSet) []Modifier {
	out := make([]Modifier, 0, len(modList))
	for _, it := range modList {
		// 避免叠加与已选规则语义重复或互相打架的修饰符。
		if it.Mod == ModNoHint && rules.Has(RuleHintCost) {
			continue
		}
		if it.Mod == ModBlind && rules.Has(RuleBlind) {
			continue
		}
		out = append(out, it.Mod)
	}
	return out
}

// pickChaos 随机抽取 1-2 个不重复的修饰符。
func pickChaos(rules RuleSet) Modifier {
	pool := clearableModifiers(rules)
	if len(pool) == 0 {
		return 0
	}
	order := rand.Perm(len(pool))
	n := min(rand.IntN(2)+1, len(pool))
	var out Modifier
	for i := range n {
		out |= pool[order[i]]
	}
	return out
}

// allModLabels 返回全部修饰符的展示标签（稳定顺序），供帮助文案拼装。
func allModLabels() []string {
	out := make([]string, 0, len(modList))
	for _, it := range modList {
		out = append(out, it.Label)
	}
	return out
}

// allModNames 返回全部修饰符标识（稳定顺序）。
func allModNames() []string {
	out := make([]string, 0, len(modList))
	for _, it := range modList {
		out = append(out, it.Name)
	}
	return out
}

// ─── 困难模式校验 ──────────────────────────────────────────────────────────────

// HardViolation 描述一次困难模式违规；零值表示不违规。
type HardViolation struct {
	// Position 是必须保持不变的 1 起位置；0 表示无此违规。
	Position int
	// Letter 是必须出现在猜测中的字母；0 表示无此违规。
	Letter rune
}

// OK 报告是否没有违规。
func (v HardViolation) OK() bool { return v.Position == 0 && v.Letter == 0 }

// checkHardMode 校验 guess 是否满足困难模式：
//   - 曾经猜中（绿色）的位置必须保持同一字母；
//   - 提示揭示的位置必须保持同一字母；
//   - 曾经出现在答案里但位置不对（黄色）的字母必须继续使用。
func checkHardMode(g *Game, guess []rune) HardViolation {
	if len(guess) != g.Length {
		return HardViolation{}
	}
	answer := []rune(g.AnswerAt(0))

	for _, prev := range g.Guesses {
		pr := []rune(prev.Word)
		for i, mk := range prev.Marks {
			if mk == Correct && i < len(pr) && i < len(guess) && guess[i] != pr[i] {
				return HardViolation{Position: i + 1}
			}
		}
	}
	for _, i := range g.Revealed {
		if i >= 0 && i < len(answer) && i < len(guess) && guess[i] != answer[i] {
			return HardViolation{Position: i + 1}
		}
	}

	required := make(map[rune]struct{}, 8)
	for _, prev := range g.Guesses {
		pr := []rune(prev.Word)
		for i, mk := range prev.Marks {
			if mk == Present && i < len(pr) {
				required[pr[i]] = struct{}{}
			}
		}
	}
	for _, i := range g.Revealed {
		if i >= 0 && i < len(answer) {
			required[answer[i]] = struct{}{}
		}
	}
	for r := range required {
		if !slices.Contains(guess, r) {
			return HardViolation{Letter: r}
		}
	}
	return HardViolation{}
}

// blindMarks 把黄色（Present）降级为灰色（Absent），用于盲猜修饰符。
func blindMarks(marks []Mark) []Mark {
	out := make([]Mark, len(marks))
	for i, m := range marks {
		if m == Present {
			out[i] = Absent
			continue
		}
		out[i] = m
	}
	return out
}

// raceGain 统计本次猜测新解锁的绿色位置数（用于抢分模式）。
func raceGain(before map[int]bool, marks []Mark) int {
	gain := 0
	for i, m := range marks {
		if m == Correct && !before[i] {
			gain++
		}
	}
	return gain
}
