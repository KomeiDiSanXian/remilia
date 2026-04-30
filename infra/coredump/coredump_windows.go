//go:build windows

package coredump

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ================== Windows 异常代码 ==================
// 参见：https://learn.microsoft.com/en-us/windows/win32/debug/exception-constants

const (
	_EXCEPTION_ACCESS_VIOLATION    = 0xC0000005
	_EXCEPTION_STACK_OVERFLOW      = 0xC00000FD
	_EXCEPTION_INT_DIVIDE_BY_ZERO  = 0xC0000094
	_EXCEPTION_ILLEGAL_INSTRUCTION = 0xC000001D
	_EXCEPTION_PRIV_INSTRUCTION    = 0xC0000096
	_EXCEPTION_ARRAY_BOUNDS        = 0xC000008C
	_EXCEPTION_FLT_DENORMAL        = 0xC000008D
	_EXCEPTION_FLT_DIVIDE_BY_ZERO  = 0xC000008E
	_EXCEPTION_FLT_INEXACT         = 0xC000008F
	_EXCEPTION_FLT_INVALID_OP      = 0xC0000090
	_EXCEPTION_FLT_OVERFLOW        = 0xC0000091
	_EXCEPTION_FLT_STACK_CHECK     = 0xC0000092
	_EXCEPTION_FLT_UNDERFLOW       = 0xC0000093
)

// ================== Windows / MiniDump 常量 ==================

const _EXCEPTION_CONTINUE_SEARCH = 0

// CreateFileW 参数
const (
	_GENERIC_READ          = 0x80000000
	_GENERIC_WRITE         = 0x40000000
	_FILE_SHARE_READ       = 0x00000001
	_FILE_SHARE_WRITE      = 0x00000002
	_CREATE_ALWAYS         = 2
	_FILE_ATTRIBUTE_NORMAL = 0x80
	_INVALID_HANDLE        = ^uintptr(0) // INVALID_HANDLE_VALUE = (HANDLE)-1
)

// MiniDump 类型标志
// 参见：https://learn.microsoft.com/en-us/windows/win32/api/minidumpapiset/ne-minidumpapiset-minidump_type
const (
	_MiniDumpNormal                = 0x00000000
	_MiniDumpWithDataSegs          = 0x00000001
	_MiniDumpWithHandleData        = 0x00000004
	_MiniDumpWithThreadInfo        = 0x00001000
	_MiniDumpWithProcessThreadData = 0x00000100
	_MiniDumpWithUnloadedModules   = 0x00000020

	// 默认组合：足够的调试信息 + 合理的文件大小。
	// 如需完整内存转储，可添加 MiniDumpWithFullMemory (0x00000002)。
	_miniDumpType = _MiniDumpNormal |
		_MiniDumpWithDataSegs |
		_MiniDumpWithHandleData |
		_MiniDumpWithThreadInfo |
		_MiniDumpWithUnloadedModules |
		_MiniDumpWithProcessThreadData
)

// ================== Windows 结构体布局 ==================

// _EXCEPTION_POINTERS 是 VEH 回调接收的参数。
type _EXCEPTION_POINTERS struct {
	ExceptionRecord unsafe.Pointer
	ContextRecord   unsafe.Pointer
}

// _EXCEPTION_RECORD 仅读取首字段 ExceptionCode。
type _EXCEPTION_RECORD struct {
	ExceptionCode uint32
}

// ================== 预分配状态 ==================
//
// 以下全局变量在 Enable() 中一次性写入（正常上下文），
// 在 VEH 回调中仅 **只读** 访问。
// 无需锁保护：写入发生在 VEH 注册之前，具有 happens-before 语义。
var (
	// 预解析的裸函数地址（避免 LazyProc.Call 的间接调用和 mustFind 检查）
	fnMiniDumpWriteDump uintptr
	fnCreateFileW       uintptr
	fnCloseHandle       uintptr
	fnGetCurrentProcess uintptr

	// 预分配的 UTF-16 转储文件路径（含 Enable 时的时间戳和 PID）。
	// VEH 中直接使用此指针，无需 string → UTF-16 转换。
	//
	// 文件名包含 PID + 时间戳，supervisor 重启进程后 PID 变化，
	// 不会覆盖前一次的 dump 文件。
	// 同一进程内，atomic CAS 保证仅写入一次。
	preDumpPathUTF16 *uint16

	// 预缓存的进程 ID（避免 VEH 中调用 os.Getpid）。
	preProcessID uint32

	// 原子标志：0 = 未写入，1 = 已写入/正在写入。
	// sync/atomic.CompareAndSwapUint32 编译为 LOCK CMPXCHG 指令，
	// 无 Go runtime 依赖，在崩溃上下文中安全。
	dumpWritten uint32
)

// ================== enablePlatform ==================

// enablePlatform 预解析所有 DLL 函数地址、预分配转储路径、注册 VEH。
//
// 所有可能失败的操作（DLL 加载、内存分配、路径转换）均在此完成。
// VEH 回调不做任何动态解析或堆分配。
func enablePlatform(cfg *Config) error {
	// ---- 加载 DLL 并预解析函数地址 ----
	dbghelp := windows.NewLazySystemDLL("dbghelp.dll")
	k32 := windows.NewLazySystemDLL("kernel32.dll")

	type entry struct {
		name string
		proc *windows.LazyProc
		dest *uintptr
	}

	entries := []entry{
		{"MiniDumpWriteDump", dbghelp.NewProc("MiniDumpWriteDump"), &fnMiniDumpWriteDump},
		{"CreateFileW", k32.NewProc("CreateFileW"), &fnCreateFileW},
		{"CloseHandle", k32.NewProc("CloseHandle"), &fnCloseHandle},
		{"GetCurrentProcess", k32.NewProc("GetCurrentProcess"), &fnGetCurrentProcess},
	}

	for _, e := range entries {
		if err := e.proc.Find(); err != nil {
			return fmt.Errorf("解析 %s 失败: %w", e.name, err)
		}
		*e.dest = e.proc.Addr()
	}

	procAddVEH := k32.NewProc("AddVectoredExceptionHandler")
	if err := procAddVEH.Find(); err != nil {
		return fmt.Errorf("解析 AddVectoredExceptionHandler 失败: %w", err)
	}

	// ---- 预分配转储文件路径（UTF-16） ----
	path := dumpFilePath(cfg.Dir, "dmp")
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("转换路径失败: %w", err)
	}
	preDumpPathUTF16 = pathUTF16
	preProcessID = uint32(os.Getpid())

	// ---- 注册 VEH（first=1，链首优先执行） ----
	cb := syscall.NewCallback(vehHandler)
	ret, _, _ := procAddVEH.Call(1, cb)
	if ret == 0 {
		return fmt.Errorf("注册 VEH 失败")
	}

	return nil
}

// diagnose 在 Windows 上执行环境诊断。
// 当前为空实现 — Windows 的核心转储配置主要由 VEH + MiniDump 处理，
// 不需要像 Unix 那样检查 core_pattern 或容器环境。
func diagnose() {}

// ================== VEH 回调 ==================
//
// 设计原则 — VEH 回调必须在"半死"的进程中存活：
//
//   - 零 Go 堆分配：所有变量在栈上或预分配的全局变量
//   - 无 sync.Once / sync.Mutex / channel / defer / interface
//   - 无 os.Create / logger / fmt / 任何可能触发 Go runtime 的函数
//   - 仅使用预解析的裸函数地址 + syscall.SyscallN
//   - CAS 标志使用 atomic.CompareAndSwapUint32（单条 LOCK CMPXCHG 指令）
//
// 已知限制（纯 Go 的天花板）：
//
//   - syscall.SyscallN 内部仍有少量 runtime 交互（entersyscall/exitsyscall），
//     这是纯 Go 能达到的最低层级。
//     真正 100% 绕过 runtime 需要平台汇编跳板（如 Chromium crashpad 的做法：
//     MOV rax, fn / CALL rax），但这超出了纯 Go 库的合理范围。
//
//   - 若 Go runtime 完全损坏（TLS 已废、g0 栈异常），
//     syscall.NewCallback 的跳板（trampoline）可能无法正常分发回调，
//     VEH 回调本身将无法执行。
//
//   - GOTRACEBACK=crash 触发的 RaiseFailFastException 绕过 VEH，
//     此时应依赖 debug.SetCrashOutput 保存的崩溃日志。
//
// 为何不传 ExceptionPointers 给 MiniDumpWriteDump？
//
//   Go 的 syscall.NewCallback trampoline 在分发回调时会切换到 Go goroutine 栈。
//   VEH 参数 info 指向的 EXCEPTION_POINTERS（及其内部的 ExceptionRecord、
//   ContextRecord）驻留在 native 异常分发栈帧上。trampoline 切栈后，
//   dbghelp.dll 内部解引用这些指针时大概率触发 ERROR_NOACCESS (998)。
//
//   在最需要 dump 的场景下（stack overflow、runtime 切栈、TLS 切换），
//   native 栈帧恰恰最不可靠。"先传 exInfo 再 fallback" 的策略会在崩溃路径上
//   多一次注定失败的 syscall，增加在半死进程中停留的时间。
//
//   因此直接传 NULL ExceptionParam —— dump 仍包含：
//     • 所有线程的完整调用栈
//     • 已加载 / 已卸载模块列表
//     • 数据段、句柄表、线程信息
//   唯一缺失的是崩溃点精确寄存器快照，而这部分信息已由
//   debug.SetCrashOutput 的 crash log 覆盖（含异常代码、地址、所有寄存器）。
//   两者互补，形成完整的事后调试数据。
//
// 可靠性矩阵：
//
//	场景                    可靠性
//	CGo/syscall 中崩溃      ✅ 可靠（goroutine 在 syscall 模式，VEH trampoline 正常）
//	普通 crash (AV/除零)     ✅ 可靠
//	heap corruption          ⚠️ 大概率可用（syscall 不依赖 heap）
//	runtime 崩溃             ⚠️ 取决于 entersyscall 是否还能工作
//	纯 Go 代码 nil 解引用    ⚠️ VEH trampoline 可能失败（goroutine 在 running 模式）
//	TLS/g 完全损坏           ❌ 回调跳板失效，无法进入 Go 代码
//	RaiseFailFastException   ❌ 绕过 VEH（依赖 debug.SetCrashOutput）
//
// 理想替代方案（外部进程架构，类似 Chromium crashpad）：
//
//   1. Enable() 时启动一个独立的监控进程，通过 named pipe 保持心跳
//   2. VEH handler 仅向 pipe 写入崩溃信号（几个字节，极小开销）
//   3. 监控进程调用 MiniDumpWriteDump(targetProcess, ...) —— 从外部 dump
//   4. 外部进程拥有干净的上下文，不受 Go runtime / 切栈 / TLS 损坏影响
//   这是工业级 crash reporter 的标准做法，但增加了进程管理和 IPC 的复杂度。
//   当前基于 VEH 的实现已满足大部分需求，此方案作为未来架构改进方向。

//go:nosplit
func vehHandler(info unsafe.Pointer) uintptr {
	// 读取异常代码 — 纯指针解引用，无 runtime 调用
	ptrs := (*_EXCEPTION_POINTERS)(info)
	rec := (*_EXCEPTION_RECORD)(ptrs.ExceptionRecord)

	if !isFatalException(rec.ExceptionCode) {
		return _EXCEPTION_CONTINUE_SEARCH
	}

	// 原子 CAS：仅首次致命异常写入 dump。
	// 编译为 LOCK CMPXCHG，无 runtime 依赖。
	if !atomic.CompareAndSwapUint32(&dumpWritten, 0, 1) {
		return _EXCEPTION_CONTINUE_SEARCH
	}

	// ---- 以下全部是预解析地址的裸 syscall，共 4 次调用 ----

	// CreateFileW(path, access, share, sa, disposition, flags, template)
	handle, _, _ := syscall.SyscallN(fnCreateFileW,
		uintptr(unsafe.Pointer(preDumpPathUTF16)),
		_GENERIC_READ|_GENERIC_WRITE,
		_FILE_SHARE_READ|_FILE_SHARE_WRITE,
		0,
		_CREATE_ALWAYS,
		_FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if handle == _INVALID_HANDLE {
		return _EXCEPTION_CONTINUE_SEARCH
	}

	// GetCurrentProcess() — 返回伪句柄 -1，无 kernel 调用
	hProcess, _, _ := syscall.SyscallN(fnGetCurrentProcess)

	// MiniDumpWriteDump(hProcess, pid, hFile, dumpType, NULL, NULL, NULL)
	// ExceptionParam = NULL：不传异常上下文，原因见上方注释。
	syscall.SyscallN(fnMiniDumpWriteDump,
		hProcess,
		uintptr(preProcessID),
		handle,
		uintptr(_miniDumpType),
		0, // NULL ExceptionParam — 见 "为何不传 ExceptionPointers" 注释
		0,
		0,
	)

	// CloseHandle(handle) — 确保数据落盘
	syscall.SyscallN(fnCloseHandle, handle)

	return _EXCEPTION_CONTINUE_SEARCH
}

// isFatalException 判断异常代码是否为致命异常。
//
// 不包含以下异常（它们无法被 VEH 可靠捕获或不应触发 dump）：
//   - EXCEPTION_BREAKPOINT (0x80000003)：调试器和 Go runtime 正常使用
//   - DBG_PRINTEXCEPTION_C (0x40010006)：调试输出，非崩溃
//   - STATUS_STACK_BUFFER_OVERRUN (0xC0000409)：触发 __fastfail → FailFast，
//     完全绕过 VEH/SEH，进程直接终止
//
//go:nosplit
func isFatalException(code uint32) bool {
	switch code {
	case
		_EXCEPTION_ACCESS_VIOLATION,
		_EXCEPTION_STACK_OVERFLOW,
		_EXCEPTION_INT_DIVIDE_BY_ZERO,
		_EXCEPTION_ILLEGAL_INSTRUCTION,
		_EXCEPTION_PRIV_INSTRUCTION,
		_EXCEPTION_ARRAY_BOUNDS,
		_EXCEPTION_FLT_DENORMAL,
		_EXCEPTION_FLT_DIVIDE_BY_ZERO,
		_EXCEPTION_FLT_INEXACT,
		_EXCEPTION_FLT_INVALID_OP,
		_EXCEPTION_FLT_OVERFLOW,
		_EXCEPTION_FLT_STACK_CHECK,
		_EXCEPTION_FLT_UNDERFLOW:
		return true
	default:
		return false
	}
}
