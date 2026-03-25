// Package coredump 提供在程序崩溃时生成核心转储（core dump）的能力。
//
// 该包通过设置 GOTRACEBACK=crash 并配合平台特定的机制，
// 确保程序在发生不可恢复的崩溃时能够保存完整的诊断信息。
//
// # 核心机制
//
//   - [debug.SetTraceback]("crash")：使 Go 运行时在 panic 时触发 OS 级崩溃信号
//     （Unix: SIGABRT，Windows: RaiseException），而非默认的仅打印栈信息后退出。
//     没有此设置，Go panic 不会产生核心转储。
//   - [debug.SetCrashOutput]：将运行时崩溃报告（goroutine 栈信息）写入文件，
//     作为核心转储的补充（尤其在核心转储不可用时）。
//
// # 平台支持
//
// Linux：
//   - 设置 RLIMIT_CORE 为无限制
//   - 调用 prctl(PR_SET_DUMPABLE, 1) 确保进程可转储
//     （setuid、容器、seccomp 等场景会导致此标志被清零）
//   - 自动诊断 /proc/sys/kernel/core_pattern：
//     检测管道模式（如 systemd-coredump、apport）并给出对应的查看命令
//   - 自动检测容器环境（Docker/Kubernetes）并警告相关限制
//
// macOS：
//   - 设置 RLIMIT_CORE 为无限制（等效于 ulimit -c unlimited）
//   - 诊断 kern.corefile sysctl（macOS 使用此值而非 /proc/sys/kernel/core_pattern）
//   - 检查 /cores 目录是否存在且可写（macOS 默认的核心转储位置）
//
// Windows：
//   - 注册向量化异常处理器（VEH）生成 MiniDump 文件
//   - VEH 回调遵循"崩溃安全"设计：零堆分配、无 Go 锁、仅裸 syscall
//   - MiniDump 不含 ExceptionPointers（Go callback trampoline 切栈后
//     native 异常帧不可达），但包含所有线程栈、模块、数据段等
//   - 崩溃点精确寄存器快照由 [debug.SetCrashOutput] 的 crash log 提供，
//     两者互补形成完整调试数据
//   - 详见 coredump_windows.go 中的设计文档
//
// # 已知限制
//
// Linux/容器：
//   - 若 core_pattern 为管道模式（如 "|/usr/lib/systemd/systemd-coredump ..."），
//     核心转储不会生成文件，而是交给外部程序处理。
//     此时需使用对应工具查看（如 coredumpctl）。
//   - Docker/Kubernetes 中默认禁用核心转储，
//     需显式配置 --ulimit core=-1 和 SYS_PTRACE capability。
//   - setuid 程序和某些安全策略会清除 dumpable 标志。
//
// macOS：
//   - SIP (System Integrity Protection) 可能阻止某些进程的核心转储。
//   - /cores 目录必须存在且可写，否则转储静默失败。
//
// Windows：
//   - GOTRACEBACK=crash 触发的 RaiseFailFastException 绕过 VEH，
//     此时 VEH 无法生成 MiniDump，需依赖 [debug.SetCrashOutput] 的崩溃日志。
//   - Go callback trampoline 切栈导致 VEH 参数中的 ExceptionPointers 不可达，
//     因此 MiniDump 不含异常上下文（ERROR_NOACCESS 998）。
//     crash log 已包含异常代码、地址和完整寄存器值作为补充。
//
// # 典型用法
//
//	// 使用默认配置启用核心转储（推荐在 main 函数开头调用）
//	if err := coredump.Enable(); err != nil {
//	    log.Printf("启用核心转储失败: %v", err)
//	}
//
//	// 自定义转储目录
//	if err := coredump.Enable(
//	    coredump.WithDir("/var/crash/myapp"),
//	); err != nil {
//	    log.Printf("启用核心转储失败: %v", err)
//	}
//
//	// 延迟诊断（容器环境中，配置可能在启动后才就绪）
//	coredump.Enable(
//	    coredump.WithDiagnoseOnEnable(false),
//	)
//	// ... 等待环境就绪 ...
//	coredump.Diagnose() // 手动触发诊断
//
// # 分析核心转储
//
// Linux（文件模式）：
//
//	dlv core ./myapp /tmp/core-myapp-12345-1700000000
//	# 或
//	gdb ./myapp /tmp/core-myapp-12345-1700000000
//
// Linux（systemd-coredump）：
//
//	coredumpctl list
//	coredumpctl debug
//
// macOS：
//
//	lldb -c /cores/core.12345 ./myapp
//
// Windows：
//
//	# 使用 WinDbg 或 Visual Studio 打开 .dmp 文件
//	# 使用 Delve: dlv core myapp.exe core-xxx.dmp
package coredump
