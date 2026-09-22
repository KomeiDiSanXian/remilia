// request.go — 请求消息副本的构造：附件保留策略与动态上下文挂载。
//
// 两者都只产出"本次请求"的消息副本，不写回会话历史——这是提示词前缀缓存
// 的基本前提：稳定前缀（System + 历史）逐轮字节一致，逐轮变化的内容排在最后。
package runtime

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// Retention 历史图片保留策略参数（来自 Config）。
type Retention struct {
	// MaxTurns ImageContextTurns：最近 N 条 user 消息。
	MaxTurns int
	// Window ImageContextWindow：时间窗（0 = 仅按条数）。
	Window time.Duration
	// MaxPerRequest MaxImagesPerRequest：单请求图片总数上限。
	MaxPerRequest int
}

// InjectDynamicContext 把动态上下文挂到消息序列中最后一条 user 消息上。
//
// 为什么挂在这里，而不是写进 System 消息：
//
//	LLM 侧的前缀缓存（DeepSeek 磁盘缓存、OpenAI/Anthropic prompt cache）按
//	请求前缀逐块比对。System 消息位于请求最前面，一旦它逐轮变化，其后的
//	全部历史消息都会变成缓存未命中。动态内容（运行时上下文/群聊窗口/长期
//	记忆/相关历史）每轮都不同，因此必须排在稳定前缀（System + 历史）之后。
//
// 为什么附着在最后一条 user 消息上，而不是追加一条新消息：
//
//	各提供商对消息序列的约束不同（Anthropic 只接受顶级 system 字段，
//	消息数组内要求 user/assistant 交替，且非首条 system 消息会被丢弃），
//	附着在既有 user 消息里对所有提供商都成立。
//
// 只作用于本次请求的消息副本，不写回会话历史：否则上一轮的运行时上下文
// （旧时间、旧群状态）会被持久化进历史，既污染后续请求的前缀，又让过期
// 信息长期留在上下文里。
//
// 无 user 消息或 dynamic 为空时原样返回。
func InjectDynamicContext(msgs []protocol.Message, dynamic string) []protocol.Message {
	if dynamic == "" {
		return msgs
	}
	lastUser := -1
	for i := range msgs {
		if msgs[i].Role == protocol.RoleUser {
			lastUser = i
		}
	}
	if lastUser < 0 {
		return msgs
	}

	block := "===== 动态上下文 =====\n" + dynamic

	if len(msgs[lastUser].ContentParts) == 0 {
		if msgs[lastUser].Content == "" {
			msgs[lastUser].Content = block
		} else {
			msgs[lastUser].Content = block + "\n\n" + msgs[lastUser].Content
		}
		return msgs
	}

	// 多模态消息：上下文作为首个 text part 前置（新建切片，不修改原消息的 parts）
	parts := make([]protocol.ContentPart, 0, len(msgs[lastUser].ContentParts)+1)
	parts = append(parts, protocol.ContentPart{Type: protocol.ContentPartText, Text: block})
	parts = append(parts, msgs[lastUser].ContentParts...)
	msgs[lastUser].ContentParts = parts
	return msgs
}

// PrepareRequestMessages 返回用于 LLM 请求的消息副本。
//
// 附件保留策略：
//   - 最后一条 user 消息（当前轮）的附件二进制始终保留；
//   - 历史 user 消息中的图片仅在"最近 MaxTurns 条内且时间窗内"时保留，
//     支持"继续追问图片细节"；更早/更老的图片降级为文本占位；
//   - 单次请求图片总数不超过 MaxPerRequest（从最近开始保留，超出降级）。
//
// 返回 (请求消息副本, 预算超限丢弃数, 时间/条数过期丢弃数)。
// 只有预算超限（max_images_per_request）需要提醒用户；过期丢弃是正常衰减。
func PrepareRequestMessages(msgs []protocol.Message, retention Retention) ([]protocol.Message, int, int) {
	lastUserIdx := -1
	for i := range msgs {
		if msgs[i].Role == protocol.RoleUser {
			lastUserIdx = i
		}
	}

	var refTime time.Time
	if lastUserIdx >= 0 {
		refTime = msgs[lastUserIdx].Timestamp
	}

	// user 消息序号（从最近的算起）：1 = 最后一条（当前轮，始终保留）。
	ordinal := make([]int, len(msgs))
	cnt := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == protocol.RoleUser {
			cnt++
			ordinal[i] = cnt
		}
	}

	out := make([]protocol.Message, len(msgs))
	imgBudget := retention.MaxPerRequest
	budgetDropped := 0
	staleDropped := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != protocol.RoleUser {
			out[i] = m
			continue
		}
		if ordinal[i] == 1 {
			// 当前轮：附件始终保留（图片计数计入请求预算）
			out[i] = m
			imgBudget -= CountImageParts(m.ContentParts)
			continue
		}

		// MaxTurns <= 0 表示仅保留当前轮（旧行为）
		withinCount := retention.MaxTurns > 0 && ordinal[i] <= retention.MaxTurns
		withinTime := retention.Window <= 0 ||
			refTime.IsZero() || m.Timestamp.IsZero() ||
			refTime.Sub(m.Timestamp) <= retention.Window
		if withinCount && withinTime {
			n := CountImageParts(m.ContentParts)
			if retention.MaxPerRequest <= 0 || imgBudget >= n {
				imgBudget -= n
				out[i] = m
				continue
			}
			budgetDropped += n
		} else {
			staleDropped += CountImageParts(m.ContentParts)
		}
		out[i] = StripBinaryParts(m)
	}
	return out, budgetDropped, staleDropped
}
