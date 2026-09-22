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

import "strings"

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
	// Shrink 为 true 时预算路径以"计数减半"适配剩余预算（FitSection）；
	// false 时单次估测，装不下即丢弃（既有运行时上下文语义）。
	Shrink bool
}

// participates 判断本节是否参与本轮（两条路径共用同一条件）。
// Enabled 为 nil 表示恒参与；正文为空的节由装配侧跳过。
func (s Source) participates() bool {
	return s.Enabled == nil || s.Enabled()
}

// fit 在剩余预算内装入本节，装入成功时扣减 *remain。
// Shrink 为 true 时按计数减半重试；否则单次估测，正文为空或超预算都不装入。
func (s Source) fit(remain *int) string {
	if s.Shrink {
		return fitSection(remain, s.Limit, s.Body)
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

// fitSection 在预算内构建一节：从上限 max 开始，估测超预算则计数减半重试，
// 直至装入或为 0。返回构建文本（空串 = 未装入），并扣减剩余预算。
func fitSection(remain *int, max int, build func(n int) string) string {
	n := max
	for n > 0 {
		text := build(n)
		if est := EstimateTokens(text); est <= *remain {
			*remain -= est
			return text
		}
		n /= 2
	}
	return ""
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
// 返回空串表示剩余预算装不下任何动态节。
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
