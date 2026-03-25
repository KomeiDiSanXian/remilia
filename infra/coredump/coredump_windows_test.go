//go:build windows

package coredump

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ================== 集成测试：真实 MiniDump 生成 ==================
//
// 测试策略（子进程模式）：
//
//  1. 父进程（正常 test runner）启动子进程，传递环境变量 COREDUMP_TEST_CRASH=1
//  2. 子进程调用 Enable()，然后通过 RaiseException 触发 EXCEPTION_ACCESS_VIOLATION
//  3. VEH 处理器在崩溃前写入 MiniDump 文件（不含异常上下文，见 vehHandler 注释）
//  4. 父进程等待子进程退出，检查 dump 目录中是否生成了 .dmp 文件
//
// 为何使用 RaiseException 而非 Go 层面 nil 解引用？
//
//   在 Go 代码中直接解引用 nil 指针会导致 goroutine 处于 "running" 状态时触发异常。
//   此时 VEH 回调的 trampoline（由 syscall.NewCallback 创建）尝试执行 exitsyscall，
//   但不存在对应的 entersyscall，导致 runtime fatal error。
//
//   通过 syscall 调用 RaiseException，goroutine 在 entersyscall 后进入 "syscall" 模式，
//   VEH callback trampoline 能正确管理状态转换。
//
//   这也是生产环境中 VEH 最可靠的场景：C/native 代码崩溃（goroutine 在 syscall 模式）。

// TestMiniDumpGeneration 验证 VEH 处理器在崩溃时确实生成了 MiniDump 文件。
func TestMiniDumpGeneration(t *testing.T) {
	// ---- 子进程分支：崩溃 ----
	if os.Getenv("COREDUMP_TEST_CRASH") == "1" {
		dir := os.Getenv("COREDUMP_TEST_DIR")
		if dir == "" {
			os.Exit(2)
		}
		if err := Enable(
			WithDir(dir),
			WithCrashLog(false),
			WithDiagnoseOnEnable(false),
		); err != nil {
			_, _ = os.Stderr.WriteString("Enable failed: " + err.Error() + "\n")
			os.Exit(2)
		}

		// 通过 Windows API RaiseException 触发致命异常。
		k32 := windows.NewLazySystemDLL("kernel32.dll")
		raiseException := k32.NewProc("RaiseException")
		_, _, _ = raiseException.Call(
			uintptr(_EXCEPTION_ACCESS_VIOLATION), // dwExceptionCode
			1,                                    // dwExceptionFlags = EXCEPTION_NONCONTINUABLE
			0,                                    // nNumberOfArguments
			0,                                    // lpArguments (NULL)
		)
		os.Exit(0) // unreachable
	}

	// ---- 父进程分支：启动子进程并验证 ----
	dir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^TestMiniDumpGeneration$", "-test.v")
	cmd.Env = append(os.Environ(),
		"COREDUMP_TEST_CRASH=1",
		"COREDUMP_TEST_DIR="+dir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("子进程应以非零退出码退出（预期崩溃），但退出码为 0")
	}
	t.Logf("子进程已退出（预期行为）: %v", err)

	// 扫描目录查找 .dmp 文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 dump 目录失败: %v", err)
	}

	var found bool
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".dmp" {
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			if info.Size() > 0 {
				found = true
				t.Logf("✅ MiniDump 文件已生成: %s (%d bytes)", e.Name(), info.Size())
			} else {
				t.Logf("⚠️  MiniDump 文件为空: %s", e.Name())
			}
		}
	}

	if !found {
		t.Log("dump 目录内容:")
		for _, e := range entries {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			t.Logf("  %s (%d bytes)", e.Name(), size)
		}
		t.Fatal("❌ 未找到非空的 MiniDump (.dmp) 文件")
	}
}

// ================== 直接测试：MiniDumpWriteDump API 可用性 ==================

// TestMiniDumpWriteDumpDirect 验证 MiniDumpWriteDump API 在正常上下文中可用。
// 此测试不触发任何异常，仅生成当前进程的快照转储（不含异常信息）。
func TestMiniDumpWriteDumpDirect(t *testing.T) {
	dir := t.TempDir()

	cfg := &Config{Dir: dir}
	if err := enablePlatform(cfg); err != nil {
		t.Fatalf("enablePlatform 失败: %v", err)
	}

	dumpPath := filepath.Join(dir, "test-direct.dmp")
	pathUTF16, err := windows.UTF16PtrFromString(dumpPath)
	if err != nil {
		t.Fatalf("UTF16 转换失败: %v", err)
	}

	handle, _, _ := syscall.SyscallN(fnCreateFileW,
		uintptr(unsafe.Pointer(pathUTF16)),
		_GENERIC_READ|_GENERIC_WRITE,
		_FILE_SHARE_READ|_FILE_SHARE_WRITE,
		0, _CREATE_ALWAYS, _FILE_ATTRIBUTE_NORMAL, 0,
	)
	if handle == _INVALID_HANDLE {
		t.Fatal("CreateFileW 失败")
	}
	defer syscall.SyscallN(fnCloseHandle, handle)

	hProcess, _, _ := syscall.SyscallN(fnGetCurrentProcess)

	ret, _, lastErr := syscall.SyscallN(fnMiniDumpWriteDump,
		hProcess,
		uintptr(uint32(os.Getpid())),
		handle,
		uintptr(_MiniDumpNormal),
		0, 0, 0,
	)
	if ret == 0 {
		t.Fatalf("MiniDumpWriteDump 返回 FALSE，GetLastError: %v", lastErr)
	}

	info, err := os.Stat(dumpPath)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("MiniDump 文件为空")
	}
	t.Logf("✅ 直接调用 MiniDumpWriteDump 成功: %s (%d bytes)", filepath.Base(dumpPath), info.Size())
}

// ================== 单元测试：isFatalException ==================

func TestIsFatalException(t *testing.T) {
	fatalCodes := []struct {
		name string
		code uint32
	}{
		{"ACCESS_VIOLATION", _EXCEPTION_ACCESS_VIOLATION},
		{"STACK_OVERFLOW", _EXCEPTION_STACK_OVERFLOW},
		{"INT_DIVIDE_BY_ZERO", _EXCEPTION_INT_DIVIDE_BY_ZERO},
		{"ILLEGAL_INSTRUCTION", _EXCEPTION_ILLEGAL_INSTRUCTION},
		{"PRIV_INSTRUCTION", _EXCEPTION_PRIV_INSTRUCTION},
		{"ARRAY_BOUNDS", _EXCEPTION_ARRAY_BOUNDS},
		{"FLT_DENORMAL", _EXCEPTION_FLT_DENORMAL},
		{"FLT_DIVIDE_BY_ZERO", _EXCEPTION_FLT_DIVIDE_BY_ZERO},
		{"FLT_INEXACT", _EXCEPTION_FLT_INEXACT},
		{"FLT_INVALID_OP", _EXCEPTION_FLT_INVALID_OP},
		{"FLT_OVERFLOW", _EXCEPTION_FLT_OVERFLOW},
		{"FLT_STACK_CHECK", _EXCEPTION_FLT_STACK_CHECK},
		{"FLT_UNDERFLOW", _EXCEPTION_FLT_UNDERFLOW},
	}

	for _, tc := range fatalCodes {
		if !isFatalException(tc.code) {
			t.Errorf("isFatalException(0x%08X) [%s] = false，应为 true", tc.code, tc.name)
		}
	}

	nonFatalCodes := []struct {
		name string
		code uint32
	}{
		{"BREAKPOINT", 0x80000003},
		{"DBG_PRINTEXCEPTION_C", 0x40010006},
		{"STATUS_STACK_BUFFER_OVERRUN", 0xC0000409},
		{"RANDOM_CODE", 0x12345678},
		{"ZERO", 0x00000000},
	}

	for _, tc := range nonFatalCodes {
		if isFatalException(tc.code) {
			t.Errorf("isFatalException(0x%08X) [%s] = true，应为 false", tc.code, tc.name)
		}
	}
}

// ================== 单元测试：enablePlatform 基础功能 ==================

func TestEnablePlatformResolvesAllDLLs(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir}

	if err := enablePlatform(cfg); err != nil {
		t.Fatalf("enablePlatform 失败: %v", err)
	}

	checks := []struct {
		name string
		addr uintptr
	}{
		{"MiniDumpWriteDump", fnMiniDumpWriteDump},
		{"CreateFileW", fnCreateFileW},
		{"CloseHandle", fnCloseHandle},
		{"GetCurrentProcess", fnGetCurrentProcess},
	}

	for _, c := range checks {
		if c.addr == 0 {
			t.Errorf("函数 %s 的地址为 0，DLL 解析失败", c.name)
		}
	}

	if preDumpPathUTF16 == nil {
		t.Error("preDumpPathUTF16 未初始化")
	}
	if preProcessID == 0 {
		t.Error("preProcessID 未初始化")
	}
}
