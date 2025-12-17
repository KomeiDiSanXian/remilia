package remilia

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
)

// HandlerError 标准化错误结构
// Message: 错误消息
// Source: 触发来源（plugin:<name> 或 global）
// Attempt: 当前重试次数
// Trace: 中间件链名称序列（需启用 SetTrace）
// EventID: 事件ID
// Stack: 错误堆栈信息（可选，由 REMILIA_STACK_TRACE 环境变量控制）
type HandlerError struct {
	Message string   `json:"message"`
	Source  string   `json:"source"`
	Attempt int      `json:"attempt"`
	Trace   []string `json:"trace,omitempty"`
	EventID string   `json:"event_id,omitempty"`
	Stack   string   `json:"stack,omitempty"`
}

func (he HandlerError) Error() string {
	return he.Message
}

// BlockError 表示处理被中间件阻断的错误
type BlockError struct {
	Message string
}

func (be BlockError) Error() string {
	return be.Message
}

// NewBlockError 创建一个阻断错误
func NewBlockError(message string) error {
	return BlockError{Message: message}
}

// IsBlockError 检查错误是否为阻断错误
func IsBlockError(err error) bool {
	var be BlockError
	return errors.As(err, &be)
}

// WrapError 构造标准化错误
//
// 如果设置了 REMILIA_STACK_TRACE=true 环境变量，会自动捕获堆栈信息。
// 堆栈捕获会带来一定的性能开销，生产环境请谨慎启用。
//
// 使用示例：
//
//	// 启用堆栈跟踪
//	os.Setenv("REMILIA_STACK_TRACE", "true")
//
//	// 或在启动时设置
//	REMILIA_STACK_TRACE=true ./my_bot
func WrapError(err error, ctx *Context, m *Matcher, attempt int) error {
	if err == nil {
		return nil
	}
	var trace []string
	if v, ok := ctx.GetState("mw_trace"); ok {
		if arr, ok := v.([]string); ok {
			trace = arr
		}
	}
	var eventID string
	if ctx.event != nil {
		eventID = string(ctx.event.ID)
	}

	herr := HandlerError{
		Message: err.Error(),
		Source:  m.Source,
		Attempt: attempt,
		Trace:   trace,
		EventID: eventID,
	}

	// 如果启用堆栈跟踪，捕获堆栈信息
	if shouldCaptureStack() {
		herr.Stack = captureStack()
	}

	return herr
}

// MarshalDeadLetterItem JSON 序列化 DeadLetterItem（携带标准化错误）
func MarshalDeadLetterItem(item DeadLetterItem) ([]byte, error) {
	var herr HandlerError
	var he HandlerError
	if errors.As(item.Err, &he) {
		herr = he
	}

	return json.Marshal(struct {
		Event *DeadLetterEvent `json:"event"`
		Error HandlerError     `json:"error"`
	}{
		Event: &DeadLetterEvent{ID: string(item.Event.ID), Type: string(item.Event.Type)},
		Error: herr,
	})
}

// DeadLetterEvent 用于文件落地的轻量事件表示
type DeadLetterEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// NewPanicError 快速构造 panic 错误
func NewPanicError(r interface{}) error {
	return fmt.Errorf("panic: %v", r)
}

// --- 堆栈跟踪相关函数 ---

var (
	stackTraceEnabled     bool
	stackTraceEnabledOnce sync.Once
)

// shouldCaptureStack 检查是否应该捕获堆栈
//
// 通过 REMILIA_STACK_TRACE 环境变量或 EnableStackTrace() 控制。
// 默认禁用以避免性能开销。
func shouldCaptureStack() bool {
	// 如果已通过 EnableStackTrace() 设置，直接返回
	if stackTraceEnabled {
		return true
	}

	// 否则检查环境变量（只检查一次）
	stackTraceEnabledOnce.Do(func() {
		if os.Getenv("REMILIA_STACK_TRACE") == "true" {
			stackTraceEnabled = true
		}
	})
	return stackTraceEnabled
}

// EnableStackTrace 手动启用堆栈跟踪
//
// 使用示例：
//
//	remilia.EnableStackTrace(true)
func EnableStackTrace(enabled bool) {
	stackTraceEnabled = enabled
}

// IsStackTraceEnabled 检查堆栈跟踪是否已启用
func IsStackTraceEnabled() bool {
	return stackTraceEnabled
}

// captureStack 捕获当前堆栈信息
//
// 返回格式化的堆栈字符串，包含文件名、行号和函数名。
// 自动过滤 runtime 和 testing 内部代码，保留用户代码。
func captureStack() string {
	const maxStackDepth = 32
	var pcs [maxStackDepth]uintptr
	n := runtime.Callers(2, pcs[:]) // 跳过 captureStack 和 WrapError

	frames := runtime.CallersFrames(pcs[:n])
	var lines []string

	for {
		frame, more := frames.Next()

		// 过滤规则：
		// 1. 排除 Go runtime 内部代码
		// 2. 排除 testing 框架代码
		// 3. 保留用户代码和测试代码
		shouldInclude := true

		if strings.Contains(frame.File, "/runtime/") ||
			strings.Contains(frame.File, "\\runtime\\") {
			shouldInclude = false
		} else if strings.Contains(frame.File, "/testing/testing.go") ||
			strings.Contains(frame.File, "\\testing\\testing.go") {
			shouldInclude = false
		}

		if shouldInclude {
			line := fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function)
			lines = append(lines, line)
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

// FormatHandlerError 格式化 HandlerError 用于日志输出
//
// 包含完整的错误信息和堆栈（如果有）。
func FormatHandlerError(err error) string {
	var he HandlerError
	if !errors.As(err, &he) {
		return err.Error()
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Message: %s", he.Message))
	parts = append(parts, fmt.Sprintf("Source: %s", he.Source))
	parts = append(parts, fmt.Sprintf("Attempt: %d", he.Attempt))

	if he.EventID != "" {
		parts = append(parts, fmt.Sprintf("EventID: %s", he.EventID))
	}

	if len(he.Trace) > 0 {
		parts = append(parts, fmt.Sprintf("Trace: %v", he.Trace))
	}

	if he.Stack != "" {
		parts = append(parts, "Stack:\n"+he.Stack)
	}

	return strings.Join(parts, "\n")
}
