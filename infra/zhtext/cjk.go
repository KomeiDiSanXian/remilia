package zhtext

import (
	"unicode"
)

// CJK Unified Ideographs 的 Unicode 区间（最常用的主要范围）。
// 含基本 CJK (U+4E00–U+9FFF)、扩展 A/B (U+3400–U+4DBF, U+20000–U+2A6DF) 等。
var cjkRangeTable = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x2E80, Hi: 0x2EFF, Stride: 1}, // CJK Radicals Supplement
		{Lo: 0x2F00, Hi: 0x2FDF, Stride: 1}, // Kangxi Radicals
		{Lo: 0x3000, Hi: 0x303F, Stride: 1}, // CJK Symbols and Punctuation
		{Lo: 0x3040, Hi: 0x309F, Stride: 1}, // Hiragana
		{Lo: 0x30A0, Hi: 0x30FF, Stride: 1}, // Katakana
		{Lo: 0x3100, Hi: 0x312F, Stride: 1}, // Bopomofo
		{Lo: 0x3400, Hi: 0x4DBF, Stride: 1}, // CJK Extension A
		{Lo: 0x4E00, Hi: 0x9FFF, Stride: 1}, // CJK Unified Ideographs (main)
		{Lo: 0xF900, Hi: 0xFAFF, Stride: 1}, // CJK Compatibility Ideographs
		{Lo: 0xFE30, Hi: 0xFE4F, Stride: 1}, // CJK Compatibility Forms
		{Lo: 0xFF00, Hi: 0xFFEF, Stride: 1}, // Halfwidth and Fullwidth Forms
	},
	R32: []unicode.Range32{
		{Lo: 0x20000, Hi: 0x2A6DF, Stride: 1}, // CJK Extension B
		{Lo: 0x2A700, Hi: 0x2B73F, Stride: 1}, // CJK Extension C
		{Lo: 0x2B740, Hi: 0x2B81F, Stride: 1}, // CJK Extension D
		{Lo: 0x2B820, Hi: 0x2CEAF, Stride: 1}, // CJK Extension E
	},
}

// IsHan 判断单个 rune 是否为 CJK 汉字（包含常见 CJK 扩展区块）。
//
// 注意：本函数范围覆盖 CJK 统一汉字及扩展，
// 但不包含日文假名、谚文等（可通过 unicode.Is(unicode.Han, r) 做更精确的汉字判断）。
func IsHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// IsCJK 判断 rune 是否属于 CJK 相关字符（含汉字、片假名、平假名等东亚字符）。
func IsCJK(r rune) bool {
	return unicode.Is(cjkRangeTable, r)
}

// ContainsChinese 报告字符串 s 中是否包含至少一个 CJK 汉字。
func ContainsChinese(s string) bool {
	for _, r := range s {
		if IsHan(r) {
			return true
		}
	}
	return false
}

// ChineseCharCount 返回字符串 s 中 CJK 汉字的数量（不含假名、标点等）。
func ChineseCharCount(s string) int {
	n := 0
	for _, r := range s {
		if IsHan(r) {
			n++
		}
	}
	return n
}

// SplitCJK 将字符串按"CJK字符 / 非CJK字符"的边界切分，
// 返回由连续相同类型字符组成的 token 列表。
//
// 示例：
//
//	SplitCJK("Hello世界foo")
//	// → ["Hello", "世界", "foo"]
//
// 这提供了一种不依赖词典的简易 CJK tokenizer，
// 适合关键字提取、命令参数分割等轻量场景。
func SplitCJK(s string) []string {
	if s == "" {
		return nil
	}
	var tokens []string
	var cur []rune
	var curCJK bool
	first := true

	for _, r := range s {
		isCJK := IsHan(r)
		if first {
			curCJK = isCJK
			first = false
		}
		if isCJK != curCJK {
			if len(cur) > 0 {
				tokens = append(tokens, string(cur))
			}
			cur = cur[:0]
			curCJK = isCJK
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		tokens = append(tokens, string(cur))
	}
	return tokens
}

// IsFullWidth 判断 rune 是否为全角字符（U+FF01–U+FF60 以及 U+FFE0–U+FFE6）。
func IsFullWidth(r rune) bool {
	return (r >= 0xFF01 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6)
}
