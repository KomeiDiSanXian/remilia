// Package ai dynamiccontext.go — 动态上下文的节来源（Provider）声明。
//
// 每节只回答"我能提供什么上下文"：这一节是否参与本轮、标题是什么、在给定
// 注入上限下生成什么正文。节序、预算编排与最终拼接由 builtin/ai/promptctx
// 的 Builder 负责，不在节内部决定。
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
package ai

import (
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// dynamicContextSources 声明动态上下文的节序与各节参与条件。
// 正文生成是惰性的：未参与的节不会被求值（群聊查询、记忆/历史检索都不发生）。
//
// session 提供当前会话的 assistant 回复内容，供群聊消息窗口包含机器人回复
// （context_group_include_bot）时去重；nil 时不去重。同时用于长期记忆/相关
// 历史的检索查询词，因此调用时应保证本轮用户消息已写入会话。
func (p *Plugin) dynamicContextSources(ctx *eventctx.Context, session *session.Session) []promptctx.Source {
	groupDefault := p.cfg.ContextGroupMessages
	if groupDefault <= 0 {
		groupDefault = 10
	}
	memDefault := p.cfg.MemoryInjectMax
	if memDefault <= 0 {
		memDefault = 8
	}
	// 机器人回复去重集合按需构建、只构建一次（预算缩减会多次生成正文）。
	skipBot := sync.OnceValue(func() map[string]bool { return promptctx.BotReplyContents(p.cfg, session) })

	return []promptctx.Source{
		{
			Header:  "运行时上下文",
			Enabled: func() bool { return p.cfg.IncludeRuntimeContext },
			Body:    func(int) string { return promptctx.BuildRuntimeContext(p.cfg, ctx) },
		},
		{
			Header:  "群聊最近消息",
			Enabled: func() bool { return p.cfg.ContextGroupMessages > 0 },
			Limit:   groupDefault,
			Body:    func(n int) string { return promptctx.BuildGroupWindowN(p.history, p.cfg, ctx, skipBot(), n) },
			Shrink:  true,
		},
		{
			Header:  "长期记忆",
			Enabled: func() bool { return p.memory != nil && p.memory.Enabled() },
			Limit:   memDefault,
			Body:    func(n int) string { return p.buildMemoryContextN(ctx, session, n) },
			Shrink:  true,
		},
		{
			Header:  "相关历史消息",
			Enabled: func() bool { return p.cfg.ContextRAGMessages > 0 },
			Limit:   p.cfg.ContextRAGMessages,
			Body:    func(n int) string { return p.buildRAGContextN(ctx, session, n) },
			Shrink:  true,
		},
	}
}

// buildDynamicContext 构建 Dynamic Context —— 逐轮变化的上下文。
//
// 依次包含（各自独立配置控制）：
//
//	运行时上下文（include_runtime_context）→ 群聊最近消息（context_group_messages）
//	→ 长期记忆（memory_enabled）→ 相关历史消息（context_rag_messages）
//
// context_window > 0 时按预算编排，动态各节按上述优先级依次装入、装不下
// 则缩减或丢弃（见 promptctx.BuildBudgeted）。
//
// 返回空串表示当前无动态上下文。调用方负责把它注入到请求尾部
// （processWithTools 挂在本轮用户消息上），不要写进 System 消息。
func (p *Plugin) buildDynamicContext(ctx *eventctx.Context, session *session.Session) string {
	sources := p.dynamicContextSources(ctx, session)
	if p.cfg.ContextWindow > 0 {
		if budgeted := promptctx.BuildBudgeted(sources, p.cfg.ContextWindow, p.buildStaticSystemPrompt(ctx)); budgeted != "" {
			return budgeted
		}
	}
	return promptctx.Build(sources)
}

// buildMemoryContextN 同上，注入条数由调用方给定（预算编排时可动态缩减）。
func (s *contextState) buildMemoryContextN(ctx *eventctx.Context, session *session.Session, limit int) string {
	sender := ctx.GetSenderInfo()
	mem := promptctx.MemoryReader(nil)
	if s.memory != nil {
		mem = s.memory
	}
	return promptctx.BuildMemoryContext(mem, ctx, runtime.LastUserMessage(session),
		userScope(sender.ID), groupScope(ctx.GetChatInfo().ID), limit)
}

// buildRAGContextN 检索并格式化相关历史消息注入文本（无命中返回空串）。
// max 为注入条数上限（<=0 表示关闭本功能）；条数上限由调用方给定。
func (p *Plugin) buildRAGContextN(ctx *eventctx.Context, session *session.Session, max int) string {
	return promptctx.BuildRAGContext(p.history, p.emb, p.cfg, ctx, session, runtime.LastUserMessage(session), max)
}
