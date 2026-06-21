package api

import (
	"bytes"
	"strings"
	"sync"
)

// LogCaptureWriter 实现 io.Writer，解析 zerolog JSON 输出并存入环形缓冲区。
type LogCaptureWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
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

		// 尝试解析 zerolog JSON 行：{"level":"info","time":"...","message":"..."}
		level := extractField(line, `"level":"`)
		msg := extractField(line, `"message":"`)
		if level == "" {
			level = "info"
		}
		if msg == "" {
			msg = string(line)
		}
		AppendLogEntry(level, msg)
	}
	return n, nil
}

func extractField(data []byte, prefix string) string {
	start := bytes.Index(data, []byte(prefix))
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := bytes.IndexByte(data[start:], '"')
	if end < 0 {
		return ""
	}
	return strings.ReplaceAll(string(data[start:start+end]), `\n`, "\n")
}

// NewLogCaptureWriter 创建一个日志捕获 writer。
// 使用前调用 logger.SetExtraWriter(w)，然后 logger.Init()。
func NewLogCaptureWriter() *LogCaptureWriter {
	return &LogCaptureWriter{}
}
