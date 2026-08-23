// Package ai toolctx.go — 工具执行上下文注入。
//
// executeTool 在调用工具 Execute 前，把当前事件的关键上下文（发送者、会话、
// 平台发送器、插件实例）注入工具调用 context，供定时提醒/记忆/待办等需要
// "写回会话状态" 的工具使用。工具 Execute 签名不接触事件上下文，与
// send_message/send_to 保持一致的安全边界。
package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ctxKeyToolSource 是 context 中存储工具源信息的键。
type ctxKeyToolSource struct{}

// toolSource 工具可用的会话源信息（executeTool 注入）。
type toolSource struct {
	// userID 当前发送者 ID。
	userID string
	// chatID 当前会话 ID（群或私聊）。
	chatID string
	// isGroup 是否为群聊会话。
	isGroup bool
	// sender 平台发送器（可能为 nil，如测试或平台不可用时）。
	sender platform.Sender
	// p 插件实例（访问提醒/记忆/待办等会话级能力）。
	p *Plugin
}

// withToolSource 将工具源信息注入 context。
func withToolSource(ctx context.Context, src toolSource) context.Context {
	return context.WithValue(ctx, ctxKeyToolSource{}, src)
}

// toolSourceFromContext 从 context 中提取工具源信息。
// 若 context 中无源信息，返回零值和 false。
func toolSourceFromContext(ctx context.Context) (toolSource, bool) {
	s, ok := ctx.Value(ctxKeyToolSource{}).(toolSource)
	return s, ok
}
