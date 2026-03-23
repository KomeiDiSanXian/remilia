//go:build darwin

package textimage

import "os"

// sysCJKFonts returns macOS CJK font candidates in priority order.
//
// Priority: PingFang SC → Heiti SC → Arial Unicode MS → Hiragino Sans GB →
// Apple SD Gothic Neo → MS YaHei (Office install)
func sysCJKFonts() []string {
	return []string{
		"/System/Library/Fonts/PingFang.ttc",           // PingFang SC
		"/System/Library/Fonts/STHeiti Medium.ttc",     // Heiti SC
		"/Library/Fonts/Arial Unicode.ttf",             // Arial Unicode MS
		"/System/Library/Fonts/Hiragino Sans GB.ttc",   // Hiragino Sans GB
		"/System/Library/Fonts/AppleSDGothicNeo.ttc",   // Apple SD Gothic Neo
		"/Library/Fonts/Microsoft/Microsoft YaHei.ttf", // MS YaHei (Office)
	}
}

// systemFontDirs returns the standard macOS font directories.
func systemFontDirs() []string {
	return []string{
		"/System/Library/Fonts",
		"/Library/Fonts",
		os.Getenv("HOME") + "/Library/Fonts",
	}
}
