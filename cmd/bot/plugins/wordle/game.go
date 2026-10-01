package wordle

import "strings"

// Mark 表示单个字母相对谜底的命中状态。
type Mark uint8

const (
	// Absent 字母不在谜底中（灰）。
	Absent Mark = iota
	// Present 字母在谜底中但位置不对（黄）。
	Present
	// Correct 字母与位置都正确（绿）。
	Correct
)

// Guess 一次猜测及其逐字母判定。
type Guess struct {
	Word string
	// Marks 是第一个谜底的判定（单谜底即全部）。
	Marks []Mark
	// Boards 多谜底模式下每个谜底的判定，长度等于棋盘数；单谜底时长度为 1。
	Boards [][]Mark
}

// MarksFor 返回第 board 个谜底的判定；越界时回退到 Marks。
func (g Guess) MarksFor(board int) []Mark {
	if board >= 0 && board < len(g.Boards) {
		return g.Boards[board]
	}
	if board == 0 {
		return g.Marks
	}
	return nil
}

// Solved 报告这次猜测是否完全命中。
func (g Guess) Solved() bool {
	if len(g.Marks) == 0 {
		return false
	}
	for _, m := range g.Marks {
		if m != Correct {
			return false
		}
	}
	return true
}

// Evaluate 按 Wordle 规则判定 guess 相对 answer 的结果。
//
// 采用标准两遍算法，正确处理重复字母：第一遍标记位置正确的字母并消耗
// 字母计数，第二遍再按剩余计数标记"存在但位置不对"，避免重复字母被
// 多标为黄色。answer 与 guess 长度不一致时返回等长的全 Absent。
func Evaluate(answer, guess string) []Mark {
	a := []rune(strings.ToLower(answer))
	g := []rune(strings.ToLower(guess))
	marks := make([]Mark, len(g))
	if len(a) != len(g) {
		return marks
	}

	var counts [26]int
	for _, r := range a {
		if i := letterIndex(r); i >= 0 {
			counts[i]++
		}
	}
	for i := range g {
		if g[i] == a[i] {
			marks[i] = Correct
			if idx := letterIndex(g[i]); idx >= 0 {
				counts[idx]--
			}
		}
	}
	for i := range g {
		if marks[i] == Correct {
			continue
		}
		if idx := letterIndex(g[i]); idx >= 0 && counts[idx] > 0 {
			marks[i] = Present
			counts[idx]--
		}
	}
	return marks
}

// KeyStates 汇总键盘上每个字母的最佳已知状态（Correct > Present > Absent）。
//
// board 为 -1 时汇总全部棋盘（多谜底模式共用一个键盘）。
func KeyStates(guesses []Guess, board int) map[rune]Mark {
	out := make(map[rune]Mark, 26)
	for _, g := range guesses {
		boards := g.Boards
		if len(boards) == 0 {
			boards = [][]Mark{g.Marks}
		}
		for bi, marks := range boards {
			if board >= 0 && bi != board {
				continue
			}
			runes := []rune(g.Word)
			for i, r := range runes {
				if i >= len(marks) {
					break
				}
				cur, ok := out[r]
				if !ok || marks[i] > cur {
					out[r] = marks[i]
				}
			}
		}
	}
	return out
}

// letterIndex 返回 'a'-'z' 的下标，非小写字母返回 -1。
func letterIndex(r rune) int {
	if r < 'a' || r > 'z' {
		return -1
	}
	return int(r - 'a')
}
