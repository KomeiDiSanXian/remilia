//go:build windows

// process_windows.go — Windows 平台的整树终止。
package procexec

import (
	"os/exec"
	"strconv"
)

// setProcessGroup Windows 不需要预置进程组：整树回收由 taskkill /T 完成。
func setProcessGroup(*exec.Cmd) {}

// killProcessTree 通过 taskkill 终止进程树；失败时回退到单进程终止。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
