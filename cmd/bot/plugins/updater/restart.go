package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// envUpdateMarker 注入给新进程的环境变量名：值为待确认更新标记文件路径。
const envUpdateMarker = "REMILIA_UPDATE_MARKER"

// envWaitParent 注入给新进程的环境变量名：值为旧进程 PID（新进程须等其退出）。
const envWaitParent = "REMILIA_UPDATED_BY"

// envChildConsole 注入给新进程的环境变量名：值为子进程控制台策略。
const envChildConsole = "REMILIA_CHILD_CONSOLE"

// envChildLog 注入给新进程的环境变量名（仅 child_console="file" 时）：值为
// 子进程 stdout/stderr 重定向的日志文件路径。
const envChildLog = "REMILIA_CHILD_LOG"

// childConsoleMode 拉起子进程时的控制台策略（运行时由插件 Setup 从
// plugins.updater.child_console 配置写入，重启后生效）。
//
//	""     安全默认：不向子进程传递任何控制台句柄（标准输出接到 NUL）。
//	      子进程日志通过 log.file 落盘。实测教训：把父进程控制台句柄传给
//	      DETACHED_PROCESS 子进程，父进程退出时子进程会被连带终止。
//	"new"  Windows 上为子进程创建独立控制台窗口（CREATE_NEW_CONSOLE），
//	      日志自成一窗，与父进程完全独立（唯一"日志可见且子进程存活"的方案）。
//	"file" 子进程 stdout/stderr 重定向到 childLogPath 文件，无窗口、无句柄依赖。
var childConsoleMode string

// childLogPath child_console="file" 时子进程日志文件路径（插件 Setup 写入）。
var childLogPath = filepath.Join("data", "updater", "child.log")

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
// 控制台策略（child_console）：
//   - ""：不传句柄（os/exec 会把标准输出接到 NUL）。子进程不依赖任何控制台，
//     实测唯一能保证"父进程退出后子进程存活"的安全路径；日志靠 log.file 落盘。
//   - "new"：独立控制台窗口（CREATE_NEW_CONSOLE），子进程早期自行重绑定 CONOUT$。
//   - "file"：子进程 stdout/stderr 重定向到 childLogPath。
//
// 注意：不要向 DETACHED_PROCESS 子进程传递父进程的控制台句柄——Windows 会把
// 子进程视为与该控制台关联，父进程退出/控制台关闭时子进程被连带终止。
//
// 成功后返回 nil（新进程已在后台运行）；失败返回错误，调用方应回滚替换。
// 包级变量以便测试注入假实现。
var spawnNewProcess = func(exePath string, markerPath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = append(os.Environ(),
		envWaitParent+"="+strconv.Itoa(os.Getpid()),
		envUpdateMarker+"="+markerPath,
	)

	switch childConsoleMode {
	case "new":
		cmd.Env = append(cmd.Env, envChildConsole+"=new")
	case "file":
		cmd.Env = append(cmd.Env, envChildConsole+"=file", envChildLog+"="+childLogPath)
		f, err := os.OpenFile(childLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			// 日志文件打不开就退回安全默认（NUL），不阻塞更新
			fmt.Fprintf(os.Stderr, "[updater] 打开子进程日志文件失败（%v），子进程输出将不可见\n", err)
			break
		}
		defer f.Close()
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := startDetachedChild(cmd); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}
	// 释放进程句柄：新进程生命周期与旧进程无关，由 OS 接管。
	// Release 后无法再 Wait，但旧进程即将退出，无需回收。
	_ = cmd.Process.Release()
	return nil
}
