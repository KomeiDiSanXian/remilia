package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage SQLite 存储实现
type SQLiteStorage struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

// NewSQLiteStorage 创建 SQLite 存储
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	storage := &SQLiteStorage{
		db:   db,
		path: dbPath,
	}

	// 初始化表结构
	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return storage, nil
}

// initSchema 初始化数据库表结构
func (s *SQLiteStorage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS kv_store (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL,
		expires_at_ms INTEGER,
		created_at_ms INTEGER NOT NULL,
		updated_at_ms INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_expires_at ON kv_store(expires_at_ms);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Get 获取值
func (s *SQLiteStorage) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var value []byte
	var expiresAtMs sql.NullInt64

	query := `SELECT value, expires_at_ms FROM kv_store WHERE key = ?`
	err := s.db.QueryRow(query, key).Scan(&value, &expiresAtMs)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get key: %w", err)
	}

	// 检查是否过期
	if expiresAtMs.Valid && time.Now().UnixMilli() > expiresAtMs.Int64 {
		// 异步删除过期键
		go s.Delete(key)
		return nil, ErrExpired
	}

	// 返回副本
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Set 设置值
func (s *SQLiteStorage) Set(key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 复制值
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	nowMs := time.Now().UnixMilli()
	var expiresAtMs sql.NullInt64

	if ttl > 0 {
		expiresAtMs.Valid = true
		expiresAtMs.Int64 = time.Now().Add(ttl).UnixMilli()
	}

	query := `
	INSERT INTO kv_store (key, value, expires_at_ms, created_at_ms, updated_at_ms)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		expires_at_ms = excluded.expires_at_ms,
		updated_at_ms = excluded.updated_at_ms
	`

	_, err := s.db.Exec(query, key, valueCopy, expiresAtMs, nowMs, nowMs)
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	return nil
}

// Delete 删除值
func (s *SQLiteStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM kv_store WHERE key = ?`
	_, err := s.db.Exec(query, key)
	if err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}

	return nil
}

// Exists 检查键是否存在
func (s *SQLiteStorage) Exists(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var expiresAtMs sql.NullInt64
	query := `SELECT expires_at_ms FROM kv_store WHERE key = ?`
	err := s.db.QueryRow(query, key).Scan(&expiresAtMs)

	if err != nil {
		return false
	}

	// 检查是否过期
	if expiresAtMs.Valid && time.Now().UnixMilli() > expiresAtMs.Int64 {
		return false
	}

	return true
}

// Keys 列出匹配的键
func (s *SQLiteStorage) Keys(pattern string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 将通配符模式转换为 SQL LIKE 模式
	sqlPattern := convertToSQLPattern(pattern)

	query := `
	SELECT key FROM kv_store 
	WHERE key LIKE ? 
	AND (expires_at_ms IS NULL OR expires_at_ms > ?)
	`

	rows, err := s.db.Query(query, sqlPattern, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("failed to query keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return keys, nil
}

// Clear 清空所有数据
func (s *SQLiteStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM kv_store`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to clear storage: %w", err)
	}

	return nil
}

// CleanExpired 清理过期数据
func (s *SQLiteStorage) CleanExpired() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM kv_store WHERE expires_at_ms IS NOT NULL AND expires_at_ms <= ?`
	result, err := s.db.Exec(query, time.Now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("failed to clean expired: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(count), nil
}

// Size 返回存储的键数量
func (s *SQLiteStorage) Size() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	query := `
	SELECT COUNT(*) FROM kv_store 
	WHERE expires_at_ms IS NULL OR expires_at_ms > ?
	`
	err := s.db.QueryRow(query, time.Now().UnixMilli()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get size: %w", err)
	}

	return count, nil
}

// Close 关闭数据库连接
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Compact 压缩数据库（运行 VACUUM）
func (s *SQLiteStorage) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("failed to compact database: %w", err)
	}

	return nil
}

// Stats 获取数据库统计信息
func (s *SQLiteStorage) Stats() (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]any)

	// 总键数
	var totalKeys int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM kv_store`).Scan(&totalKeys)
	if err != nil {
		return nil, err
	}
	stats["total_keys"] = totalKeys

	// 有效键数（未过期）
	var validKeys int
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM kv_store 
		WHERE expires_at_ms IS NULL OR expires_at_ms > ?
	`, time.Now().UnixMilli()).Scan(&validKeys)
	if err != nil {
		return nil, err
	}
	stats["valid_keys"] = validKeys

	// 过期键数
	stats["expired_keys"] = totalKeys - validKeys

	// 数据库文件大小
	var pageCount, pageSize int
	err = s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
	if err == nil {
		s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
		stats["db_size_bytes"] = pageCount * pageSize
	}

	stats["db_path"] = s.path

	return stats, nil
}

// convertToSQLPattern 转换通配符模式为 SQL LIKE 模式
func convertToSQLPattern(pattern string) string {
	if pattern == "*" {
		return "%"
	}

	// 将 * 替换为 %
	var sqlPattern strings.Builder
	for _, ch := range pattern {
		if ch == '*' {
			sqlPattern.WriteString("%")
		} else if ch == '?' {
			sqlPattern.WriteString("_")
		} else {
			sqlPattern.WriteString(string(ch))
		}
	}

	return sqlPattern.String()
}
