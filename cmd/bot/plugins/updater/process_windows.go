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

// startDetachedChild 分离启动新进程（Windows）：
// DETACHED_PROCESS 不继承控制台；CREATE_NEW_PROCESS_GROUP 使 Ctrl+C 不扩散到新进程。
//
// 优先携带 CREATE_BREAKAWAY_FROM_JOB 尝试脱离父进程所在的 Job Object：
// 某些启动器/终端会把启动的进程放进带 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 的
// Job 中，父进程退出时 Job 关闭会连带杀死刚拉起的子进程——表现为"旧进程已退出
// 但新进程没有起来"。脱离后子进程不再受父进程所在 Job 的生命周期约束。
//
// 若父进程所在 Job 不允许脱离（未置 BREAKAWAY_OK / SILENT_BREAKAWAY_OK），
// CreateProcess 会返回 ERROR_ACCESS_DENIED，此时回退到不带该标志的普通分离启动
// （行为与旧版一致，至少不劣化）。
func startDetachedChild(cmd *exec.Cmd) error {
	baseFlags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: baseFlags | windows.CREATE_BREAKAWAY_FROM_JOB,
	}
	if err := cmd.Start(); err == nil {
		return nil
	}

	// Job Object 不允许脱离 → 回退普通分离启动
	fallback := *cmd
	fallback.SysProcAttr = &syscall.SysProcAttr{CreationFlags: baseFlags}
	if err := fallback.Start(); err != nil {
		return err
	}
	*cmd = fallback
	return nil
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
