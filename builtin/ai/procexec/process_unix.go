//go:build !windows

// process_unix.go — Unix 平台的进程组与整树终止。
package procexec

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 让子进程成为新进程组的组长，取消时可整组终止。
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree 终止子进程所在的整个进程组。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
