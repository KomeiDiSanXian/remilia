package engine

import "unicode"

// SplitCommandPattern 将命令模式拆分为前缀和命令名。
//
// 前缀由开头的连续非字母非数字字符组成。
// 例如:
//
//	"/help"   → ("/", "help")
//	"!!admin" → ("!!", "admin")
//	"$#ping"  → ("$#", "ping")
//	"帮助"     → ("", "帮助")
//	"!@#test" → ("!@#", "test")
//
// 如果整个模式都不含字母数字（如 "!!!"），则 name 为空字符串。
func SplitCommandPattern(pattern string) (prefix, name string) {
	for i, r := range pattern {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return pattern[:i], pattern[i:]
		}
	}
	return pattern, ""
}
