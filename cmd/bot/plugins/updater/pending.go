package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// pendingFileName 待确认更新标记文件名（位于 data 目录）。
const pendingFileName = "pending.json"

// defaultPendingPath 崩溃窗口兜底的默认标记路径（相对 cwd，与 cmd/bot 的
// WithDataDir("data/updater") 一致；var 便于测试注入）。
var defaultPendingPath = filepath.Join("data", "updater", pendingFileName)

// PendingUpdate 是待确认更新的标记内容（由旧进程写入、新进程消费）。
type PendingUpdate struct {
	FromVersion string    `json:"from_version"` // 旧版本号
	ToVersion   string    `json:"to_version"`   // 新版本号（tag 不含 v 前缀）
	BackupPath  string    `json:"backup_path"`  // 旧二进制备份路径（空 = 未启用备份）
	ExePath     string    `json:"exe_path"`     // 可执行文件路径
	At          time.Time `json:"at"`
}

// writePending 原子写入待确认更新标记（临时文件 + rename，中途崩溃不留半截文件）。
func writePending(markerPath string, p *PendingUpdate) error {
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return fmt.Errorf("创建标记目录失败: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := markerPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入标记临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, markerPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("落盘标记失败: %w", err)
	}
	return nil
}

// readPending 读取标记；不存在时返回 nil。
func readPending(markerPath string) (*PendingUpdate, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p PendingUpdate
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("解析更新标记失败: %w", err)
	}
	return &p, nil
}

// removePending 删除标记文件（不存在时静默成功）。
func removePending(markerPath string) error {
	err := os.Remove(markerPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HandlePendingUpdate 必须在新进程 main 的最早期调用（绑定端口之前）。
//
// 触发条件（满足其一即执行确认流程，否则直接返回 nil）：
//   - 环境变量 REMILIA_UPDATED_BY 存在（更新流程拉起的子进程）
//   - 默认标记路径 data/updater/pending.json 存在（覆盖"替换后、拉起前崩溃"的窗口：
//     下次手动启动时仍会确认/回滚）
//
// 流程：
//  1. 若带 REMILIA_UPDATED_BY：等待旧进程（父进程）退出——旧进程仍在优雅关闭，
//     直接启动会端口冲突
//  2. 读取更新标记，校验自身版本与标记的 ToVersion 一致
//  3. 一致 → 更新成功：删除标记，清理残留临时文件
//  4. 不一致 → 更新失败：回滚旧备份到 exe 路径，重新执行旧版本，当前进程退出
//     （标记保留给旧进程确认并清理，防再次启动时重复回滚）
//
// 返回错误时调用方应记日志后继续启动（回滚路径已尽力执行）。
func HandlePendingUpdate() error {
	pidStr := os.Getenv(envWaitParent)
	markerPath := os.Getenv(envUpdateMarker)
	if markerPath == "" {
		// 崩溃窗口兜底：按默认数据目录探测标记（cmd/bot 的 dataDir 为 data/updater）
		markerPath = defaultPendingPath
	}

	if pidStr != "" {
		fmt.Fprintf(os.Stderr, "[updater] 更新后首次启动，等待旧进程（pid=%s）退出...\n", pidStr)
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			fmt.Fprintf(os.Stderr, "[updater] 环境变量 %s 非法（%q），跳过等待\n", envWaitParent, pidStr)
		} else if err := waitProcessExit(pid, 60*time.Second, 300*time.Millisecond); err != nil {
			fmt.Fprintf(os.Stderr, "[updater] %v，继续启动\n", err)
		}
	}

	pending, err := readPending(markerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[updater] 读取更新标记失败: %v\n", err)
		return err
	}
	if pending == nil {
		if pidStr != "" {
			fmt.Fprintln(os.Stderr, "[updater] 更新标记不存在（可能已被消费），跳过确认")
		}
		return nil
	}

	current := CurrentVersion()
	if current != "" && pending.ToVersion != "" && sameVersion(current, pending.ToVersion) {
		// 更新成功
		_ = removePending(markerPath)
		cleanupBackups(pending.ExePath, pending.BackupPath)
		fmt.Fprintf(os.Stderr, "[updater] 更新确认成功：v%s → v%s\n", pending.FromVersion, pending.ToVersion)
		return nil
	}

	// 版本不匹配：回滚
	fmt.Fprintf(os.Stderr, "[updater] 版本校验失败（当前 %q，期望 %q），回滚到 v%s\n",
		current, pending.ToVersion, pending.FromVersion)
	sw := &swapper{backup: true}
	if err := sw.restore(pending.ExePath, pending.BackupPath); err != nil {
		// 备份不可用（未启用备份 / 更新未到替换阶段 / 备份被手动移除）：
		// 没有可回滚的现场，保留标记只会让每次启动都重复此路径——删除标记后继续。
		// 此时磁盘上的二进制与标记无关（标记先于替换写入），直接运行即可；
		// 若备份文件仍在，用户仍可手动 /update rollback。
		_ = removePending(markerPath)
		fmt.Fprintf(os.Stderr, "[updater] 备份不可用，已清除更新标记并继续启动（备份位置: %s）: %v\n",
			pending.BackupPath, err)
		return nil
	}
	fmt.Fprintln(os.Stderr, "[updater] 已回滚旧版本，启动旧进程...")
	if err := restartCurrent(pending.ExePath); err != nil {
		return fmt.Errorf("回滚后重启失败: %w", err)
	}
	// 标记保留：旧进程（版本与 ToVersion 一致）下次启动时自行确认并清理残留。
	// 当前进程是坏版本二进制，尚未初始化任何资源，直接退出最安全——
	// 若继续启动会与回滚进程争抢端口。
	os.Exit(0)
	return nil
}

// restartCurrent 重新执行指定二进制（回滚路径：新进程自我替换为旧版本）。
func restartCurrent(exePath string) error {
	cmd := newExecCommand(exePath, os.Args[1:]...)
	cmd.Env = envWithout(envWaitParent, envUpdateMarker)
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("回滚后重启失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
