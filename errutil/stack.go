package errutil

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var (
	stackTraceEnabled     bool
	stackTraceEnabledOnce sync.Once
)

// ShouldCaptureStack 检查是否启用了堆栈跟踪捕获。
// 可通过调用 EnableStackTrace(true) 或将环境变量 REMILIA_STACK_TRACE 设为 "true" 来启用。
func ShouldCaptureStack() bool {
	if stackTraceEnabled {
		return true
	}
	stackTraceEnabledOnce.Do(func() {
		if os.Getenv("REMILIA_STACK_TRACE") == "true" {
			stackTraceEnabled = true
		}
	})
	return stackTraceEnabled
}

// EnableStackTrace 全局启用或禁用堆栈跟踪捕获。
func EnableStackTrace(enabled bool) {
	stackTraceEnabled = enabled
}

// IsStackTraceEnabled 返回当前是否启用了堆栈跟踪捕获。
func IsStackTraceEnabled() bool {
	return stackTraceEnabled
}

// CaptureStack 捕获当前调用栈并以字符串形式返回。
// 会过滤掉 runtime 和 testing 帧，保持输出简洁。
func CaptureStack() string {
	const maxStackDepth = 32
	var pcs [maxStackDepth]uintptr
	n := runtime.Callers(2, pcs[:])

	frames := runtime.CallersFrames(pcs[:n])
	var lines []string

	for {
		frame, more := frames.Next()

		shouldInclude := true
		if strings.Contains(frame.File, "/runtime/") || strings.Contains(frame.File, "\\runtime\\") {
			shouldInclude = false
		} else if strings.Contains(frame.File, "/testing/testing.go") || strings.Contains(frame.File, "\\testing\\testing.go") {
			shouldInclude = false
		}

		if shouldInclude {
			lines = append(lines, frame.File+":"+strconv.Itoa(frame.Line)+" "+frame.Function)
		}
		if !more {
			break
		}
	}

	if len(lines) == 0 {
		return "no stack trace available"
	}
	return strings.Join(lines, "\n")
}
