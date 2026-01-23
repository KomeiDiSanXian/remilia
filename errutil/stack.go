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

// ShouldCaptureStack checks if stack trace capture is enabled.
// Stack traces can be enabled either by calling EnableStackTrace(true)
// or by setting the REMILIA_STACK_TRACE environment variable to "true".
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

// EnableStackTrace enables or disables stack trace capture globally.
func EnableStackTrace(enabled bool) {
	stackTraceEnabled = enabled
}

// IsStackTraceEnabled returns whether stack trace capture is currently enabled.
func IsStackTraceEnabled() bool {
	return stackTraceEnabled
}

// CaptureStack captures the current call stack and returns it as a string.
// It filters out runtime and testing frames to keep the output clean.
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
