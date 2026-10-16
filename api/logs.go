package api

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// LogEntry 日志条目
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// logSlot 是环形缓冲区中的一个日志条目及其单调递增序号。
type logSlot struct {
	seq   uint64
	entry LogEntry
}

// LogBuffer 环形日志缓冲区。
//
// 每个条目携带单调递增序号，消费方（如 SSE 流）按序号增量读取——
// 不能用 len(Recent(...)) 判断是否有新日志：缓冲区写满后长度不再变化。
type LogBuffer struct {
	mu    sync.RWMutex
	ring  []logSlot
	size  int
	head  int
	count int
	seq   uint64
}

func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{ring: make([]logSlot, size), size: size}
}

func (b *LogBuffer) Append(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	b.ring[b.head] = logSlot{seq: b.seq, entry: entry}
	b.head = (b.head + 1) % b.size
	if b.count < b.size {
		b.count++
	}
}

// Recent 返回最新的 n 条日志（n 超过当前条数时返回全部）。
func (b *LogBuffer) Recent(n int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n <= 0 || n > b.count {
		n = b.count
	}
	if n == 0 {
		return nil
	}
	start := (b.head - n + b.size) % b.size
	result := make([]LogEntry, n)
	for i := range n {
		result[i] = b.ring[(start+i)%b.size].entry
	}
	return result
}

// Since 返回序号大于 seq 的日志条目（最多 max 条）以及缓冲区当前最新序号。
//
// 返回的 lastSeq 始终推进到最新序号：即使调用方落后超过缓冲区容量、
// 旧条目已被覆盖，也只丢弃最老的、保留最新的最多 max 条，保证流不会停更。
func (b *LogBuffer) Since(seq uint64, max int) ([]LogEntry, uint64) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.count == 0 {
		return nil, b.seq
	}
	if max <= 0 || max > b.count {
		max = b.count
	}
	start := (b.head - b.count + b.size) % b.size
	entries := make([]LogEntry, 0, max)
	for i := 0; i < b.count; i++ {
		slot := b.ring[(start+i)%b.size]
		if slot.seq > seq {
			entries = append(entries, slot.entry)
		}
	}
	if len(entries) > max {
		entries = entries[len(entries)-max:]
	}
	return entries, b.seq
}

var logBuffer = NewLogBuffer(500)

// AppendLogEntry 直接向全局日志缓冲区追加条目（由 LogCaptureWriter 调用）。
func AppendLogEntry(level, msg string) {
	logBuffer.Append(LogEntry{
		Time:    time.Now().Format("15:04:05.000"),
		Level:   level,
		Message: msg,
	})
}

// ---- HTTP handlers ----

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	n := parseLimit(r.URL.Query().Get("n"), 100)
	writeOK(w, map[string]any{
		"entries": logBuffer.Recent(n),
	})
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	enc := jsontext.NewEncoder(w)
	writeLog := func(entry LogEntry) {
		fmt.Fprint(w, "data: ")
		// 直接流式编码到 ResponseWriter，避免每次分配中间 []byte。
		if err := jsonv2.MarshalEncode(enc, entry); err != nil {
			logger.WithError(err).Error("[API] Failed to encode log entry")
			return
		}
		fmt.Fprint(w, "\n\n")
	}

	// 初始回放最近 50 条，并保留最新序号作为后续增量游标。
	initial, lastSeq := logBuffer.Since(0, 50)
	for _, entry := range initial {
		writeLog(entry)
	}
	flusher.Flush()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			entries, seq := logBuffer.Since(lastSeq, 1000)
			lastSeq = seq
			if len(entries) == 0 {
				continue
			}
			for _, entry := range entries {
				writeLog(entry)
			}
			flusher.Flush()
		}
	}
}
