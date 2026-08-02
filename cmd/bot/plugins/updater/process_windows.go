//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// setDetached 让新进程脱离控制台（Windows）：
// DETACHED_PROCESS 不继承控制台；CREATE_NEW_PROCESS_GROUP 使 Ctrl+C 不扩散到新进程。
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

// waitProcessExit 等待 pid 对应的进程退出（Windows 使用进程句柄等待）。
func waitProcessExit(pid int, timeout, pollInterval time.Duration) error {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// 进程不存在（或句柄权限不足）视为已退出
		return nil
	}
	defer windows.CloseHandle(handle)

	deadline := time.Now().Add(timeout)
	for {
		var code uint32
		if err := windows.GetExitCodeProcess(handle, &code); err == nil && code != uint32(windows.STATUS_PENDING) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待旧进程（pid=%d）退出超时（%s）", pid, timeout)
		}
		time.Sleep(pollInterval)
	}
}

// triggerSelfShutdown 在 Windows 上无法向自身发送 SIGTERM（Go 的
// syscall.Kill 在 Windows 不存在），更新消息已冲刷完成后直接退出进程。
func triggerSelfShutdown() {
	os.Exit(0)
}
