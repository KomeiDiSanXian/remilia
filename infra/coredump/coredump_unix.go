//go:build unix

package coredump

import "syscall"

// enablePlatform 在 Unix 系统上启用核心转储。
//
// 执行两个阶段：
//  1. 设置 RLIMIT_CORE 为无限制（允许 OS 生成核心转储）
//  2. 平台特定初始化（如 Linux prctl PR_SET_DUMPABLE）
//
// 环境诊断（core_pattern、容器检测等）由 [Enable] 根据
// Config.DiagnoseOnEnable 决定是否调用，也可通过 [Diagnose] 手动触发。
func enablePlatform(_ *Config) error {
	if err := setRlimitCore(); err != nil {
		return err
	}

	platformInit()

	return nil
}

// setRlimitCore 设置 RLIMIT_CORE 为无限制，允许操作系统生成核心转储。
//
// RLIMIT_CORE 控制核心转储文件的最大大小。
// 首先尝试设置为无限制，如果失败（非 root 用户），
// 则回退到将软限制设置为当前硬限制值。
//
// 由于 syscall.Rlimit 的字段因平台而异
// （Linux/macOS: uint64, FreeBSD: int64），
// 实际的 RLIM_INFINITY 赋值由各平台的 setRlimMax() 完成。
func setRlimitCore() error {
	var rlim syscall.Rlimit

	// 尝试设置为该平台的 RLIM_INFINITY
	setRlimMax(&rlim)
	if syscall.Setrlimit(syscall.RLIMIT_CORE, &rlim) == nil {
		return nil
	}

	// 无法设置为无限（通常是非 root），回退到当前硬限制
	if err := syscall.Getrlimit(syscall.RLIMIT_CORE, &rlim); err != nil {
		return err
	}
	rlim.Cur = rlim.Max
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &rlim)
}
