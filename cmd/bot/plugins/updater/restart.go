package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// envUpdateMarker 注入给新进程的环境变量名：值为待确认更新标记文件路径。
const envUpdateMarker = "REMILIA_UPDATE_MARKER"

// envWaitParent 注入给新进程的环境变量名：值为旧进程 PID（新进程须等其退出）。
const envWaitParent = "REMILIA_UPDATED_BY"

// spawnNewProcess 以新二进制重新启动自身（分离进程）。
//
// 新进程携带 REMILIA_UPDATED_BY（旧 PID）与 REMILIA_UPDATE_MARKER（标记路径），
// 在 main 最早期等待旧进程退出后再继续——旧进程仍在优雅关闭中，直接启动会
// 与健康检查/API/平台 Webhook 端口冲突。
//
// 成功后返回 nil（新进程已在后台运行）；失败返回错误，调用方应回滚替换。
// 包级变量以便测试注入假实现。
var spawnNewProcess = func(exePath string, markerPath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = append(os.Environ(),
		envWaitParent+"="+strconv.Itoa(os.Getpid()),
		envUpdateMarker+"="+markerPath,
	)
	setDetached(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}
	// 释放进程句柄：新进程生命周期与旧进程无关，由 OS 接管。
	// Release 后无法再 Wait，但旧进程即将退出，无需回收。
	_ = cmd.Process.Release()
	return nil
}
