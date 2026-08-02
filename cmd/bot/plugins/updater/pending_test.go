package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// deadPID 是不存在的进程号（Unix kill(pid,0) 返回 ESRCH；Windows OpenProcess 失败）。
const deadPID = "2147483647"

func TestHandlePendingUpdateSuccess(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "remilia"+exeSuffix())
	backup := filepath.Join(t.TempDir(), "remilia.old.1.29.0")
	os.WriteFile(exePath, []byte("new"), 0o755)
	os.WriteFile(backup, []byte("old"), 0o755)

	markerPath := filepath.Join(t.TempDir(), "pending.json")
	pending := &PendingUpdate{
		FromVersion: "1.29.0",
		ToVersion:   CurrentVersion(), // 与测试二进制版本一致 → 确认成功
		BackupPath:  backup,
		ExePath:     exePath,
	}
	if err := writePending(markerPath, pending); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envWaitParent, deadPID)
	t.Setenv(envUpdateMarker, markerPath)

	if err := HandlePendingUpdate(); err != nil {
		t.Fatalf("HandlePendingUpdate: %v", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("成功后标记应被删除")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Error("确认成功后备份应保留（供 rollback）")
	}
}

func TestHandlePendingUpdateRollback(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "remilia"+exeSuffix())
	backup := filepath.Join(t.TempDir(), "remilia.old.1.29.0")
	os.WriteFile(exePath, []byte("bad-new-binary"), 0o755)
	os.WriteFile(backup, []byte("good-old-binary"), 0o755)

	markerPath := filepath.Join(t.TempDir(), "pending.json")
	pending := &PendingUpdate{
		FromVersion: "1.29.0",
		ToVersion:   "9.9.9", // 与当前版本不匹配 → 触发回滚
		BackupPath:  backup,
		ExePath:     exePath,
	}
	if err := writePending(markerPath, pending); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envWaitParent, deadPID)
	t.Setenv(envUpdateMarker, markerPath)

	// 注入必然失败的 exec（真实重启不可在测试中发生）
	oldExec := newExecCommand
	newExecCommand = func(path string, args ...string) *exec.Cmd {
		return exec.Command("definitely-not-a-real-binary-xyz-123")
	}
	defer func() { newExecCommand = oldExec }()

	err := HandlePendingUpdate()
	if err == nil {
		t.Fatal("应返回回滚后重启失败的错误")
	}
	if !strings.Contains(err.Error(), "回滚后重启失败") {
		t.Errorf("err = %v", err)
	}

	got, _ := os.ReadFile(exePath)
	if string(got) != "good-old-binary" {
		t.Errorf("exe 应恢复为旧版本，got %q", got)
	}
	// 标记保留给回滚进程（旧版本二进制）下次启动时确认并清理
	if _, err := os.Stat(markerPath); err != nil {
		t.Error("回滚后标记应保留（由旧进程确认消费）")
	}
}

func TestHandlePendingUpdateCrashWindowFallback(t *testing.T) {
	// 模拟"swap 后、spawn 前崩溃"：无任何环境变量，仅默认路径存在标记。
	// 版本一致 → 确认成功并清理。
	oldDefault := defaultPendingPath
	defaultPendingPath = filepath.Join(t.TempDir(), "pending.json")
	defer func() { defaultPendingPath = oldDefault }()

	os.WriteFile(defaultPendingPath, []byte(`{"from_version":"1.29.0","to_version":"`+CurrentVersion()+`","backup_path":"","exe_path":"dummy"}`), 0o600)

	if err := HandlePendingUpdate(); err != nil {
		t.Fatalf("HandlePendingUpdate: %v", err)
	}
	if _, err := os.Stat(defaultPendingPath); !os.IsNotExist(err) {
		t.Error("确认后标记应被删除")
	}
}

func TestHandlePendingUpdateNoEnv(t *testing.T) {
	// 普通启动（无 REMILIA_UPDATED_BY、无标记）→ 直接返回 nil
	oldDefault := defaultPendingPath
	defaultPendingPath = filepath.Join(t.TempDir(), "pending.json")
	defer func() { defaultPendingPath = oldDefault }()

	if err := HandlePendingUpdate(); err != nil {
		t.Fatalf("普通启动不应报错: %v", err)
	}
}
