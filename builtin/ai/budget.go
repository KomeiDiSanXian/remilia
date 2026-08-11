// Package ai budget.go — 系统提示的全局 token 预算编排。
//
// 本文件实现 context_window 配置：按模型上下文窗口控制系统提示各注入节的
// 总量。此前各节（群聊窗口/长期记忆/相关历史/运行时上下文）各自为政，
// 小上下文模型可能被注入内容撑爆——预算模式按优先级动态缩减：
//
//	核心（框架+自定义指令）→ 运行时上下文 → 群聊窗口 → 长期记忆 → 相关历史
//
// 预算分配：核心内容优先保障；其余各节按优先级依次装入，每节以"计数减半"
// 的方式适配剩余预算（群聊窗口减条数、记忆/RAG 减注入上限），装不下则丢弃。
// context_window <= 0 时维持旧行为（各节按配置上限）。
package ai

import (
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// promptReserveTokens 预算中为输出 token 预留的余量。
const promptReserveTokens = 512

// estimateTextTokens 粗略估算文本 token 数（CJK 3 字节/字 ≈ 1 token，
// ASCII 混合下按字节/3 近似，够用于预算编排即可）。
func estimateTextTokens(s string) int {
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
		if est := estimateTextTokens(text); est <= *remain {
			*remain -= est
			return text
		}
		n /= 2
	}
	return ""
}

// effectiveCustomPrompt 返回生效的自定义系统提示词（群策略优先）。
func (p *Plugin) effectiveCustomPrompt(ctx *eventctx.Context) string {
	systemPrompt := p.cfg.SystemPrompt
	if gp := p.groupPolicyFor(ctx); gp != nil {
		if gpPrompt := gp.EffectiveSystemPrompt(); gpPrompt != "" {
			systemPrompt = gpPrompt
		}
	}
	return systemPrompt
}

// buildSystemPromptBudgeted 按 context_window 预算编排系统提示。
// 返回空串表示预算非法（调用方回退默认构建）。
func (p *Plugin) buildSystemPromptBudgeted(ctx *eventctx.Context, session *Session) string {
	window := p.cfg.ContextWindow
	if window <= 0 {
		return ""
	}

	remain := window - promptReserveTokens
	if remain <= 0 {
		remain = window / 2
	}

	// 核心内容：框架提示词 + 自定义指令（始终保障）。
	core := DefaultFrameworkPrompt
	if custom := p.effectiveCustomPrompt(ctx); custom != "" {
		core += "\n\n===== 自定义指令 =====\n" + custom
	}
	remain -= estimateTextTokens(core)
	if remain <= 0 {
		return core
	}

	var parts []string
	parts = append(parts, core)

	// 运行时上下文（预算允许时纳入）。
	if p.cfg.IncludeRuntimeContext {
		if rt := p.buildRuntimeContext(ctx); rt != "" {
			if est := estimateTextTokens(rt); est <= remain {
				parts = append(parts, "===== 运行时上下文 =====\n"+rt)
				remain -= est
			}
		}
	}

	// 群聊窗口 → 长期记忆 → 相关历史（优先级从高到低）。
	groupDefault := p.cfg.ContextGroupMessages
	if groupDefault <= 0 {
		groupDefault = 10
	}
	memDefault := p.cfg.MemoryInjectMax
	if memDefault <= 0 {
		memDefault = 8
	}

	// 会话历史已包含 AI 自己的回复（assistant 轮次）——开启
	// context_group_include_bot 时按内容去重，避免窗口与对话历史重复。
	var skipBot map[string]bool
	if p.cfg.ContextGroupIncludeBot && session != nil {
		skipBot = make(map[string]bool)
		for _, m := range session.Messages {
			if m.Role == RoleAssistant && m.Content != "" {
				skipBot[m.Content] = true
			}
		}
	}

	if text := fitSection(&remain, groupDefault, func(n int) string {
		return p.buildGroupContextN(ctx, skipBot, n)
	}); text != "" {
		parts = append(parts, "===== 群聊最近消息 =====\n"+text)
	}

	if p.memory != nil && p.memory.Enabled() {
		if text := fitSection(&remain, memDefault, func(n int) string {
			return p.buildMemoryContextN(ctx, session, n)
		}); text != "" {
			parts = append(parts, "===== 长期记忆 =====\n"+text)
		}
	}

	if p.cfg.ContextRAGMessages > 0 {
		if text := fitSection(&remain, p.cfg.ContextRAGMessages, func(n int) string {
			return p.buildRAGContextN(ctx, session, n)
		}); text != "" {
			parts = append(parts, "===== 相关历史消息 =====\n"+text)
		}
	}

	return strings.Join(parts, "\n\n")
}
