//go:build !windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// startDetachedChild 分离启动新进程（Unix：Setsid 脱离父进程的进程组与会话，
// 旧进程退出/终端关闭时新进程不受影响）。
func startDetachedChild(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// waitProcessExit 轮询等待 pid 对应的进程退出，超时返回错误。
func waitProcessExit(pid int, timeout, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		// signal 0 仅探测进程存在性，不实际发送信号
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待旧进程（pid=%d）退出超时（%s）", pid, timeout)
		}
		time.Sleep(pollInterval)
	}
}

// triggerSelfShutdown 向自身发送 SIGTERM，触发 main 的优雅关闭路径
// （WaitForShutdown 收到信号后执行完整清理并退出，新进程随后接管）。
func triggerSelfShutdown() {
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}
