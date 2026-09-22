// Package ai extract.go — 事实记忆自动抽取的装配门面。
//
// 抽取提示词、最近一轮对话渲染、鲁棒解析与"跑一轮抽取并写入"见
// builtin/ai/runtime；本文件保留依赖插件状态的装配：启用判定、节流
// （memory_min_interval）、生命周期后台任务与作用域键格式。
package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// maybeExtractMemory 在对话回复完成后异步抽取记忆（受 memory_enabled 与节流控制）。
// 使用插件生命周期 context 执行，不依赖事件 context（事件可能在回复后很快超时）。
func (p *Plugin) maybeExtractMemory(ctx *eventctx.Context, session *session.Session) {
	if p.memory == nil || !p.memory.Enabled() {
		return
	}
	sender := ctx.GetSenderInfo()
	if sender.ID == "" {
		return
	}
	chat := ctx.GetChatInfo()

	scopes := []string{userScope(sender.ID)}
	if chat.IsGroup && chat.ID != "" {
		scopes = append(scopes, groupScope(chat.ID))
	}
	extractor := p.memoryExtractor()
	for _, scope := range scopes {
		if !p.memory.CanExtract(scope) {
			continue
		}
		p.memory.MarkExtracted(scope)
		scope := scope
		p.lifecycleSpawn(func() {
			if err := extractor.Extract(scope, sender.ID, chat, session); err != nil {
				logger.Debugf("[AI] Memory extract failed: %v", err)
			}
		})
	}
}

// memoryExtractor 组装事实抽取器；记忆写入端口按"未启用即 nil"注入。
func (p *Plugin) memoryExtractor() runtime.Extractor {
	return runtime.Extractor{
		Client:       p.runtimeClient(),
		Memory:       p.memoryWriter(),
		Scopes:       memoryScopeKeys{},
		LifecycleCtx: p.lifecycleCtx,
	}
}

// memoryWriter 返回记忆写入端口；未启用时返回 nil 接口——类型化 nil 指针
// 赋给端口后不再等于 nil，会把"未启用"误判为已启用。
func (s *contextState) memoryWriter() runtime.MemoryWriter {
	if s.memory == nil {
		return nil
	}
	return s.memory
}

// memoryScopeKeys 把记忆作用域键的构造交给运行时（键格式仍只在本包定义）。
type memoryScopeKeys struct{}

// UserScope 返回用户作用域键。
func (memoryScopeKeys) UserScope(userID string) string { return userScope(userID) }

// GroupScope 返回群作用域键。
func (memoryScopeKeys) GroupScope(chatID string) string { return groupScope(chatID) }

// lifecycleSpawn 在插件生命周期上下文上启动后台任务。
func (r *runtimeState) lifecycleSpawn(fn func()) {
	if r.lifecycleCtx == nil {
		go fn()
		return
	}
	go func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		select {
		case <-r.lifecycleCtx.Done():
		case <-done:
		}
	}()
}
