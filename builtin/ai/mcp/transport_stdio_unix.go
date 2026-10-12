//go:build !windows

package mcp

// transport_stdio_unix.go — stdio 传输的 Unix 平台分支：进程组配置与整组终止。
//
// 官方示例配置（npx -y ...）必然派生 node 孙进程：只杀 npx 会让孤儿孙进程
// 存活并持有 stdout 写端——ReadBytes 永远等不到 EOF，读取循环不结束、
// supervise 认为"连接还在"，工具调用全部超时却永不重连（僵尸会话）。
// 让子进程自成进程组后用 -pid 整组 SIGKILL 是最可靠的清理方式。

import (
	"os/exec"
	"syscall"
)

// stdioSysProcAttr 让子进程拥有独立进程组（组长），供 killProcessGroup 用
// -pid 命中整组。返回 nil 表示无平台配置。
func stdioSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup 向整个进程组发送 SIGKILL（子进程由 Setpgid 自成组长）。
// 组不存在（例如进程已退出）时返回错误，调用方回退逐进程 kill。
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
