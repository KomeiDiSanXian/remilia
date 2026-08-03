//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// startDetachedChild 分离启动新进程（Windows）：
// CREATE_NEW_PROCESS_GROUP 使 Ctrl+C 不扩散到新进程；控制台策略二选一——
// 默认 DETACHED_PROCESS 不继承控制台，child_console="new" 时 CREATE_NEW_CONSOLE
// 为子进程创建独立控制台窗口（stdout/stderr 由子进程启动早期重绑定到 CONOUT$）。
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
	baseFlags := uint32(windows.CREATE_NEW_PROCESS_GROUP)
	if currentConsoleMode() == "new" {
		baseFlags |= windows.CREATE_NEW_CONSOLE
	} else {
		baseFlags |= windows.DETACHED_PROCESS
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: baseFlags | windows.CREATE_BREAKAWAY_FROM_JOB,
	}
	if err := cmd.Start(); err == nil {
		return nil
	}

	// Job Object 不允许脱离 → 回退普通分离启动。
	// 注意不能用 *cmd 复制后再次 Start：exec.Cmd 的 startCalled 标志不可重置，
	// 复制品第二次 Start 必然返回 "exec: already started"——必须重建全新命令。
	fresh := exec.Command(cmd.Path, cmd.Args[1:]...)
	fresh.Env = cmd.Env
	fresh.Stdout = cmd.Stdout
	fresh.Stderr = cmd.Stderr
	fresh.SysProcAttr = &syscall.SysProcAttr{CreationFlags: baseFlags}
	if err := fresh.Start(); err != nil {
		return err
	}
	cmd.Process = fresh.Process
	return nil
}

// otherInstances 返回与本进程同名的其它运行实例 PID（排除自身）。
//
// 用于更新前的体检：多个实例共用同一 data 目录时，LevelDB（antispam 等
// 插件存储）的排他锁会导致新进程启动即失败（"文件被另一个进程占用"）。
func otherInstances(exeBase string) []int {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	var pids []int
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if int(entry.ProcessID) == os.Getpid() {
			continue
		}
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, exeBase) {
			pids = append(pids, int(entry.ProcessID))
		}
	}
	return pids
}

// bindChildConsole 将子进程的标准句柄重绑定到自己的控制台（CONIN$/CONOUT$）。
//
// 仅当以 child_console="new" 拉起且本进程确实拥有新控制台时执行：CREATE_NEW_CONSOLE
// 已为进程创建控制台窗口，但 os/exec 注入的 stdin/stdout/stderr 句柄仍指向 NUL，
// 不重绑定则日志依然不可见。必须在任何日志输出（logger.Init）之前调用，
// HandlePendingUpdate 位于 main 最早期，是唯一合适的位置。
//
// 注意：Go 包级 init 已用旧句柄构造了默认 logger，但 main 会在 HandlePendingUpdate
// 之后重新 logger.Init，届时会拾取重绑定后的 os.Stdout/os.Stderr。
func bindChildConsole() {
	if currentConsoleMode() != "new" {
		return
	}

	conIn, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONIN$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		os.Stdin = os.NewFile(uintptr(conIn), "CONIN$")
	} else {
		fmt.Fprintf(os.Stderr, "[updater] 打开 CONIN$ 失败: %v\n", err)
	}

	conOut, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		os.Stdout = os.NewFile(uintptr(conOut), "CONOUT$")
		os.Stderr = os.NewFile(uintptr(conOut), "CONOUT$")
	} else {
		fmt.Fprintf(os.Stderr, "[updater] 打开 CONOUT$ 失败: %v\n", err)
	}
}

// exitGracePeriod 检测到旧进程退出后、子进程继续启动前的宽限时间：
// 让内核完成旧进程文件句柄的关闭，避免新进程立即打开 LevelDB 等共享数据文件时
// 撞上"文件被另一个进程占用"。
const exitGracePeriod = 500 * time.Millisecond

// waitProcessExit 等待 pid 对应的进程退出（Windows）。
//
// 使用 WaitForSingleObject（需 SYNCHRONIZE 权限）等待进程**完整终止**：
// 进程对象在句柄清理完成后才会置为 signaled。而 GetExitCodeProcess 在进程
// 退出代码可见时即返回，此时内核可能尚未关闭该进程的全部文件句柄——新进程
// 立即继续启动并打开共享数据文件（LevelDB 等）会报"文件被另一个进程占用"
// （实测：子进程"等待 0s"继续启动，父进程数秒后才完成退出，DB 打开失败）。
//
// 等待结束后再宽限 exitGracePeriod，双保险确保句柄释放。
func waitProcessExit(pid int, timeout, pollInterval time.Duration) error {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(handle)
		ev, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
		if err != nil {
			return fmt.Errorf("等待旧进程（pid=%d）失败: %w", pid, err)
		}
		if ev == uint32(windows.WAIT_TIMEOUT) {
			return fmt.Errorf("等待旧进程（pid=%d）退出超时（%s）", pid, timeout)
		}
		time.Sleep(exitGracePeriod)
		return nil
	}

	// 进程已不存在（或句柄权限不足）→ 按已退出处理，但仍宽限等待句柄释放
	fmt.Fprintf(os.Stderr, "[updater] OpenProcess(%d) 失败（%v），视为旧进程已退出\n", pid, err)
	time.Sleep(exitGracePeriod)
	return nil
}

// triggerSelfShutdown 在 Windows 上无法向自身发送 SIGTERM（Go 的
// syscall.Kill 在 Windows 不存在），更新消息已冲刷完成后直接退出进程。
//
// 退出前先执行注入的优雅关闭回调（SetShutdownHook）：插件 Teardown 会确定性
// 关闭 LevelDB 等数据文件句柄，子进程随后启动时不会撞上"文件被另一个进程占用"
// （等待宽限仅作为回调缺失/异常退出时的兜底）。
func triggerSelfShutdown() {
	if hook := shutdownHook; hook != nil {
		hook()
	}
	os.Exit(0)
}
