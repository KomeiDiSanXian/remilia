package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// LogEntry 日志条目
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// LogBuffer 环形日志缓冲区
type LogBuffer struct {
	mu    sync.RWMutex
	ring  []LogEntry
	size  int
	head  int
	count int
}

func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{ring: make([]LogEntry, size), size: size}
}

func (b *LogBuffer) Append(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ring[b.head] = entry
	b.head = (b.head + 1) % b.size
	if b.count < b.size {
		b.count++
	}
}

func (b *LogBuffer) Recent(n int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n <= 0 || n > b.count {
		n = b.count
	}
	if n == 0 {
		return nil
	}
	start := (b.head - b.count + b.size) % b.size
	result := make([]LogEntry, n)
	for i := range n {
		result[i] = b.ring[(start+i)%b.size]
	}
	return result
}

var logBuffer = NewLogBuffer(500)

// AppendLogEntry 直接向全局日志缓冲区追加条目（由 main 的 zerolog hook 调用）。
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

	for _, entry := range logBuffer.Recent(50) {
		data, _ := json.Marshal(entry)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastCount := len(logBuffer.Recent(9999))
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			entries := logBuffer.Recent(9999)
			if len(entries) > lastCount {
				for _, entry := range entries[lastCount:] {
					data, _ := json.Marshal(entry)
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
				lastCount = len(entries)
				flusher.Flush()
			}
		}
	}
}
