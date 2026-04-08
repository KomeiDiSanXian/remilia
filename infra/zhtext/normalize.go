package zhtext

import (
	"strings"
	"unicode"
)

// FullToHalf 将字符串中的全角 ASCII 可打印字符转换为半角。
//
// 转换范围：
//   - U+FF01 ！→ U+0021 !（全角 ASCII 可打印字符，共 94 个）
//   - U+3000 　→ U+0020 （全角空格 → 半角空格）
//
// 示例：
//
//	FullToHalf("ＡＢＣＤ１２３／ping")
//	// → "ABCD123/ping"
func FullToHalf(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0x3000:
			b.WriteByte(' ')
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// HalfToFull 将字符串中的半角 ASCII 可打印字符转换为全角。
//
// 转换范围：
//   - U+0021 ! → U+FF01 ！（共 94 个）
//   - U+0020 空格 → U+3000 全角空格
//
// 示例:
//
//	HalfToFull("/ping 123")
//	// → "／ｐｉｎｇ　１２３"
func HalfToFull(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 3) // 每个 ASCII 字符可能扩展为 3 字节 UTF-8
	for _, r := range s {
		switch {
		case r == 0x20:
			b.WriteRune(0x3000)
		case r >= 0x21 && r <= 0x7E:
			b.WriteRune(r + 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeCJK 执行面向 Bot 输入规范化的组合操作：
//  1. 全角 ASCII → 半角
//  2. 去除首尾空白（含 Unicode 空白）
//
// 适合在命令解析前调用，确保 "/ping"、"／ping"、"/ ping " 能统一匹配。
//
// 示例：
//
//	NormalizeCJK("　／天气　上海　")
//	// → "/天气　上海"   （注意：字符串内部的全角空格不被折叠，只转 ASCII 全角）
func NormalizeCJK(s string) string {
	return strings.TrimSpace(FullToHalf(s))
}

// CollapseSpaces 将字符串中的连续空白（含 Unicode 空白字符）折叠为单个空格，
// 并去除首尾空白。
//
// 示例：
//
//	CollapseSpaces("  hello   world  ")
//	// → "hello world"
func CollapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	started := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if started {
				prevSpace = true
			}
			continue
		}
		if prevSpace {
			b.WriteByte(' ')
			prevSpace = false
		}
		started = true
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeInput 全量规范化：全角→半角 + 折叠空白 + 首尾去空白。
// 适合对用户输入做最全面的预处理后再进行命令匹配。
//
// 示例：
//
//	NormalizeInput("　／天气　　上海　")
//	// → "/天气 上海"
func NormalizeInput(s string) string {
	return CollapseSpaces(FullToHalf(s))
}
