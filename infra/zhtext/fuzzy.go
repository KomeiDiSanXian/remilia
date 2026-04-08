package zhtext

import (
	"github.com/lithammer/fuzzysearch/fuzzy"
)

// ── 基础模糊匹配 ──────────────────────────────────────────────────────────────

// Match 报告 source 是否模糊匹配 target（区分大小写）。
//
// "模糊匹配"：source 中的字符按序出现在 target 中（不要求连续）。
//
// 示例：
//
//	Match("天气", "天气预报")   // true
//	Match("气天", "天气预报")   // false（顺序不同）
//	Match("tbr", "textbook")    // true
func Match(source, target string) bool {
	return fuzzy.Match(source, target)
}

// MatchFold 报告 source 是否模糊匹配 target（不区分大小写）。
func MatchFold(source, target string) bool {
	return fuzzy.MatchFold(source, target)
}

// ── 列表过滤 ──────────────────────────────────────────────────────────────────

// Find 在 targets 中查找所有模糊匹配 source 的项（区分大小写）。
//
// 示例：
//
//	Find([]string{"天气预报", "天氣預報", "温度查询"}, "天气")
//	// → ["天气预报"]  （"天氣預報" 使用繁体，字不同，不匹配）
func Find(targets []string, source string) []string {
	return fuzzy.Find(source, targets)
}

// FindFold 在 targets 中查找所有模糊匹配 source 的项（不区分大小写）。
//
// 对英文命令模糊搜索更友好：
//
//	FindFold([]string{"WeatherForecast", "TempQuery"}, "weather")
//	// → ["WeatherForecast"]
func FindFold(targets []string, source string) []string {
	return fuzzy.FindFold(source, targets)
}

// ── 相关度排序 ────────────────────────────────────────────────────────────────

// RankMatch 带相关度分数的匹配结果。
type RankMatch = fuzzy.Rank

// RankFind 在 targets 中查找所有模糊匹配 source 的项，
// 并按相关度排序（区分大小写）。
//
// 返回 []RankMatch，每项包含 Source（原始词）和 Distance（越小越相关）。
//
// 示例：
//
//	results := RankFind([]string{"天气预报", "天气查询", "其他"}, "天气")
//	// 按 Distance 升序排列
func RankFind(targets []string, source string) []RankMatch {
	return fuzzy.RankFind(source, targets)
}

// RankFindFold 在 targets 中查找所有模糊匹配 source 的项，
// 并按相关度排序（不区分大小写）。
func RankFindFold(targets []string, source string) []RankMatch {
	return fuzzy.RankFindFold(source, targets)
}

// Rank 计算 source 相对于 target 的模糊匹配距离（Levenshtein 距离）。
// 返回 -1 表示不匹配。
func Rank(source, target string) int {
	return fuzzy.LevenshteinDistance(source, target)
}
