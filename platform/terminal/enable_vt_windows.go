//go:build windows

package terminal

import "golang.org/x/sys/windows"

// enableVT100 在 Windows 上启用虚拟终端处理（VT100 转义序列支持）。
//
// term.MakeRaw 会关闭 ENABLE_PROCESSED_OUTPUT，但不会打开
// ENABLE_VIRTUAL_TERMINAL_PROCESSING（0x0004），导致 refreshLine
// 使用的擦除/重绘 escape codes 无效。此函数补上这个标记。
func enableVT100(fd uintptr) {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	_ = windows.SetConsoleMode(windows.Handle(fd), mode)
}
