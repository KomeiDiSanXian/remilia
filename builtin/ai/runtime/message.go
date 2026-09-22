// message.go — 消息与载荷的纯形状工具。
//
// 这些函数只读写协议消息本身（文本提取、多模态占位、工具调用序列修复、
// 参数摘要、错误文案），不依赖插件状态，因此从回合编排里独立出来。
package runtime

import (
	"fmt"
	"slices"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ToolResultMissing 工具结果缺失时的占位回填文本（用于修复消息序列）。
const ToolResultMissing = "（工具结果缺失，消息序列已修复）"

// MaxToolResultLen 单条工具结果回填给 LLM 的最大字符数。
// 防止一条命令输出巨型结果撑爆上下文窗口。
const MaxToolResultLen = 8000

// MergeChatAttachments 合并工具捕获附件与模型直接输出的附件（保持顺序）。
func MergeChatAttachments(groups ...[]platform.Attachment) []platform.Attachment {
	var out []platform.Attachment
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// LastUserIsReplan 判断会话最后一条用户消息是否已是重规划指令（防重复追加）。
func LastUserIsReplan(sess *session.Session) bool {
	msgs := sess.SnapshotMessages()
	for _, msg := range slices.Backward(msgs) {
		if msg.Role == protocol.RoleUser {
			return strings.HasPrefix(msg.Content, "计划步骤")
		}
	}
	return false
}

// SummarizeArgs 生成工具参数摘要（按键排序、截断，用于审批展示，避免敏感信息泄漏）。
func SummarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k == "arguments" {
			continue // 真实命令的原始参数串单独展示
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s := fmt.Sprintf("%v", args[k])
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return strings.Join(parts, ", ")
}

// LastUserMessage 从 session 中提取最后一条用户消息的文本内容。
// 多模态消息（ContentParts 模式）时从 text part 提取，保证工具选择/RAG/
// 记忆查询拿到的是文字而非空串或媒体占位符。
func LastUserMessage(sess *session.Session) string {
	sess.Lock()
	defer sess.Unlock()
	for _, v := range slices.Backward(sess.Messages) {
		if v.Role != protocol.RoleUser {
			continue
		}
		if text := MessageText(v); text != "" {
			return text
		}
		return ""
	}
	return ""
}

// MessageText 提取消息的文本内容：优先 Content，其次 ContentParts 中的 text part。
func MessageText(m protocol.Message) string {
	if m.Content != "" {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.ContentParts {
		if p.Type == protocol.ContentPartText && p.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// CountImageParts 统计消息中图片 ContentPart 的数量。
func CountImageParts(parts []protocol.ContentPart) int {
	n := 0
	for _, p := range parts {
		if p.Type == protocol.ContentPartImage {
			n++
		}
	}
	return n
}

// RepairToolCallSequence 修复消息序列中的工具调用完整性：
// 每条含 tool_calls 的 assistant 消息之后，必须为其中每个 tool_call_id
// 提供对应的 tool 消息（OpenAI/Anthropic API 硬性约束，缺失即 400）。
//
// 中断抢占跳过的工具、进程异常退出、持久化损坏或上下文裁剪
// 都可能导致序列残缺，这里双向自愈：
//   - 缺失的 tool 响应：插入占位 tool 消息（tool 消息必须紧跟在
//     assistant 之后，因此在遇到下一条非 tool 消息或序列末尾时补位）
//   - 孤儿的 tool 消息：前置 assistant(tool_calls) 被裁掉后残留的
//     tool 消息没有可响应的 tool_call_id，API 同样会以 400 拒绝，直接丢弃
//
// 返回新的消息切片，不修改原切片。
func RepairToolCallSequence(msgs []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, 0, len(msgs)+4)
	var pending []string // 尚未响应的 tool_call_id
	for _, m := range msgs {
		if m.Role == protocol.RoleTool {
			// 只保留响应当前 assistant(tool_calls) 组的 tool 消息；
			// 找不到对应 tool_call_id 即为孤儿消息（如上下文裁剪切掉了
			// 前置 assistant），丢弃以免整个请求被 API 以 400 拒绝。
			idx := -1
			for i, id := range pending {
				if id == m.ToolCallID {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			pending = append(pending[:idx], pending[idx+1:]...)
			out = append(out, m)
			continue
		}

		if len(pending) > 0 {
			// 非 tool 消息出现：先把前面 assistant 缺失的工具响应补位插回
			// （插在本消息之前，确保 tool 消息紧随 assistant）。
			for _, id := range pending {
				out = append(out, protocol.Message{Role: protocol.RoleTool, Content: ToolResultMissing, ToolCallID: id})
			}
			pending = nil
		}
		out = append(out, m)

		if m.Role == protocol.RoleAssistant && len(m.ToolCalls) > 0 {
			pending = nil
			seen := make(map[string]bool, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || seen[tc.ID] {
					continue
				}
				seen[tc.ID] = true
				pending = append(pending, tc.ID)
			}
		}
	}
	// 序列末尾仍未响应的 tool_call_id 补位
	for _, id := range pending {
		out = append(out, protocol.Message{Role: protocol.RoleTool, Content: ToolResultMissing, ToolCallID: id})
	}
	return out
}

// StripBinaryParts 将消息中的多模态附件二进制内容替换为文本占位。
// 无 ContentParts 时原样返回。
func StripBinaryParts(m protocol.Message) protocol.Message {
	if len(m.ContentParts) == 0 {
		return m
	}
	parts := make([]string, 0, len(m.ContentParts))
	for _, p := range m.ContentParts {
		switch p.Type {
		case protocol.ContentPartText:
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		case protocol.ContentPartImage:
			parts = append(parts, "[历史图片未随本次请求发送]")
		case protocol.ContentPartAudio:
			parts = append(parts, "[历史音频未随本次请求发送]")
		}
	}
	m.ContentParts = nil
	m.Content = strings.Join(parts, "\n")
	return m
}

// TruncateToolResult 截断过长的工具结果，按 rune 截断避免劈开多字节字符。
func TruncateToolResult(result string) string {
	runes := []rune(result)
	if len(runes) <= MaxToolResultLen {
		return result
	}
	return string(runes[:MaxToolResultLen]) + "\n…(工具结果过长已截断)"
}

// FormatAIError 将 provider 返回的错误转换为用户友好的提示。
//
// 常见错误映射：
//   - 401: API Key 无效或未配置
//   - 404: 模型名称错误或 API 地址不对
//   - 429: 速率限制
//   - timeout / context deadline: 请求超时（可检查网络或增大 timeout）
//     其他: 记录详细日志后返回通用提示，避免向用户暴露可能包含
//     组织/计费信息的原始 API 错误体。
func FormatAIError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return "API 认证失败，请检查 api_key 配置"
	case strings.Contains(msg, "404"):
		return "API 地址或模型名称错误，请检查 base_url 和 model 配置"
	case strings.Contains(msg, "429"):
		return "请求过于频繁，请稍后再试"
	case strings.Contains(msg, "context deadline exceeded"):
		return "请求超时，请检查网络连接或增大超时配置"
	case strings.Contains(msg, "connection refused"):
		return "无法连接 API 服务器，请检查 base_url 配置"
	case strings.Contains(msg, "no such host"):
		return "API 域名解析失败，请检查 base_url 配置"
	default:
		logger.Warnf("[AI] Unhandled LLM error: %v", err)
		return "AI 处理出错，请稍后再试"
	}
}
