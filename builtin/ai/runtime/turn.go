// turn.go — 回合内工具调用的并行编排与调用追踪。
//
// 两者都不依赖插件状态：并行编排只关心调用列表、并发度与中断信号，追踪只往
// 会话里追加一条记录。真正"执行单个工具"的策略与装配由调用方以闭包注入，
// 因此这里只做调度，不引入新的执行契约。
package runtime

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
)

// ToolExecResult 单个工具调用的执行结果（回合编排按原始顺序回填）。
type ToolExecResult struct {
	// Result 工具执行结果文本。
	Result string
	// Err 非空表示本次调用本身失败（重试预算与反思引导的依据）。
	// 只看类型化错误，不再用结果文本前缀反推。
	Err error
	// Skipped 中断抢占后未启动的工具调用（不入历史、不计数）。
	Skipped bool
}

// ExecuteToolCallsParallel 以 parallel 并发度执行一轮全部工具调用。
//
// 结果按原始顺序返回；interrupted 在派发前检查，为真则该调用直接标记为
// Skipped（未启动）。parallel <= 1 或只有一个调用时退化为顺序执行
// （行为与旧版一致；审批/执行/追踪相互独立）。exec 由调用方提供，负责单个
// 调用的策略评估与执行。
func ExecuteToolCallsParallel(
	calls []protocol.ToolCall,
	parallel int,
	interrupted func() bool,
	exec func(i int, tc protocol.ToolCall) ToolExecResult,
) []ToolExecResult {
	results := make([]ToolExecResult, len(calls))
	if parallel <= 1 || len(calls) <= 1 {
		for i := range calls {
			results[i] = exec(i, calls[i])
		}
		return results
	}
	if parallel > len(calls) {
		parallel = len(calls)
	}

	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i := range calls {
		if interrupted() {
			// 抢占信号到达：未启动的工具调用直接跳过。
			results[i] = ToolExecResult{Skipped: true}
			continue
		}
		wg.Add(1)
		go func(i int, tc protocol.ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = exec(i, tc)
		}(i, calls[i])
	}
	wg.Wait()
	return results
}

// RecordToolTrace 记录一次工具调用的追踪信息（耗时、参数摘要、失败标记）。
// failed 由调用方按类型化错误给出，不再从结果文本前缀推断。
func RecordToolTrace(sess *session.Session, tc protocol.ToolCall, start time.Time, result string, err error) {
	entry := session.ToolTraceEntry{
		Time:     start,
		ToolName: tc.Name,
		Args:     textutil.TruncateRunes(SummarizeArgs(tc.Arguments), 80),
		Duration: time.Since(start),
	}
	if err != nil {
		entry.Err = textutil.TruncateRunes(result, 120)
	}
	sess.AppendToolTrace(entry)
}
