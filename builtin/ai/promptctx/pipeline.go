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
//
// 从"字符串拼接"到"条目集合"：本文件的所有 Builder 内部都产出 [Item]
// （见 item.go），最终由 [Render] 拼成文本。这样预算编排、通道划分与
// 渲染彼此独立——预算变化不会顺手改掉发出去的字节，通道划分也不需要
// 在渲染层再做一次文本匹配。

package promptctx

import (
	"strings"
	"unicode/utf8"
)

// ReserveTokens 预算中为输出 token 预留的余量。
const ReserveTokens = 512

// Source 动态上下文的一节来源（Provider）。
// 只描述"能提供什么"，不决定其他节是否存在、也不决定整体预算。
//
// Source 产出的条目一律是 [Ephemeral]：群聊窗口、长期记忆、相关历史都可能
// 来自任意成员或被模型抽取，回放它们等于把不可信内容钉成历史指令。能产
// [Persistent] 条目的只有 [EnvironmentFacts]。
type Source struct {
	// Enabled 本节是否参与；nil 表示恒参与。
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

// collect 在剩余预算内装入本节，返回条目（未装入时返回 nil）。
// 装入成功时扣减 *remain。
func (s Source) collect(remain *int) []Item {
	body, ok := s.fit(remain)
	if !ok {
		return nil
	}
	return []Item{{section: s.Header, channel: Ephemeral, body: body}}
}

// fit 在剩余预算内装入本节，返回正文与是否装入。
func (s Source) fit(remain *int) (string, bool) {
	if s.Shrink {
		text := fitSection(remain, s.Limit, s.KeepTail, s.Body)
		return text, text != ""
	}
	body := s.Body(s.Limit)
	if body == "" {
		return "", false
	}
	if est := EstimateTokens(body); est <= *remain {
		*remain -= est
		return body, true
	}
	return "", false
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

// Collect 非预算路径：各节按节序装入，正文为空的节跳过。
// persistent 为已确定可以随历史回放的条目（顺序在全部节之前）。
func Collect(sources []Source, persistent []Item) Snapshot {
	items := make([]Item, 0, len(persistent)+len(sources))
	items = append(items, persistent...)
	for _, s := range sources {
		if !s.participates() {
			continue
		}
		body := s.Body(s.Limit)
		if body == "" {
			continue
		}
		items = append(items, Item{section: s.Header, channel: Ephemeral, body: body})
	}
	return newSnapshot(items)
}

// CollectBudgeted 按窗口预算编排动态上下文：
// reserve 从窗口中预留后，先扣除 staticSystemPrompt 与 persistent 条目的
// 估算用量，剩余额度按节序依次装入，装不下则缩减或丢弃。
//
// 本函数是预算路径的权威实现：window > 0 时调用方必须直接采用其返回值，
// 返回空快照只表示"本轮无可用上下文"，不得据此回退到 [Collect] 的
// 非预算路径——那会恰好在预算最紧时突破窗口。装配入口请用 [BuildWindowedSnapshot]。
func CollectBudgeted(sources []Source, persistent []Item, window int, staticSystemPrompt string) Snapshot {
	if window <= 0 {
		return Snapshot{}
	}

	remain := window - ReserveTokens
	if remain <= 0 {
		remain = window / 2
	}

	// 稳定系统提示词（框架 + 自定义指令）始终作为 System 消息发送，
	// 先从这里扣除它的预算，剩余额度才归动态各节分配。
	remain -= EstimateTokens(staticSystemPrompt)

	// Persistent 条目同样占据窗口：它们会随请求发出（而且逐轮存在于历史中），
	// 必须与各节共享同一份预算，否则"加上环境事实"会悄悄突破 context_window。
	items := make([]Item, 0, len(persistent)+len(sources))
	for _, it := range persistent {
		est := EstimateTokens(it.body)
		if remain < est {
			break
		}
		remain -= est
		items = append(items, it)
	}
	if remain <= 0 {
		return newSnapshot(items)
	}

	for _, s := range sources {
		if !s.participates() {
			continue
		}
		items = append(items, s.collect(&remain)...)
	}
	return newSnapshot(items)
}

// BuildWindowedSnapshot 是动态上下文装配的唯一入口（条目形态）：按
// context_window 选择路径。
//
//	window > 0  → 预算路径（权威）：稳定系统提示词与 persistent 条目先扣除，
//	              剩余额度装不下的节一律缩减或丢弃，绝不回退到非预算路径；
//	window <= 0 → 非预算路径：各节按配置上限装配。
//
// 两条路径共用同一套参与条件（见 Source.participates），因此返回的快照只
// 表达"本轮装下的上下文"，空快照是合法结果（无动态内容或预算装不下）。
func BuildWindowedSnapshot(sources []Source, persistent []Item, window int, staticSystemPrompt string) Snapshot {
	if window > 0 {
		return CollectBudgeted(sources, persistent, window, staticSystemPrompt)
	}
	return Collect(sources, persistent)
}

// Build 非预算路径：各节按节序装入，正文为空的节跳过。
// 返回空串表示当前无动态上下文。
func Build(sources []Source) string {
	return Collect(sources, nil).Render()
}

// BuildBudgeted 按窗口预算编排动态上下文（字符串形态，保留给既有调用方）。
//
// 语义与 [CollectBudgeted] 完全一致，只是省去了条目层。
func BuildBudgeted(sources []Source, window int, staticSystemPrompt string) string {
	return CollectBudgeted(sources, nil, window, staticSystemPrompt).Render()
}

// BuildWindowed 是动态上下文装配的字符串入口：按 context_window 选择路径。
//
// 与 [BuildWindowedSnapshot] 等价，只是直接返回渲染结果。新代码应优先使用
// 快照形态（它保留了通道与 token 用量），字符串入口保留给只需要文本的调用方。
func BuildWindowed(sources []Source, window int, staticSystemPrompt string) string {
	return BuildWindowedSnapshot(sources, nil, window, staticSystemPrompt).Render()
}
