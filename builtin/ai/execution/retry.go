// retry.go — 调用失败时回填给模型的提示文案。
//
// 工具循环按"同一工具连续失败次数"决定何时追加这两段文案：
//   - BuildReflectionMessage：第 2 次连续失败起注入的显式反思指令，
//     要求模型分析失败原因并采用不同策略（用户消息）
//   - BuildRetryAbortMessage：重试预算耗尽时的优雅中止回复，
//     替代原先撞 max_depth 的裸错误
//
// 预算本身（tool_retry_limit，默认 2）由调用方维护：工具执行成功清零计数，
// 连续失败达到 limit+1 次时中止本轮。失败与否由 ActionResult.Err 类型化判定，
// 不用结果文本前缀反推——工具正文以"错误:"开头不代表调用失败。

package execution

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// BuildReflectionMessage 构建显式反思轮的用户消息。
// 追加到工具失败结果之后，引导模型在下一轮调用前先分析失败原因。
func BuildReflectionMessage(toolName string, fails int, lastErr string) protocol.Message {
	return protocol.Message{
		Role: protocol.RoleUser,
		Content: fmt.Sprintf(
			"反思提示：工具 `%s` 已连续失败 %d 次，最后一次错误：%s\n"+
				"请先分析失败原因（参数不对？结果格式问题？工具用错了？），"+
				"再采用与上次不同的策略重试；如果确实无法完成，请直接告知用户无法完成并说明原因，"+
				"不要重复相同的失败操作。",
			toolName, fails, lastErr),
	}
}

// BuildRetryAbortMessage 构建重试预算耗尽时的优雅中止回复。
func BuildRetryAbortMessage(toolName string, fails int, lastErr string) string {
	return fmt.Sprintf(
		"抱歉，工具 `%s` 连续执行失败 %d 次，已停止尝试。最后错误：%s",
		toolName, fails, lastErr)
}
