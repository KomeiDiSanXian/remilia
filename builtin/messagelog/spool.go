package messagelog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// errSpoolFull spool 达到磁盘上限（Durable 语义下由调用方阻塞生产者）。
var errSpoolFull = errors.New("spool full")

// spoolRecord 一条来自 spool 的记录；next 是消费到该记录后的文件偏移
// （ack 到 next 即视为已消费）。
type spoolRecord struct {
	entry RecordEntry
	next  int64
}

// spool 是 append-only 的持久缓冲（at-least-once 兜底）。
//
// 记录格式：JSONL（每行一条 RecordEntry）。cursor = 已确认（SQLite 已提交）
// 的字节偏移，仅在 ack 时推进并持久化；crash 后从未确认偏移重放，
// 由 event_id 幂等（INSERT OR IGNORE）保证最终恰好一条。
type spool struct {
	mu         sync.Mutex
	dir        string
	file       string
	cursorFile string
	maxSize    int64

	f      *os.File
	size   int64 // 文件当前字节数
	cursor int64 // 已确认偏移
	// records 未确认记录数（启动时扫描一次，append/ack 增量维护）
	records int64
}

// openSpool 打开（或创建）spool。cursor 无效/越界时回退 0（重放 + 幂等兜底）。
func openSpool(dir string, maxSize int64) (*spool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create spool dir: %w", err)
	}
	s := &spool{
		dir:        dir,
		file:       filepath.Join(dir, "spool.log"),
		cursorFile: filepath.Join(dir, "spool.cursor"),
		maxSize:    maxSize,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	mlMetricsInst.spoolSizeBytes.Set(float64(s.size))
	mlMetricsInst.spoolRecords.Set(float64(s.records))
	return s, nil
}

func (s *spool) load() error {
	f, err := os.OpenFile(s.file, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open spool file: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat spool file: %w", err)
	}
	s.f = f
	s.size = st.Size()

	cursor := int64(0)
	if data, err := os.ReadFile(s.cursorFile); err == nil {
		if v, err := strconv.ParseInt(string(data), 10, 64); err == nil {
			cursor = v
		}
	}
	if cursor < 0 || cursor > s.size {
		cursor = 0
	}
	s.cursor = cursor
	s.records = countRecords(s.file, cursor, s.size)
	return nil
}

// countRecords 统计 [from, to) 区间的记录行数（跳过损坏行，损坏行按已消费处理）。
func countRecords(path string, from, to int64) int64 {
	if from >= to {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return 0
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var n int64
	for sc.Scan() {
		if int64(len(sc.Bytes())) == 0 {
			continue
		}
		n++
	}
	return n
}

// append 追加一条记录。达到 maxSize 时返回 errSpoolFull（不写入）。
func (s *spool) append(entry RecordEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.size >= s.maxSize {
		return errSpoolFull
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal spool record: %w", err)
	}
	data = append(data, '\n')
	n, err := s.f.Write(data)
	if err != nil {
		return fmt.Errorf("write spool: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("sync spool: %w", err)
	}
	s.size += int64(n)
	s.records++
	mlMetricsInst.spoolSizeBytes.Set(float64(s.size))
	mlMetricsInst.spoolRecords.Set(float64(s.records))
	return nil
}

// read 读取从 offset 开始的最多 n 条未确认记录（不推进 cursor）。
// 损坏行跳过并直接推进 cursor（无法解析的数据不阻塞消费）。
func (s *spool) read(offset int64, n int) ([]spoolRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < s.cursor {
		offset = s.cursor
	}
	if offset >= s.size || n <= 0 {
		return nil, nil
	}
	f, err := os.Open(s.file)
	if err != nil {
		return nil, fmt.Errorf("open spool for read: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []spoolRecord
	for sc.Scan() && len(out) < n {
		line := sc.Bytes()
		if len(line) == 0 {
			offset++
			continue
		}
		next := offset + int64(len(line)) + 1
		var e RecordEntry
		if err := json.Unmarshal(line, &e); err != nil {
			logger.WithError(err).Warn("[MessageLog] spool corrupt record skipped")
			s.cursor = next
			if s.records > 0 {
				s.records--
			}
			if err := s.persistCursorLocked(); err != nil {
				return out, err
			}
			offset = next
			continue
		}
		offset = next
		out = append(out, spoolRecord{entry: e, next: next})
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// ack 推进 cursor 到 offset（SQLite 已提交）并扣减已消费记录数。
// 全部消费完时截断文件回收空间。
func (s *spool) ack(offset int64, recordsConsumed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset <= s.cursor {
		return nil
	}
	if offset > s.size {
		offset = s.size
	}
	if recordsConsumed > 0 && s.records >= int64(recordsConsumed) {
		s.records -= int64(recordsConsumed)
	}
	s.cursor = offset
	if err := s.persistCursorLocked(); err != nil {
		return err
	}
	if s.cursor == s.size {
		// 全部消费：重建空文件回收空间（Windows 上对 O_APPEND 句柄
		// Truncate 会被拒绝，采用关闭 + 删除 + 重建）。
		if err := s.f.Close(); err != nil {
			return fmt.Errorf("close spool: %w", err)
		}
		if err := os.Remove(s.file); err != nil {
			return fmt.Errorf("remove spool: %w", err)
		}
		f, err := os.OpenFile(s.file, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("recreate spool: %w", err)
		}
		s.f = f
		s.size = 0
		s.cursor = 0
		s.records = 0
		if err := s.persistCursorLocked(); err != nil {
			return err
		}
	}
	mlMetricsInst.spoolSizeBytes.Set(float64(s.size))
	mlMetricsInst.spoolRecords.Set(float64(s.records))
	return nil
}

// unacked 返回未确认记录数（指标用）。
func (s *spool) unacked() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records
}

// oldestAge 返回最旧未确认记录的年龄（秒）；无未确认记录返回 0。
func (s *spool) oldestAge() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records <= 0 || s.cursor >= s.size {
		return 0
	}
	f, err := os.Open(s.file)
	if err != nil {
		return 0
	}
	defer f.Close()
	if _, err := f.Seek(s.cursor, io.SeekStart); err != nil {
		return 0
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		return 0
	}
	var e RecordEntry
	if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
		return 0
	}
	return time.Since(e.Timestamp).Seconds()
}

func (s *spool) persistCursorLocked() error {
	tmp := s.cursorFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(s.cursor, 10)), 0o644); err != nil {
		return fmt.Errorf("write spool cursor: %w", err)
	}
	if err := os.Rename(tmp, s.cursorFile); err != nil {
		return fmt.Errorf("rename spool cursor: %w", err)
	}
	return nil
}

// close 释放文件句柄（Stop 时调用；Windows 上否则阻塞目录清理）。
func (s *spool) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		err := s.f.Close()
		s.f = nil
		return err
	}
	return nil
}
