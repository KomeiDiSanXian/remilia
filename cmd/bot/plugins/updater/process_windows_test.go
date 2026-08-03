//go:build windows

package updater

import (
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// helperChildEnv 标记测试二进制以"辅助子进程"模式运行。
const helperChildEnv = "REMILIA_UPDATER_HELPER_CHILD"

// TestStartDetachedChildHelper 是被 TestStartDetachedChildBreakawayFallback
// 拉起的辅助子进程：正常结束测试即退出（exit 0），用于验证子进程能被创建并运行。
// 注意不能调用 os.Exit：Go 1.26 起测试中的 os.Exit 会被 testing 拦截并 panic。
func TestStartDetachedChildHelper(t *testing.T) {
	if os.Getenv(helperChildEnv) != "1" {
		t.Skip("helper 仅作为子进程运行")
	}
}

// TestStartDetachedChildBreakawayFallback 验证：父进程位于不允许脱离的
// KILL_ON_JOB_CLOSE Job 中时，startDetachedChild 的 CREATE_BREAKAWAY_FROM_JOB
// 首次尝试失败后能回退到普通分离启动。
//
// 回归背景：此前回退路径用 *cmd 复制 Cmd 后二次 Start，但 exec.Cmd 的
// startCalled 标志不可重置，复制品必然报 "exec: already started"——回退永远失败，
// 处于此类 Job 的环境下更新会错误地触发"启动新进程失败"并回滚。
func TestStartDetachedChildBreakawayFallback(t *testing.T) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Skipf("CreateJobObject: %v", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		t.Skipf("SetInformationJobObject: %v", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		// 父环境已把进程放入不允许嵌套的 Job 时无法自建测试 Job
		t.Skipf("AssignProcessToJobObject: %v", err)
	}
	// 注意：Job 句柄必须在子进程退出后再关闭，否则 KILL_ON_JOB_CLOSE 会杀掉它。

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestStartDetachedChildHelper")
	cmd.Env = append(os.Environ(), helperChildEnv+"=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := startDetachedChild(cmd); err != nil {
		t.Fatalf("startDetachedChild（breakaway 被拒，期望回退成功）: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := cmd.Process.Wait()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child wait: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("子进程 30s 未退出")
	}

	windows.CloseHandle(job) // 子进程已退出，关闭安全
}
