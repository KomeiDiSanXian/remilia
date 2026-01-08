package remilia

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

// shouldCaptureStack checks if stack trace capture is enabled.
func shouldCaptureStack() bool {
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

func EnableStackTrace(enabled bool) { stackTraceEnabled = enabled }

func IsStackTraceEnabled() bool { return stackTraceEnabled }

func captureStack() string {
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
