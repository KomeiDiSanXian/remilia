// trace.go — 会话级工具调用追踪与连续失败计数。
package session

import (
	"time"
)

// ToolTraceEntry 一次工具调用的追踪记录（调用链可观测性）。
type ToolTraceEntry struct {
	// Time 调用开始时间。
	Time time.Time `json:"time"`
	// ToolName 工具名。
	ToolName string `json:"tool_name"`
	// Args 参数摘要（截断，防敏感信息泄漏）。
	Args string `json:"args,omitempty"`
	// Duration 执行耗时。
	Duration time.Duration `json:"duration"`
	// Err 非空表示执行失败（错误文本摘要）。
	Err string `json:"err,omitempty"`
}

// MaxToolTrace 每个会话保留的最大工具调用追踪条数（超出淘汰最旧）。
const MaxToolTrace = 50

// AppendToolTrace 追加一条工具调用追踪（线程安全，超出上限淘汰最旧）。
func (s *Session) AppendToolTrace(e ToolTraceEntry) {
	s.Lock()
	defer s.Unlock()
	s.trace = append(s.trace, e)
	if len(s.trace) > MaxToolTrace {
		s.trace = s.trace[len(s.trace)-MaxToolTrace:]
	}
}

// ToolTrace 返回会话的工具调用追踪副本（从旧到新）。
func (s *Session) ToolTrace() []ToolTraceEntry {
	s.Lock()
	defer s.Unlock()
	out := make([]ToolTraceEntry, len(s.trace))
	copy(out, s.trace)
	return out
}

// IncrToolFailure 递增指定工具的连续失败次数并返回新值。
// 线程安全。
func (s *Session) IncrToolFailure(name string) int {
	s.Lock()
	defer s.Unlock()
	if s.toolFailures == nil {
		s.toolFailures = make(map[string]int)
	}
	s.toolFailures[name]++
	return s.toolFailures[name]
}

// ResetToolFailure 清除指定工具的连续失败计数（执行成功时调用）。
// 线程安全。
func (s *Session) ResetToolFailure(name string) {
	s.Lock()
	defer s.Unlock()
	if s.toolFailures != nil {
		delete(s.toolFailures, name)
	}
}
