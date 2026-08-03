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

// envChildConsole 注入给新进程的环境变量名：值为子进程控制台策略
// （""=继承父进程控制台句柄，日志继续出现在原终端；"new"=独立新控制台窗口）。
const envChildConsole = "REMILIA_CHILD_CONSOLE"

// childConsoleMode 拉起子进程时的控制台策略（运行时由插件 Setup 从
// plugins.updater.child_console 配置写入，重启后生效）。
//
//	""       继承父进程控制台句柄：更新后新进程日志继续出现在启动它的终端
//	"new"    子进程获得独立的新控制台窗口（CREATE_NEW_CONSOLE），日志自成一窗
var childConsoleMode string

// currentConsoleMode 返回当前生效的控制台策略：优先取子进程注入的环境变量
// （回滚路径的子进程未经过插件 Setup，只能靠环境变量传递），否则取配置值。
func currentConsoleMode() string {
	if m := os.Getenv(envChildConsole); m != "" {
		return m
	}
	return childConsoleMode
}

// spawnNewProcess 以新二进制重新启动自身（分离进程）。
//
// 新进程携带 REMILIA_UPDATED_BY（旧 PID）与 REMILIA_UPDATE_MARKER（标记路径），
// 在 main 最早期等待旧进程退出后再继续——旧进程仍在优雅关闭中，直接启动会
// 与健康检查/API/平台 Webhook 端口冲突。
//
// 显式传递父进程的 stdout/stderr 句柄：Go 的 os/exec 在字段为 nil 时会把子进程
// 标准输出接到 NUL，导致更新后新进程"活着但没有任何可见输出"。
//
// 成功后返回 nil（新进程已在后台运行）；失败返回错误，调用方应回滚替换。
// 包级变量以便测试注入假实现。
var spawnNewProcess = func(exePath string, markerPath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = append(os.Environ(),
		envWaitParent+"="+strconv.Itoa(os.Getpid()),
		envUpdateMarker+"="+markerPath,
	)
	if childConsoleMode == "new" {
		cmd.Env = append(cmd.Env, envChildConsole+"=new")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := startDetachedChild(cmd); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}
	// 释放进程句柄：新进程生命周期与旧进程无关，由 OS 接管。
	// Release 后无法再 Wait，但旧进程即将退出，无需回收。
	_ = cmd.Process.Release()
	return nil
}
