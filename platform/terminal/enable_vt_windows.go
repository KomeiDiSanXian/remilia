//go:build windows

package terminal

import "golang.org/x/sys/windows"

// enableVT100 在 Windows 上启用虚拟终端处理（VT100 转义序列支持）。
//
// term.MakeRaw 会关闭 ENABLE_PROCESSED_OUTPUT，但不会打开
// ENABLE_VIRTUAL_TERMINAL_PROCESSING（0x0004），导致 refreshLine
// 使用的擦除/重绘 escape codes 无效。此函数补上这个标记。
//
// 注意：必须传入**输出**句柄（os.Stdout.Fd()）。
// Windows 控制台的输入模式与输出模式是两套独立的标志位命名空间，
// 且数值有重叠：ENABLE_VIRTUAL_TERMINAL_PROCESSING 与
// ENABLE_ECHO_INPUT 同为 0x0004。把该值 OR 进**输入**句柄的 mode，
// 既不会开启任何 VT 输出处理，反而会把 term.MakeRaw 刚关掉的回显重新打开
// （或因 ENABLE_ECHO_INPUT 需与 ENABLE_LINE_INPUT 同时设置而直接失败）。
func enableVT100(fd uintptr) {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	_ = windows.SetConsoleMode(windows.Handle(fd), mode)
}
