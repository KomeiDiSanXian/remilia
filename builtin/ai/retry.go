// Package ai retry.go — 工具执行失败的重试预算与反思引导。
//
// 本文件实现工具循环中的失败处理策略：
//   - isToolErrorResult：识别 executeTool 返回的错误结果文本
//   - buildReflectionMessage：第 2 次连续失败起注入的"反思指令"消息，
//     要求模型分析失败原因并采用不同策略（显式反思轮）
//   - buildRetryAbortMessage：重试预算耗尽时的优雅中止回复，
//     替代原先撞 max_depth 的裸错误
//
// 重试预算语义（tool_retry_limit，默认 2）：
//
//	同一工具连续失败 → 每次失败结果正常回填（模型天然可重试）；
//	从第 2 次失败起额外注入反思指令；连续失败达到 limit+1 次时中止本轮。
//	工具执行成功会清零该工具的连续失败计数。
package ai

import (
	"fmt"
	"strings"
)

// isToolErrorResult 判断工具执行结果是否为失败。
// executeTool 的错误结果统一以 "错误" 前缀开头（见 execute.go / executeSkillTool）。
func isToolErrorResult(result string) bool {
	return strings.HasPrefix(result, "错误:") || strings.HasPrefix(result, "错误：")
}

// buildReflectionMessage 构建显式反思轮的用户消息。
// 追加到工具失败结果之后，引导模型在下一轮调用前先分析失败原因。
func buildReflectionMessage(toolName string, fails int, lastErr string) Message {
	return Message{
		Role: RoleUser,
		Content: fmt.Sprintf(
			"反思提示：工具 `%s` 已连续失败 %d 次，最后一次错误：%s\n"+
				"请先分析失败原因（参数不对？结果格式问题？工具用错了？），"+
				"再采用与上次不同的策略重试；如果确实无法完成，请直接告知用户无法完成并说明原因，"+
				"不要重复相同的失败操作。",
			toolName, fails, lastErr),
	}
}

// buildRetryAbortMessage 构建重试预算耗尽时的优雅中止回复。
func buildRetryAbortMessage(toolName string, fails int, lastErr string) string {
	return fmt.Sprintf(
		"抱歉，工具 `%s` 连续执行失败 %d 次，已停止尝试。最后错误：%s",
		toolName, fails, lastErr)
}
