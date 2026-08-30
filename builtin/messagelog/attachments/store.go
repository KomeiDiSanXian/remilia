package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrTooLarge 附件超过单文件硬上限。
var ErrTooLarge = errors.New("attachment exceeds max size")

// Store 附件二进制存储接口（可插拔：默认文件存储，未来可换 S3 等）。
type Store interface {
	// Put 将 r 的内容原子写入存储，返回内容哈希 storage key 与实际写入字节数。
	// maxSize > 0 时超过即返回 ErrTooLarge（不残留临时文件）。
	Put(r io.Reader, maxSize int64) (key string, size int64, err error)
	// PutTemp 两阶段写入的第一阶段：流式写入临时文件并计算内容哈希。
	// 返回 storage key、字节数与临时文件路径。临时文件对 GC 不可见，
	// 须由调用方在持有 SQLite 写锁事务内 CommitTemp 提交（与引用行原子）。
	PutTemp(r io.Reader, maxSize int64) (key string, size int64, tmp string, err error)
	// CommitTemp 两阶段写入的第二阶段（须在持有 SQLite 写锁事务内调用）：
	// 目标 key 已存在（内容去重）时丢弃临时文件；否则原子 rename 为最终文件。
	// 与 GC 的删除在同一把写锁下串行化，杜绝「文件可见后、引用行就绪前」
	// 被回收的悬空窗口。成功或失败后临时文件均不残留。
	CommitTemp(key, tmp string) error
	// DiscardTemp 丢弃未提交的临时文件（两阶段写入中途失败时调用）。
	DiscardTemp(tmp string) error
	// Open 打开已存储的 key。
	Open(key string) (io.ReadCloser, error)
	// Delete 删除 key 对应的文件；文件不存在返回 nil。
	Delete(key string) error
	// Size 返回 key 对应文件大小；不存在返回 os.ErrNotExist。
	Size(key string) (int64, error)
	// Keys 列出全部已存储 key（GC 用，跳过未完成的临时文件）。
	Keys() ([]string, error)
	// TotalSize 返回当前存储占用字节数（磁盘预算依据）。
	TotalSize() int64
}

// FileStore 默认文件存储：data/attachments/<sha256>。
//
// 文件名即内容哈希（storage key），天然去重且 GC 枚举简单；
// 相比 <sha256>.<ext> 省略扩展名——MIME 已存于元数据，扩展名不影响
// 内容寻址与消费。
type FileStore struct {
	dir   string
	mu    sync.Mutex
	total int64
}

// NewFileStore 打开（或创建）附件存储目录并扫描现有文件统计占用。
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create attachment dir: %w", err)
	}
	fs := &FileStore{dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan attachment dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fs.total += info.Size()
	}
	return fs, nil
}

// Put 单阶段便捷入口：PutTemp + CommitTemp。
func (s *FileStore) Put(r io.Reader, maxSize int64) (string, int64, error) {
	key, size, tmp, err := s.PutTemp(r, maxSize)
	if err != nil {
		return "", 0, err
	}
	if err := s.CommitTemp(key, tmp); err != nil {
		return "", 0, err
	}
	return key, size, nil
}

// PutTemp 流式写入：sha256 边算边写临时文件，不产生最终可见文件。
// 返回临时文件路径，由调用方 CommitTemp 提交或 DiscardTemp 丢弃。
func (s *FileStore) PutTemp(r io.Reader, maxSize int64) (string, int64, string, error) {
	h := sha256.New()
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if maxSize > 0 && written+int64(n) > maxSize {
				cleanup()
				return "", 0, "", ErrTooLarge
			}
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				cleanup()
				return "", 0, "", fmt.Errorf("write temp file: %w", werr)
			}
			h.Write(buf[:n])
			written += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			cleanup()
			return "", 0, "", fmt.Errorf("read download body: %w", rerr)
		}
	}

	key := hex.EncodeToString(h.Sum(nil))
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", 0, "", fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", 0, "", fmt.Errorf("close temp file: %w", err)
	}
	return key, written, tmpName, nil
}

// CommitTemp 提交两阶段写入的临时文件：目标已存在则去重复用（丢弃临时文件），
// 否则原子 rename。调用方必须持有 SQLite 写锁事务，保证与 GC 删除互斥。
func (s *FileStore) CommitTemp(key, tmp string) error {
	if tmp == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	final := filepath.Join(s.dir, key)
	if _, err := os.Stat(final); err == nil {
		// 内容已存在：去重复用，丢弃本次临时文件
		os.Remove(tmp)
		return nil
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename attachment: %w", err)
	}
	if info, err := os.Stat(final); err == nil {
		s.total += info.Size()
	}
	return nil
}

// DiscardTemp 丢弃未提交的临时文件；不存在视为成功。
func (s *FileStore) DiscardTemp(tmp string) error {
	if tmp == "" {
		return nil
	}
	err := os.Remove(tmp)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Open 打开已存储文件。
func (s *FileStore) Open(key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.dir, key))
}

// Delete 删除文件；不存在视为成功。
func (s *FileStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, key)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	s.total -= info.Size()
	return nil
}

// Size 返回文件大小。
func (s *FileStore) Size(key string) (int64, error) {
	info, err := os.Stat(filepath.Join(s.dir, key))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Keys 列出全部已存储 key（跳过临时文件）。
func (s *FileStore) Keys() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		keys = append(keys, e.Name())
	}
	return keys, nil
}

// TotalSize 返回当前存储占用字节数。
func (s *FileStore) TotalSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}
