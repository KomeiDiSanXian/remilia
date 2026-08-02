package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// swapper 负责可执行文件的备份、替换与回滚。
//
// 跨平台语义（实测惯例）：
//   - Unix：运行中的文件可直接 rename/覆盖
//   - Windows：运行中的 exe 允许改名但不允许被覆盖——因此替换走
//     "改名自己 → 新文件改名到原路径" 两步，回滚是同样的反向两步
type swapper struct {
	backup bool // 替换前是否备份旧二进制
}

// backupPathFor 返回旧二进制的备份路径（与 exe 同目录，保证同卷原子性）。
func backupPathFor(exePath, fromVersion string) string {
	return exePath + ".old." + strings.TrimPrefix(fromVersion, "v")
}

// swap 将 exePath 替换为 newBinary，返回备份路径（未启用备份时为空串）。
//
// 顺序：备份（改名旧文件）→ 改名新文件到原路径。第二步失败时自动回滚第一步。
func (s *swapper) swap(exePath, newBinary, fromVersion string) (string, error) {
	var backupPath string
	if s.backup {
		backupPath = backupPathFor(exePath, fromVersion)
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("清理旧备份失败: %w", err)
		}
		if err := os.Rename(exePath, backupPath); err != nil {
			return "", fmt.Errorf("备份当前二进制失败: %w", err)
		}
	}

	if err := os.Rename(newBinary, exePath); err != nil {
		// 新文件无法就位：回滚备份，保证现有进程文件不受影响
		if s.backup {
			if rbErr := os.Rename(backupPath, exePath); rbErr != nil {
				return "", fmt.Errorf("替换失败且回滚失败（备份位于 %s）: %v; %w", backupPath, rbErr, err)
			}
		}
		return "", fmt.Errorf("替换二进制失败: %w", err)
	}

	return backupPath, nil
}

// restore 将备份文件回滚到 exePath（当前进程仍在运行）。
//
// 两步走与 swap 相同：先把运行中的文件改名让位，再把备份改名到原路径。
func (s *swapper) restore(exePath, backupPath string) error {
	if backupPath == "" {
		return fmt.Errorf("没有可用的备份")
	}
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份不存在: %w", err)
	}
	// 让位：运行中的 exe 改名（Unix/Windows 均允许）
	tmp := exePath + ".rollback"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理回滚临时文件失败: %w", err)
	}
	if err := os.Rename(exePath, tmp); err != nil {
		return fmt.Errorf("让位当前二进制失败: %w", err)
	}
	if err := os.Rename(backupPath, exePath); err != nil {
		// 尝试把让位的文件放回去
		_ = os.Rename(tmp, exePath)
		return fmt.Errorf("恢复备份失败: %w", err)
	}
	_ = os.Remove(tmp)
	return nil
}

// cleanupBackups 清理 exe 同目录下残留的 .old.* / .rollback / .updater-tmp* 文件
// （保留 backupPath 本身，供 /update rollback 使用）。
func cleanupBackups(exePath string, keepPath string) {
	dir := filepath.Dir(exePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		if full == keepPath {
			continue
		}
		if strings.HasPrefix(name, ".updater-tmp") {
			os.RemoveAll(full)
			continue
		}
		if strings.HasSuffix(name, ".rollback") ||
			(strings.HasPrefix(name, filepath.Base(exePath)+".old.")) {
			os.Remove(full)
		}
	}
}
