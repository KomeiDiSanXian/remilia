// pipeline.go — 动态上下文的节来源（Provider）与装配（Builder）。
//
// 动态上下文由若干"节"组成。每节只回答"我能提供什么上下文"：
// 这一节是否参与本轮、标题是什么、在给定注入上限下生成什么正文。
// 节序、预算编排与最终拼接属于 Builder 的职责，不在节内部决定。
//
// 节序即优先级：
//
//	运行时上下文 → 群聊最近消息 → 长期记忆 → 相关历史消息
//
// context_window > 0 时按预算编排：稳定系统提示词优先扣除，其余各节依次装入，
// 装不下则缩减或丢弃。context_window <= 0 时各节按配置上限（非预算路径）。
//
// 两条路径共用同一套参与条件：正文为空的节一律不输出，群聊最近消息受
// context_group_messages > 0 控制（关闭时两条路径都不纳入）。
//
// 各节的正文由调用方（装配侧）提供函数，因此本包不读取任何插件状态。
package promptctx

import (
	"strings"
	"unicode/utf8"
)

// ReserveTokens 预算中为输出 token 预留的余量。
const ReserveTokens = 512

// Source 动态上下文的一节来源（Provider）。
// 只描述"能提供什么"，不决定其他节是否存在、也不决定整体预算。
type Source struct {
	// Enabled 本节是否参与非预算路径；nil 表示恒参与。
	Enabled func() bool
	// Body 生成正文；limit 为调用方给出的上限（预算编排时会缩减）。
	Body func(limit int) string
	// Header 节标题（渲染为 "===== <header> ====="，不含换行）。
	Header string
	// Limit 本节注入上限的起步值（预算编排以此为起点缩减）。
	Limit int
	// Shrink 为 true 时预算路径按比例缩量适配剩余预算：先按上限构建一次，
	// 超预算则按"估算用量/预算"缩量重建一次，仍装不下才按 [truncateToTokenBudget]
	// 截断（详见 fitSection）。false 时单次估测，装不下即丢弃（运行时上下文语义）。
	Shrink bool
	// KeepTail 仅在 Shrink 截断兜底时生效：true 表示保留正文末尾（截掉开头），
	// 适用于按时间序排列、最新内容在末尾的节（如群聊最近消息，旧→新）。
	// 默认 false 保留开头（截掉结尾），适用于引导行/高分命中在前的节。
	KeepTail bool
}

// participates 判断本节是否参与本轮（两条路径共用同一条件）。
// Enabled 为 nil 表示恒参与；正文为空的节由装配侧跳过。
func (s Source) participates() bool {
	return s.Enabled == nil || s.Enabled()
}

// fit 在剩余预算内装入本节，装入成功时扣减 *remain。
// Shrink 为 true 时按比例缩量重试一次、必要时截断；否则单次估测，
// 正文为空或超预算都不装入。
func (s Source) fit(remain *int) string {
	if s.Shrink {
		return fitSection(remain, s.Limit, s.KeepTail, s.Body)
	}
	body := s.Body(s.Limit)
	if body == "" {
		return ""
	}
	if est := EstimateTokens(body); est <= *remain {
		*remain -= est
		return body
	}
	return ""
}

// render 渲染为注入文本：标题 + 换行 + 正文。
func (s Source) render(body string) string {
	return "===== " + s.Header + " =====\n" + body
}

// EstimateTokens 粗略估算文本 token 数（CJK 3 字节/字 ≈ 1 token，
// ASCII 混合下按字节/3 近似，够用于预算编排即可）。
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return len(s)/3 + 1
}

// fitSection 在预算内构建一节：按上限构建一次，超预算则按比例缩量重建一次，
// 仍装不下才截断到剩余额度。
//
// 先前的"沿计数减半反复重建"会让每次缩减都重跑一遍检索/嵌入（记忆与历史
// 检索的正文生成并不廉价），且当正文大小与条数无关时（如 RAG 会话缓存命中
// 直接返回整段文本）永远缩不下去，最终整节被丢弃。改为：
//  1. build(max)：一次检索/嵌入，覆盖绝大多数够预算的场景；
//  2. 按 est/预算 比例缩量重建一次：保留"按条数裁剪"的语义
//     （时间序节留最新、高分节留最优），重复构建最多两次；
//  3. 截断兜底：正文与条数无关（缓存）或极度超预算时，截到预算内而非整节丢弃。
//
// keepTail 仅在步骤 3 生效，见 [Source.KeepTail]。
// 返回构建文本（空串 = 未装入），并扣减剩余预算。
func fitSection(remain *int, max int, keepTail bool, build func(n int) string) string {
	text := build(max)
	if text == "" {
		return ""
	}
	est := EstimateTokens(text)
	if est <= *remain {
		*remain -= est
		return text
	}

	if max > 1 {
		if scaled := max * (*remain) / est; scaled > 0 && scaled < max {
			if smaller := build(scaled); smaller != "" {
				if est2 := EstimateTokens(smaller); est2 <= *remain {
					*remain -= est2
					return smaller
				}
			}
		}
	}

	trimmed := truncateToTokenBudget(text, *remain, keepTail)
	if trimmed == "" {
		return ""
	}
	*remain -= EstimateTokens(trimmed)
	return trimmed
}

// truncateToTokenBudget 把文本截到 token 预算内（口径同 [EstimateTokens]），
// 截断点落在 UTF-8 字符边界上，避免截出半个多字节字符。
//
// keepTail 为 true 时保留末尾、截掉开头，并丢弃开头被截断的半行（跳到第一个
// 换行之后），使注入内容从整行开始；false 时保留开头、截掉结尾。
// 返回空串表示预算不足以放下任何完整内容。
func truncateToTokenBudget(text string, budget int, keepTail bool) string {
	if budget <= 0 {
		return ""
	}
	// EstimateTokens(s) = len(s)/3 + 1 ≤ budget ⇔ len(s) ≤ (budget-1)*3。
	maxBytes := (budget - 1) * 3
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if !keepTail {
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		return strings.TrimRight(text[:cut], "\n")
	}
	start := len(text) - maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	if start > 0 {
		if nl := strings.IndexByte(text[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
	}
	return strings.TrimLeft(text[start:], "\n")
}

// Build 非预算路径：各节按节序装入，正文为空的节跳过。
// 返回空串表示当前无动态上下文。
func Build(sources []Source) string {
	var parts []string
	for _, s := range sources {
		if !s.participates() {
			continue
		}
		body := s.Body(s.Limit)
		if body == "" {
			continue
		}
		parts = append(parts, s.render(body))
	}
	return strings.Join(parts, "\n\n")
}

// BuildBudgeted 按窗口预算编排动态上下文：
// reserve 从窗口中预留后，先扣除 staticSystemPrompt 的估算用量，
// 剩余额度按节序依次装入，装不下则缩减或丢弃。
//
// 本函数是预算路径的权威实现：window > 0 时调用方必须直接采用其返回值，
// 返回空串只表示"本轮无动态上下文可装下"，不得据此回退到 [Build] 的
// 非预算路径——那会恰好在预算最紧时突破窗口。装配入口请用 [BuildWindowed]。
func BuildBudgeted(sources []Source, window int, staticSystemPrompt string) string {
	if window <= 0 {
		return ""
	}

	remain := window - ReserveTokens
	if remain <= 0 {
		remain = window / 2
	}

	// 稳定系统提示词（框架 + 自定义指令）始终作为 System 消息发送，
	// 先从这里扣除它的预算，剩余额度才归动态各节分配。
	remain -= EstimateTokens(staticSystemPrompt)
	if remain <= 0 {
		return ""
	}

	var parts []string
	for _, s := range sources {
		if !s.participates() {
			continue
		}
		body := s.fit(&remain)
		if body == "" {
			continue
		}
		parts = append(parts, s.render(body))
	}
	return strings.Join(parts, "\n\n")
}

// BuildWindowed 是动态上下文装配的唯一入口：按 context_window 选择路径。
//
//	window > 0  → 预算路径（权威）：稳定系统提示词先扣除，剩余额度装不下的
//	              节一律缩减或丢弃，绝不回退到非预算路径；
//	window <= 0 → 非预算路径：各节按配置上限装配。
//
// 两条路径共用同一套参与条件（见 Source.participates），因此返回值只表达
// "本轮装下的动态上下文"，空串是合法结果（无动态内容或预算装不下）。
func BuildWindowed(sources []Source, window int, staticSystemPrompt string) string {
	if window > 0 {
		return BuildBudgeted(sources, window, staticSystemPrompt)
	}
	return Build(sources)
}
