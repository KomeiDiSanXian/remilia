//go:build unix && !linux && !darwin

package coredump

import "syscall"

// setRlimMax 将 rlimit 设置为 RLIM_INFINITY 的安全近似值。
//
// BSD 系统（FreeBSD、OpenBSD、NetBSD）的 syscall.Rlimit 字段类型为 int64，
// 使用 1<<63-1（math.MaxInt64）确保不溢出。
func setRlimMax(rlim *syscall.Rlimit) {
	rlim.Cur = 1<<63 - 1
	rlim.Max = 1<<63 - 1
}

// platformInit 在其他 Unix 系统（FreeBSD、OpenBSD、NetBSD 等）上执行平台特定初始化。
//
// 这些平台的 RLIMIT_CORE 已在 coredump_unix.go 中设置，
// 通常不需要额外的初始化操作。
func platformInit() {}

// diagnose 在其他 Unix 系统上执行诊断。
//
// 目前仅提供 RLIMIT_CORE 设置（来自 coredump_unix.go）。
// BSD 系列系统的核心转储路径通常由 sysctl kern.corefile 控制，
// 但各系统的具体行为不同，此处不做自动诊断。
func diagnose() {}
