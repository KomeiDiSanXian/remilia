//go:build linux

package coredump

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// setRlimMax 将 rlimit 设置为 Linux 的 RLIM_INFINITY。
// Linux syscall.Rlimit 字段类型为 uint64。
func setRlimMax(rlim *syscall.Rlimit) {
	rlim.Cur = ^uint64(0) // 0xFFFFFFFFFFFFFFFF = RLIM_INFINITY
	rlim.Max = ^uint64(0)
}

// platformInit 在 Linux 上设置进程为可转储状态。
//
// 在以下场景中，内核会将进程的 dumpable 标志清零：
//   - setuid / setgid 程序
//   - 通过 execve 切换了 credentials
//   - 某些容器运行时的安全策略
//   - seccomp 过滤器
//
// 调用 prctl(PR_SET_DUMPABLE, 1) 可恢复该标志，
// 使内核在进程崩溃时允许生成核心转储。
//
// 使用 golang.org/x/sys/unix.Prctl 而非 syscall.RawSyscall，
// 因为 syscall 包已冻结，x/sys/unix 对新内核的兼容性更好。
func platformInit() {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); err != nil {
		logger.Warnf("coredump: prctl(PR_SET_DUMPABLE, 1) 失败: %v", err)
	}
}

// diagnose 在 Linux 上诊断核心转储配置的潜在问题。
//
// 检查项：
//  1. /proc/self/dumpable — 进程是否可转储
//  2. /proc/sys/kernel/core_pattern — 核心转储文件的去向
//  3. 容器环境检测 — Docker/Kubernetes 环境下的限制
func diagnose() {
	checkDumpable()
	checkCorePattern()
	checkContainer()
}

// checkDumpable 读取 /proc/self/dumpable 验证 prctl 是否生效。
//
// 值含义：
//   - 0: 不可转储（不会生成核心转储）
//   - 1: 可转储（正常）
//   - 2: suidsafe（受限转储，仅 root 可读）
func checkDumpable() {
	data, err := os.ReadFile("/proc/self/dumpable")
	if err != nil {
		return // /proc 不可用（极少见），静默跳过
	}
	val := strings.TrimSpace(string(data))
	if val == "1" {
		return // 正常
	}
	logger.Warnf("coredump: /proc/self/dumpable = %s（期望 1），核心转储可能不会生成", val)
	logger.Warnf("coredump: 可能原因: setuid/setgid、seccomp、容器安全策略")
	logger.Warnf("coredump: 尝试修复: sysctl -w fs.suid_dumpable=2（需要 root）")
}

// checkCorePattern 读取 /proc/sys/kernel/core_pattern 并发出诊断。
//
// core_pattern 有两种形式：
//   - 文件路径模式（如 "core", "/tmp/core-%e-%p"）：核心转储直接写入文件
//   - 管道模式（如 "|/usr/lib/systemd/systemd-coredump ..."）：
//     核心转储被管道到外部程序处理，不会在文件系统直接生成文件
func checkCorePattern() {
	data, err := os.ReadFile("/proc/sys/kernel/core_pattern")
	if err != nil {
		logger.Warnf("coredump: 无法读取 /proc/sys/kernel/core_pattern: %v", err)
		return
	}
	pattern := strings.TrimSpace(string(data))

	if !strings.HasPrefix(pattern, "|") {
		// 文件模式：直接告诉用户路径
		logger.Infof("coredump: core_pattern = %s（文件模式）", pattern)
		return
	}

	// 管道模式：核心转储被外部程序接管
	fields := strings.Fields(pattern)
	handler := fields[0][1:] // 去掉前导 "|"

	logger.Warnf("coredump: core_pattern 为管道模式，核心转储由外部程序接管，" +
		"不会直接在文件系统生成文件")
	logger.Warnf("coredump: 处理器: %s", handler)

	switch {
	case strings.Contains(handler, "systemd-coredump"):
		logger.Infof("coredump: 检测到 systemd-coredump，请使用以下命令查看转储:")
		logger.Infof("coredump:   coredumpctl list                    — 列出所有转储")
		logger.Infof("coredump:   coredumpctl info                    — 查看最新转储详情")
		logger.Infof("coredump:   coredumpctl dump -o /tmp/core.dump  — 导出为文件")
		logger.Infof("coredump:   coredumpctl debug                   — 使用 gdb 调试")
	case strings.Contains(handler, "apport"):
		logger.Infof("coredump: 检测到 Ubuntu apport，转储可能在 /var/crash/")
		logger.Infof("coredump: 禁用 apport: sudo systemctl disable apport.service")
	default:
		logger.Infof("coredump: 未知的管道处理器: %s", handler)
	}

	logger.Infof("coredump: 如需直接生成文件（需要 root）:")
	logger.Infof("coredump:   echo '/tmp/core-%%e-%%p-%%t' | sudo tee /proc/sys/kernel/core_pattern")
}

// checkContainer 检测是否在容器环境中运行，并对限制发出警告。
//
// 容器环境通常会限制核心转储：
//   - rlimit 可能被容器运行时覆盖
//   - core_pattern 继承自宿主机，容器内无法修改
//   - dumpable 标志可能被安全策略清零
func checkContainer() {
	var indicators []string

	if _, err := os.Stat("/.dockerenv"); err == nil {
		indicators = append(indicators, "/.dockerenv 存在")
	}

	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		switch {
		case strings.Contains(s, "docker"):
			indicators = append(indicators, "cgroup 包含 docker")
		case strings.Contains(s, "kubepods"):
			indicators = append(indicators, "cgroup 包含 kubepods")
		case strings.Contains(s, "containerd"):
			indicators = append(indicators, "cgroup 包含 containerd")
		}
	}

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		indicators = append(indicators, "KUBERNETES_SERVICE_HOST 已设置")
	}

	if len(indicators) == 0 {
		return
	}

	logger.Warnf("coredump: 检测到容器环境 (%s)", strings.Join(indicators, "; "))
	logger.Warnf("coredump: 容器中核心转储通常受限，建议:")
	logger.Warnf("coredump:   Docker:     docker run --ulimit core=-1 --cap-add SYS_PTRACE ...")
	logger.Warnf("coredump:   K8s Pod:    在 securityContext 中添加 SYS_PTRACE capability")
	logger.Warnf("coredump:   宿主机:     确认宿主机的 core_pattern 配置正确")
	logger.Warnf("coredump:   共享存储:   挂载一个 volume 到转储目录以便提取文件")
}
