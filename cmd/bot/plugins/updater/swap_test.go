package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwap(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "remilia")
	newBin := filepath.Join(dir, "new-remilia")
	if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBin, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	sw := &swapper{backup: true}
	backup, err := sw.swap(exePath, newBin, "v1.30.0")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if backup != filepath.Join(dir, "remilia.old.1.30.0") {
		t.Errorf("backup path = %q", backup)
	}

	got, _ := os.ReadFile(exePath)
	if string(got) != "new" {
		t.Error("exe should now be new binary")
	}
	got, _ = os.ReadFile(backup)
	if string(got) != "old" {
		t.Error("backup should hold old binary")
	}

	// 回滚
	if err := sw.restore(exePath, backup); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ = os.ReadFile(exePath)
	if string(got) != "old" {
		t.Error("restore should bring back old binary")
	}
}

func TestSwapNoBackup(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "remilia")
	newBin := filepath.Join(dir, "new-remilia")
	os.WriteFile(exePath, []byte("old"), 0o755)
	os.WriteFile(newBin, []byte("new"), 0o755)

	sw := &swapper{backup: false}
	backup, err := sw.swap(exePath, newBin, "v1.30.0")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if backup != "" {
		t.Error("no backup expected")
	}
}

func TestSwapRollbackOnFailure(t *testing.T) {
	// 让新文件改名失败：newBin 指向不存在的路径
	dir := t.TempDir()
	exePath := filepath.Join(dir, "remilia")
	os.WriteFile(exePath, []byte("old"), 0o755)

	sw := &swapper{backup: true}
	_, err := sw.swap(exePath, filepath.Join(dir, "missing-new"), "v1.30.0")
	if err == nil {
		t.Fatal("swap should fail")
	}
	// 原文件应被回滚回来
	got, _ := os.ReadFile(exePath)
	if string(got) != "old" {
		t.Error("exe should be restored after failed swap")
	}
}

func TestRestoreNoBackup(t *testing.T) {
	sw := &swapper{backup: true}
	if err := sw.restore(t.TempDir()+"/x", ""); err == nil {
		t.Error("restore with empty backup should fail")
	}
}

func TestCleanupBackups(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "remilia")
	keep := filepath.Join(dir, "remilia.old.1.29.0")
	for _, f := range []string{
		keep,
		filepath.Join(dir, "remilia.old.1.28.0"),
		filepath.Join(dir, "remilia.rollback"),
		filepath.Join(dir, ".updater-tmp-stale"),
		filepath.Join(dir, "unrelated.txt"),
	} {
		os.WriteFile(f, []byte("x"), 0o600)
	}

	cleanupBackups(exe, keep)

	for _, gone := range []string{
		filepath.Join(dir, "remilia.old.1.28.0"),
		filepath.Join(dir, "remilia.rollback"),
		filepath.Join(dir, ".updater-tmp-stale"),
	} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("should be cleaned: %s", gone)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("kept backup should remain")
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.txt")); err != nil {
		t.Error("unrelated files must not be touched")
	}
}
