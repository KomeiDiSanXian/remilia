//go:build windows

package mcp

// transport_stdio_windows.go — stdio 传输的 Windows 平台分支。
//
// Windows 的进程树终止走 killProcessTree 里的 taskkill /F /T（连并进程树），
// 此文件只提供 no-op 的进程组配置以满足跨平台编译。

import "syscall"

// stdioSysProcAttr 无平台配置（Windows 进程树终止由 taskkill /T 负责）。
func stdioSysProcAttr() *syscall.SysProcAttr { return nil }

// killProcessGroup Windows 上无进程组语义：返回不支持错误，
// killProcessTree 会回退 taskkill /T 与逐进程 kill。
func killProcessGroup(pid int) error {
	return errKillGroupUnsupported
}

// errKillGroupUnsupported Windows 平台的 killProcessGroup 占位错误。
var errKillGroupUnsupported = syscall.EWINDOWS
