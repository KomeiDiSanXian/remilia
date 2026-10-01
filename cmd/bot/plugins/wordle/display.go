package wordle

import (
	"hash/fnv"
	"sort"
	"strconv"
)

// TileStyle 是单个格子在棋盘/键盘上的显示样式。
//
// 它与逻辑判定 [Mark] 解耦：Mark 永远记录真值（供胜负判定与困难模式校验），
// 而 TileStyle 可以在渲染时被颜色玩法改写（遮挡、延迟、说谎、新增颜色等），
// 从而保证"看到的不一定是真的"也不会污染对局逻辑。
type TileStyle uint8

const (
	// StyleAbsent 灰：字母不在谜底中。
	StyleAbsent TileStyle = iota
	// StylePresent 黄：字母在谜底中但位置不对。
	StylePresent
	// StyleCorrect 绿：字母与位置都正确。
	StyleCorrect
	// StyleNear 青：字母在谜底中且与真实位置相差 ±1（--near）。
	StyleNear
	// StyleRepeat 紫：谜底含该字母多个副本（--repeat）。
	StyleRepeat
	// StyleUnknown 中性格：字母与颜色都被隐藏（--unknown / --delayed）。
	StyleUnknown
	// StyleScore 计分色：整行同色编码绿色总数（--score-color）。
	StyleScore
)

// styleFromMark 把逻辑判定映射为基础显示样式。
func styleFromMark(m Mark) TileStyle {
	switch m {
	case Correct:
		return StyleCorrect
	case Present:
		return StylePresent
	default:
		return StyleAbsent
	}
}

// stylePriority 返回字母在键盘上保留"最有价值"状态时的优先级。
// 未知/计分等不携带字母状态的样式优先级最低。
func stylePriority(s TileStyle) int {
	switch s {
	case StyleCorrect:
		return 6
	case StyleNear:
		return 5
	case StyleRepeat:
		return 4
	case StylePresent:
		return 3
	case StyleAbsent:
		return 2
	default: // StyleUnknown / StyleScore
		return 1
	}
}

// displayStyles 计算整局所有猜测、所有棋盘的显示样式。
//
// 返回值的形状为 [猜测下标][棋盘子标][字母下标]；无猜测时返回 nil。
// 所有随机改写都由 游戏 ID + 猜测下标 + 棋盘子标 确定性派生，
// 保证同一对局的每次渲染（以及进程重启后）看到的是同一套"谎言"。
func displayStyles(g *Game) [][][]TileStyle {
	if g == nil || len(g.Guesses) == 0 {
		return nil
	}
	boards := g.BoardCount()
	out := make([][][]TileStyle, len(g.Guesses))

	mole := -1
	if g.Rules.Has(RuleMole) {
		if idx := hashedIndices("mole:"+g.ID, 1, g.Length); len(idx) == 1 {
			mole = idx[0]
		}
	}
	swap := g.Rules.Has(RuleSwapMeaning) && hashBit("swap:"+g.ID)

	for i, guess := range g.Guesses {
		word := []rune(guess.Word)
		row := make([][]TileStyle, boards)
		for bi := range boards {
			styles := make([]TileStyle, len(word))
			marks := guess.MarksFor(bi)
			for c := range styles {
				if c < len(marks) {
					styles[c] = styleFromMark(marks[c])
				} else {
					styles[c] = StyleAbsent
				}
			}

			answer := g.AnswerAt(bi)
			if g.Rules.Has(RuleRepeat) {
				applyRepeat(styles, word, answer)
			}
			if g.Rules.Has(RuleNear) {
				applyNear(styles, word, answer)
			}
			if swap {
				swapCorrectPresent(styles)
			}
			if g.Rules.Has(RuleInvert) {
				invertStyles(styles)
			}
			if g.Rules.Has(RuleHitOnly) {
				hitOnly(styles)
			}
			if mole >= 0 && mole < len(styles) {
				styles[mole] = StyleCorrect
			}

			seed := styleSeed(g.ID, i, bi)
			if g.Rules.Has(RuleDecoy) {
				applyDecoy(styles, g.DecoyCells, seed+":decoy")
			}
			if g.Rules.Has(RuleGlitch) {
				applyGlitch(styles, seed+":glitch")
			}
			if g.Rules.Has(RuleColorFog) {
				applyColorFog(styles, g.ColorFog, seed+":fog")
			}

			// 计分行级改写优先于逐格改写。
			if g.Rules.Has(RuleScoreColor) {
				for c := range styles {
					styles[c] = StyleScore
				}
			}
			// 衰减 / 延迟是整行改写，优先级最高。
			if g.Rules.Has(RuleDecay) && i < len(g.Guesses)-g.DecayRows {
				applyDecay(styles)
			}
			if g.Rules.Has(RuleDelayed) && i >= len(g.Guesses)-g.DelayedRows {
				applyUnknown(styles)
			}
			row[bi] = styles
		}
		out[i] = row
	}
	return out
}

// keyStyles 汇总键盘上每个字母的显示状态（依据显示样式而非真值）。
// 需要逐格字母时使用 g.Guesses；未知/计分样式不参与键盘着色。
func keyStyles(g *Game, styles [][][]TileStyle) map[rune]TileStyle {
	out := make(map[rune]TileStyle, 26)
	if g == nil || styles == nil {
		return out
	}
	for i, gs := range g.Guesses {
		if i >= len(styles) {
			break
		}
		word := []rune(gs.Word)
		for _, sts := range styles[i] {
			for c, st := range sts {
				if c >= len(word) || st == StyleUnknown || st == StyleScore {
					continue
				}
				ch := word[c]
				if cur, ok := out[ch]; !ok || stylePriority(st) > stylePriority(cur) {
					out[ch] = st
				}
			}
		}
	}
	return out
}

// ─── 单条规则的颜色变换 ────────────────────────────────────────────────────────

// applyNear 把"存在但位置不对"且与真实位置相邻的字母升级为青色。
func applyNear(styles []TileStyle, word []rune, answer string) {
	a := []rune(answer)
	for i, st := range styles {
		if st != StylePresent || i >= len(word) {
			continue
		}
		for j := range a {
			if a[j] == word[i] && (j == i-1 || j == i+1) {
				styles[i] = StyleNear
				break
			}
		}
	}
}

// applyRepeat 把谜底含多个副本的黄色字母升级为紫色。
func applyRepeat(styles []TileStyle, word []rune, answer string) {
	counts := make(map[rune]int, len(answer))
	for _, r := range answer {
		counts[r]++
	}
	for i, st := range styles {
		if st == StylePresent && i < len(word) && counts[word[i]] > 1 {
			styles[i] = StyleRepeat
		}
	}
}

// swapCorrectPresent 互换绿/黄的显示含义（--swap-meaning）。
func swapCorrectPresent(styles []TileStyle) {
	for i, st := range styles {
		switch st {
		case StyleCorrect:
			styles[i] = StylePresent
		case StylePresent:
			styles[i] = StyleCorrect
		}
	}
}

// invertStyles 互换绿/灰的显示含义（--invert）。
func invertStyles(styles []TileStyle) {
	for i, st := range styles {
		switch st {
		case StyleCorrect:
			styles[i] = StyleAbsent
		case StyleAbsent:
			styles[i] = StyleCorrect
		}
	}
}

// hitOnly 把绿色降级为黄色，只保留"字母在谜底中"的信息（--hit-only）。
func hitOnly(styles []TileStyle) {
	for i, st := range styles {
		if st == StyleCorrect {
			styles[i] = StylePresent
		}
	}
}

// applyDecoy 随机把若干灰色谎报为黄色（--decoy）。
func applyDecoy(styles []TileStyle, n int, seed string) {
	if n <= 0 {
		return
	}
	candidates := make([]int, 0, len(styles))
	for i, st := range styles {
		if st == StyleAbsent {
			candidates = append(candidates, i)
		}
	}
	for _, p := range hashedIndices(seed, n, len(candidates)) {
		styles[candidates[p]] = StylePresent
	}
}

// applyGlitch 随机翻转一格的颜色（--glitch）。
func applyGlitch(styles []TileStyle, seed string) {
	idx := hashedIndices(seed, 1, len(styles))
	if len(idx) == 0 {
		return
	}
	switch styles[idx[0]] {
	case StyleAbsent:
		styles[idx[0]] = StylePresent
	case StylePresent, StyleNear, StyleRepeat, StyleCorrect:
		styles[idx[0]] = StyleAbsent
	}
}

// applyColorFog 每行只保留 n 个最有价值的着色格，其余变灰（--colorfog）。
func applyColorFog(styles []TileStyle, n int, seed string) {
	if n <= 0 {
		return
	}
	type cell struct {
		idx  int
		rank uint64
	}
	cells := make([]cell, 0, len(styles))
	for i, st := range styles {
		if st == StyleAbsent || st == StyleUnknown || st == StyleScore {
			continue
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(seed))
		_, _ = h.Write([]byte(strconv.Itoa(i)))
		cells = append(cells, cell{idx: i, rank: uint64(stylePriority(st))*1_000_000_000_000 + h.Sum64()%1_000_000_000_000})
	}
	if len(cells) <= n {
		return
	}
	sort.Slice(cells, func(a, b int) bool { return cells[a].rank > cells[b].rank })
	for _, c := range cells[n:] {
		styles[c.idx] = StyleAbsent
	}
}

// applyDecay 把整行颜色衰减为灰（保留字母）。
func applyDecay(styles []TileStyle) {
	for i := range styles {
		styles[i] = StyleAbsent
	}
}

// applyUnknown 把整行隐藏为中性「?」。
func applyUnknown(styles []TileStyle) {
	for i := range styles {
		styles[i] = StyleUnknown
	}
}

// ─── 确定性随机 ───────────────────────────────────────────────────────────────

// hashedIndices 由 seed 稳定地派生 n 个互不相同的 [0,total) 下标。
func hashedIndices(seed string, n, total int) []int {
	if total <= 0 || n <= 0 {
		return nil
	}
	n = min(n, total)
	out := make([]int, 0, n)
	seen := make(map[int]struct{}, n)
	for salt := 0; len(out) < n && salt < 4096; salt++ {
		h := fnv.New64a()
		_, _ = h.Write([]byte(seed))
		_, _ = h.Write([]byte{byte(salt), byte(salt >> 8)})
		idx := int(h.Sum64() % uint64(total))
		if _, dup := seen[idx]; dup {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	return out
}

// hashBit 由 seed 稳定地派生一个布尔位。
func hashBit(seed string) bool {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return h.Sum64()&1 == 1
}

func styleSeed(id string, guessIdx, board int) string {
	return id + "|" + strconv.Itoa(guessIdx) + "|" + strconv.Itoa(board)
}
