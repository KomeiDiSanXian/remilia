// envcontext.go — 环境事实消息的编码与放置。
//
// 会话历史里，环境事实以 provider 中立的 [protocol.RoleEnvContext] 存在，
// 位置固定在"它所属的那条 user 消息之前"。把它翻译成某个端点认识的线格式后，
// 位置约束各家不同，官方文档的要求是硬性的（违规直接 400）：
//
//	OpenAI developer           无位置约束                    → 保持在 user 之前
//	Anthropic 中途 system      必须跟在 user（或以 tool_result 结尾的
//	                           assistant）之后、且在 assistant 之前或数组末尾
//	                           → 必须挪到 user 消息**之后**
//
// 最后一条是本文件存在的理由：如果照搬"env 在 user 之前"的形状，Anthropic
// 会在第一个 user 消息处直接 400（system 后面跟 user 是不允许的组合）。
//
// 各形状统一由 [EncodeEnvContext] 处理，因此放置规则只有一处实现，
// 适配器只负责序列化拿到的消息。

package runtime

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// EncodeEnvContext 把消息序列里的 [protocol.RoleEnvContext] 翻译成目标端点
// 的原生形状，并按该形状的位置约束重新排列。
//
// 返回值是新切片，不修改入参。四种情况的语义：
//
//   - Developer：就地替换角色（env 本来就在 user 之前）。
//   - MidSystem：与紧随其后的 user 消息交换位置，并把角色换成 RoleSystemMid
//     （Anthropic 适配器再映射成数组内的 "system"）。
//   - None：删除该消息，并把正文合并进紧随其后的 user 消息——这就是
//     "回退到把事实拼进 user 文本"的兜底形态，对所有端点都成立。
//
// 原生编码失败（载荷无法渲染成该形状的正文）同样折进 user 文本，而不是丢掉
// 事实：400 的代价远高于"这一轮用散文表达"。
//
// 没有 user 消息跟随的 env（历史被裁剪过的异常残留）一律丢弃：留下它既违反
// 位置约束，又没有任何 user 轮次需要这份事实。
func EncodeEnvContext(msgs []protocol.Message, shape protocol.NativeContext) []protocol.Message {
	if !hasEnvContext(msgs) {
		return msgs
	}

	out := make([]protocol.Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role != protocol.RoleEnvContext {
			out = append(out, m)
			continue
		}

		// 找紧随其后的 user 消息（正常情况就是下一条）。
		next := i + 1
		if next >= len(msgs) || msgs[next].Role != protocol.RoleUser {
			continue // 异常残留：丢弃
		}

		switch shape {
		case protocol.NativeContextDeveloper:
			if enc, ok := protocol.EncodeEnvContext(shape, m.Content); ok {
				out = append(out, enc)
				// user 消息留给下一轮循环处理。
				continue
			}
			// 该载荷无法渲染成原生形状：折进 user 文本。
			user := msgs[next]
			user = prependUserText(user, envBlock(m.Content))
			out = append(out, user)
			i = next
		case protocol.NativeContextMidSystem:
			// 先放 user，再放 system：Anthropic 要求 system 跟在 user 之后。
			enc, ok := protocol.EncodeEnvContext(shape, m.Content)
			if !ok {
				user := msgs[next]
				user = prependUserText(user, envBlock(m.Content))
				out = append(out, user)
			} else {
				out = append(out, msgs[next], enc)
			}
			i = next // user 已经输出
		default:
			// 兜底：把事实折进 user 文本，形状与改造前的动态上下文一致。
			user := msgs[next]
			user = prependUserText(user, envBlock(m.Content))
			out = append(out, user)
			i = next
		}
	}
	return out
}

// envBlock 给环境事实加上与运行时上下文一致的节标题，供兜底形态使用。
func envBlock(content string) string {
	body := protocol.EnvContextProse(content)
	if body == "" {
		return ""
	}
	return "===== 运行时上下文 =====\n" + body
}

// prependUserText 把文本前置到 user 消息（多模态时作为首个 text part）。
//
// 这与 [InjectDynamicContext] 处理多模态的分支保持同一套语义：新建切片、
// 不修改原消息的 parts。
func prependUserText(m protocol.Message, text string) protocol.Message {
	if text == "" {
		return m
	}
	if len(m.ContentParts) == 0 {
		if m.Content == "" {
			m.Content = text
		} else {
			m.Content = text + "\n\n" + m.Content
		}
		return m
	}
	parts := make([]protocol.ContentPart, 0, len(m.ContentParts)+1)
	parts = append(parts, protocol.ContentPart{Type: protocol.ContentPartText, Text: text})
	parts = append(parts, m.ContentParts...)
	m.ContentParts = parts
	return m
}

// hasEnvContext 判断序列里是否存在环境事实消息（避免无谓的切片分配）。
func hasEnvContext(msgs []protocol.Message) bool {
	for _, m := range msgs {
		if m.Role == protocol.RoleEnvContext {
			return true
		}
	}
	return false
}

// EnvContextMessage 构造一条环境事实消息（写入会话历史时使用）。
//
// 位置约定：调用方必须把它插在**所属那条 user 消息之前**。这个顺序是
// provider 中立的规范形态，编码时才按端点调整（见 [EncodeEnvContext]）。
func EnvContextMessage(content string) protocol.Message {
	return protocol.Message{Role: protocol.RoleEnvContext, Content: content}
}

// DropEnvContext 删除序列里的全部环境事实消息（返回新切片）。
//
// 用于"完全不注入运行时上下文"（include_runtime_context=false）：
// [EncodeEnvContext] 在 None 形状下会把这些记录折进 user 文本，对隐私开关而言
// 那依然是一次注入，因此必须在编码之前整条丢弃。
func DropEnvContext(msgs []protocol.Message) []protocol.Message {
	if !hasEnvContext(msgs) {
		return msgs
	}
	out := make([]protocol.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == protocol.RoleEnvContext {
			continue
		}
		out = append(out, m)
	}
	return out
}
