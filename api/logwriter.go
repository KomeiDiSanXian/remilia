package api

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"sync"
)

// LogCaptureWriter 实现 io.Writer，解析 zerolog JSON 输出并存入环形缓冲区。
type LogCaptureWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// logLine 是 zerolog JSON 行中关注的字段。
type logLine struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

func (w *LogCaptureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	w.buf.Write(p)

	// 逐行处理
	for {
		line, err := w.buf.ReadBytes('\n')
		if err != nil {
			// 未读完的行放回去
			w.buf.Write(line)
			break
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// 解析 zerolog JSON 行：{"level":"info","time":"...","message":"..."}
		level, msg := parseLogLine(line)
		if msg == "" {
			msg = string(line)
		}
		AppendLogEntry(level, msg)
	}
	return n, nil
}

// parseLogLine 使用 encoding/json/v2（Go 1.27）解析 zerolog 行：
// 字段顺序无关，字符串转义（如 \n）由解码器正确处理。
// 无法解析时返回 ("info", "")，由调用方回退到原始行。
func parseLogLine(line []byte) (level, msg string) {
	var entry logLine
	if err := jsonv2.Unmarshal(line, &entry); err != nil {
		return "info", ""
	}
	if entry.Level == "" {
		entry.Level = "info"
	}
	return entry.Level, entry.Message
}

// NewLogCaptureWriter 创建一个日志捕获 writer。
// 使用前调用 logger.SetExtraWriter(w)，然后 logger.Init()。
func NewLogCaptureWriter() *LogCaptureWriter {
	return &LogCaptureWriter{}
}
