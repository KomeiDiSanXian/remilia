package attachments

import (
	"context"
	"database/sql"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// gcLoop 周期执行引用计数 GC（grace_period 过后回收孤儿二进制）。
func (m *Manager) gcLoop(ctx context.Context) {
	interval := m.cfg.GCInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.triggerGC:
			if err := m.gcOnce(ctx); err != nil {
				logger.WithError(err).Warn("[attachments] GC round failed")
			}
		case <-ticker.C:
			if err := m.gcOnce(ctx); err != nil {
				logger.WithError(err).Warn("[attachments] GC round failed")
			}
		}
	}
}

// gcOnce 执行一轮 GC：
//  1. 枚举 Store 全部 key；
//  2. 对每个 key 以 COUNT(*)（message_attachments 引用数）为准做 reconcile；
//  3. 引用数为 0 且文件 mtime 超过 grace_period → 在 BEGIN IMMEDIATE 写锁
//     事务内删除二进制。
//
// 已删除的二进制的引用行标记为 deleted（与「从未下载成功」的 failed 区分）。
func (m *Manager) gcOnce(ctx context.Context) error {
	if m == nil || m.db == nil || m.store == nil {
		return nil
	}
	metricsInst.gcRunsTotal.Inc()
	keys, err := m.store.Keys()
	if err != nil {
		return err
	}
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	var reclaimed int64
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		info, err := os.Stat(storePath(m.store, key))
		if err != nil {
			continue
		}
		if m.cfg.GracePeriod > 0 && time.Since(info.ModTime()) < m.cfg.GracePeriod {
			continue
		}
		ok, err := m.deleteIfUnreferenced(ctx, sqlDB, key)
		if err != nil {
			logger.WithError(err).Warn("[attachments] GC delete skipped")
			continue
		}
		if ok {
			reclaimed++
		}
	}
	metricsInst.gcDeletedTotal.Add(float64(reclaimed))
	m.budget.refresh(metricsInst)
	m.refreshStatusMetrics()
	return nil
}

// deleteIfUnreferenced 在 BEGIN IMMEDIATE 写锁事务内核对引用数并删除二进制。
//
// SQLite 同一时刻仅一个写者：GC 持有写锁期间，任何并发引用写入（下载完成
// 标记 ready / flush 直写 storage_key）都会被阻塞，从根上消除
// 「计数为 0 → 删除瞬间新引用插入」的误删窗口（共享 sha256 不误删）。
//
// 先标记引用行 deleted、再删二进制、最后提交；任一步失败整体回滚，
// 下一轮 GC 重试。
func (m *Manager) deleteIfUnreferenced(ctx context.Context, sqlDB *sql.DB, key string) (bool, error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var refs int64
	if err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM message_attachments WHERE storage_key = ?", key).Scan(&refs); err != nil {
		return false, err
	}
	if refs > 0 {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return false, err
		}
		committed = true
		return false, nil
	}

	if _, err := conn.ExecContext(ctx,
		"UPDATE message_attachments SET status = ? WHERE storage_key = ?", StatusDeleted, key); err != nil {
		return false, err
	}
	if err := m.store.Delete(key); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

// storePath 取 key 的存储路径（供 GC stat 使用）。
func storePath(s Store, key string) string {
	if fs, ok := s.(*FileStore); ok {
		return fs.dir + "/" + key
	}
	return key
}
