//go:build darwin

package coredump

import (
	"os"
	"syscall"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// setRlimMax 将 rlimit 设置为 macOS 的 RLIM_INFINITY。
// macOS 的 RLIM_INFINITY 定义为 (((__uint64_t)1 << 63) - 1)。
// macOS syscall.Rlimit 字段类型为 uint64。
func setRlimMax(rlim *syscall.Rlimit) {
	rlim.Cur = 1<<63 - 1 // 0x7FFFFFFFFFFFFFFF = macOS RLIM_INFINITY
	rlim.Max = 1<<63 - 1
}

// platformInit 在 macOS 上执行平台特定初始化。
//
// macOS 不需要 prctl 等额外操作，但确保核心转储行为符合预期。
// macOS 的 rlimit 已在 coredump_unix.go 中设置。
func platformInit() {
	// macOS 不需要额外的初始化操作。
	// 与 Linux 不同，macOS 没有 prctl(PR_SET_DUMPABLE) 机制。
	// 核心转储的可用性主要取决于：
	//   1. ulimit -c (RLIMIT_CORE) — 已在 setRlimitCore() 中设置
	//   2. kern.corefile sysctl — 控制转储文件的路径模式
	//   3. /cores 目录是否存在且可写
}

// diagnose 在 macOS 上诊断核心转储配置的潜在问题。
//
// macOS 的核心转储机制与 Linux 有显著区别：
//   - 不使用 /proc/sys/kernel/core_pattern
//   - 使用 sysctl kern.corefile 控制转储路径（默认 /cores/core.%P）
//   - 需要 /cores 目录存在且可写
//   - SIP (System Integrity Protection) 可能阻止某些进程的核心转储
func diagnose() {
	checkCorefile()
	checkCoresDir()
}

// checkCorefile 读取 macOS 的 kern.corefile sysctl 值。
//
// kern.corefile 定义核心转储文件的路径模式。
// 默认值通常为 /cores/core.%P（%P 会被替换为进程 PID）。
func checkCorefile() {
	corefile, err := syscall.Sysctl("kern.corefile")
	if err != nil {
		logger.Warnf("coredump: 无法读取 kern.corefile: %v", err)
		return
	}
	logger.Infof("coredump: macOS 核心转储路径模式: %s", corefile)
	logger.Infof("coredump: 如需更改（需要 sudo）: sudo sysctl kern.corefile=/cores/core.%%P")
}

// checkCoresDir 检查 macOS 默认的 /cores 目录。
//
// macOS 默认将核心转储写入 /cores 目录。
// 如果该目录不存在或不可写，核心转储将静默失败。
func checkCoresDir() {
	info, err := os.Stat("/cores")
	if err != nil {
		logger.Warnf("coredump: /cores 目录不存在，核心转储可能无法写入")
		logger.Warnf("coredump: 创建目录: sudo mkdir -p /cores && sudo chmod 1777 /cores")
		return
	}
	if !info.IsDir() {
		logger.Warnf("coredump: /cores 存在但不是目录，核心转储无法写入")
		return
	}

	// 检查目录权限（粗略检查 — 实际写入权限取决于进程 UID）
	if info.Mode().Perm()&0o002 == 0 && os.Getuid() != 0 {
		logger.Warnf("coredump: /cores 目录可能不可写（权限: %s），"+
			"非 root 用户的核心转储可能失败", info.Mode().Perm())
		logger.Warnf("coredump: 修复: sudo chmod 1777 /cores")
	}
}
