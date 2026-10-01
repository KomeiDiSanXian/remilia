// Package wordle 提供 Wordle 猜词小游戏插件。
//
// 所有玩法都挂在 /wordle 之下（子命令而非摊平的顶层命令）：
//
//	/wordle [--length N] [--tries N] [--daily] [--scope user|group]  开始新局
//	/wordle guess <单词>   提交一次猜测
//	/wordle giveup         放弃并公布答案
//	/wordle hint           揭示一个尚未猜中的字母位置
//	/wordle board          重新发送当前棋盘
//	/wordle stats [@用户]  查看统计
//	/wordle top            全局排行榜
//	/wordle lang <locale>  切换语言（zh-CN / en-US）
//
// 依赖: i18n（多语言文案）、storage（统计与每日锁持久化）。
package wordle

import (
	"embed"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
)

//go:embed words/*.txt
var wordsFS embed.FS

// SupportedLengths 随包词库支持的单词长度。
var SupportedLengths = []int{4, 5, 6, 7}

const (
	// DefaultLength 默认单词长度。
	DefaultLength = 5
	// DefaultMaxAttempts 默认答题次数。
	DefaultMaxAttempts = 6
	// minAttempts / maxAttempts 用户可指定的答题次数范围。
	minAttempts = 1
	maxAttempts = 12
)

// IsSupportedLength 报告给定长度是否有随包词库可用。
func IsSupportedLength(n int) bool {
	return slices.Contains(SupportedLengths, n)
}

// WordBank 单一长度的词库。
type WordBank struct {
	Length  int
	answers []string            // 谜底池（加载时已去重）
	obscure []string            // 冷门谜底池 = 合法输入 - 常用谜底（已排序，保证每日题稳定）
	allowed map[string]struct{} // 合法输入集合（含全部谜底）
}

var bankCache sync.Map // int -> *WordBank

// loadWordBank 加载并缓存指定长度的词库。
func loadWordBank(length int) (*WordBank, error) {
	if v, ok := bankCache.Load(length); ok {
		return v.(*WordBank), nil
	}
	bank, err := readWordBank(length)
	if err != nil {
		return nil, err
	}
	bankCache.Store(length, bank)
	return bank, nil
}

func readWordBank(length int) (*WordBank, error) {
	answers, err := readWordList(fmt.Sprintf("words/answers_%d.txt", length), length)
	if err != nil {
		return nil, err
	}
	if len(answers) == 0 {
		return nil, fmt.Errorf("wordle: 词库 answers_%d 为空", length)
	}
	allowed, err := readWordList(fmt.Sprintf("words/allowed_%d.txt", length), length)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(allowed)+len(answers))
	for _, w := range allowed {
		set[w] = struct{}{}
	}
	for _, w := range answers {
		set[w] = struct{}{} // 谜底必须总能被猜出
	}
	answerSet := make(map[string]struct{}, len(answers))
	for _, w := range answers {
		answerSet[w] = struct{}{}
	}
	obscure := make([]string, 0, len(set)-len(answers))
	for w := range set {
		if _, isAnswer := answerSet[w]; !isAnswer {
			obscure = append(obscure, w)
		}
	}
	slices.Sort(obscure)
	return &WordBank{Length: length, answers: answers, obscure: obscure, allowed: set}, nil
}

func readWordList(name string, length int) ([]string, error) {
	data, err := wordsFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("wordle: 读取词库 %s: %w", name, err)
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, 4096)
	for line := range strings.SplitSeq(string(data), "\n") {
		w := strings.ToLower(strings.TrimSpace(line))
		if len(w) != length || !isAlpha(w) {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out, nil
}

// Pick 随机返回一个谜底。
func (b *WordBank) Pick() string {
	return b.answers[rand.IntN(len(b.answers))]
}

// Pool 返回指定难度下的谜底池：obscure 为 true 时返回冷门池（为空则回退常用池）。
func (b *WordBank) Pool(obscure bool) []string {
	if obscure && len(b.obscure) > 0 {
		return b.obscure
	}
	return b.answers
}

// ObscureSize 返回冷门谜底池大小。
func (b *WordBank) ObscureSize() int { return len(b.obscure) }

// PickFrom 从谜底池中随机返回一个词；池为空时返回空串。
func PickFrom(pool []string) string {
	if len(pool) == 0 {
		return ""
	}
	return pool[rand.IntN(len(pool))]
}

// PickManyFrom 从谜底池中随机返回 n 个互不相同的词（n 超过池大小时返回整个池的乱序）。
func PickManyFrom(pool []string, n int) []string {
	if n <= 0 || len(pool) == 0 {
		return nil
	}
	n = min(n, len(pool))
	order := rand.Perm(len(pool))
	out := make([]string, 0, n)
	for _, i := range order[:n] {
		out = append(out, pool[i])
	}
	return out
}

// DailyIndexIn 在给定谜底池上按日期推导稳定下标（同一日期跨进程一致）。
func DailyIndexIn(pool []string, day string) int {
	if len(pool) == 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("wordle:" + day))
	return int(h.Sum64() % uint64(len(pool)))
}

// PickWhere 从谜底池中随机返回一个满足 pred 的谜底；无候选时 ok 为 false。
func (b *WordBank) PickWhere(pred func(string) bool) (string, bool) {
	candidates := make([]string, 0, 64)
	for _, w := range b.answers {
		if pred(w) {
			candidates = append(candidates, w)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[rand.IntN(len(candidates))], true
}

// HasDistinctLetters 报告单词是否不含重复字母（用于 NOREPEAT 修饰符）。
func HasDistinctLetters(w string) bool {
	seen := make(map[rune]struct{}, len(w))
	for _, r := range w {
		if _, dup := seen[r]; dup {
			return false
		}
		seen[r] = struct{}{}
	}
	return true
}

// AnswerAt 返回指定下标的谜底，下标按池大小取模。
func (b *WordBank) AnswerAt(i int) string {
	n := len(b.answers)
	return b.answers[((i%n)+n)%n]
}

// DailyIndex 根据日期推导当日谜底下标，保证同一日期跨进程、跨重启稳定。
func (b *WordBank) DailyIndex(day string) int {
	return DailyIndexIn(b.answers, day)
}

// IsAllowed 报告单词是否在合法输入集合中。
func (b *WordBank) IsAllowed(word string) bool {
	_, ok := b.allowed[word]
	return ok
}

// Size 返回谜底池大小。
func (b *WordBank) Size() int { return len(b.answers) }

// isAlpha 报告 s 是否仅由小写英文字母组成。
func isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}
