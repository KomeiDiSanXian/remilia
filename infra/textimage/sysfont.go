package textimage

import "os"

// SystemCJKFontPath 返回当前操作系统上已安装的合适 CJK（中文/日文/韩文）字体
// 的绝对路径；若未找到则返回空字符串。
//
// 各平台的搜索优先级定义在同级的 sysfont_*.go 文件中，通过条件编译选择：
//
//   - Windows → sysfont_windows.go
//   - macOS   → sysfont_darwin.go
//   - 其他    → sysfont_unix.go
func SystemCJKFontPath() string {
	return firstExisting(sysCJKFonts()...)
}

// SystemFontPath 在平台标准字体目录中按（大小写不敏感的）文件名查找字体。
// 未找到时返回 ""。
func SystemFontPath(filename string) string {
	for _, dir := range systemFontDirs() {
		p := dir + string(os.PathSeparator) + filename
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// ─── 辅助函数 ─────────────────────────────────────────────────────────────────

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
