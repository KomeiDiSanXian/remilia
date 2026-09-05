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
	// platform 当前平台标识（如 qq / telegram）。
	platform string
	// isGroup 是否为群聊会话。
	isGroup bool
	// sender 平台发送器（可能为 nil，如测试或平台不可用时）。
	sender platform.Sender
	// p 插件实例（访问提醒/记忆/待办等会话级能力）。
	p *Plugin
}

// ToolSource 插件可读取的会话源信息（经 [ToolSourceFromContext] 提取）。
type ToolSource struct {
	// UserID 当前发送者 ID。
	UserID string
	// ChatID 当前会话 ID（群或私聊）。
	ChatID string
	// Platform 当前平台标识（如 qq / telegram）。
	Platform string
	// IsGroup 是否为群聊会话。
	IsGroup bool
}

// withToolSource 将工具源信息注入 context。
func withToolSource(ctx context.Context, src toolSource) context.Context {
	return context.WithValue(ctx, ctxKeyToolSource{}, src)
}

// WithToolSource 将会话源信息注入 context（测试或自定义执行方使用）。
func WithToolSource(ctx context.Context, src ToolSource) context.Context {
	return context.WithValue(ctx, ctxKeyToolSource{}, toolSource{
		userID:   src.UserID,
		chatID:   src.ChatID,
		platform: src.Platform,
		isGroup:  src.IsGroup,
	})
}

// toolSourceFromContext 从 context 中提取工具源信息。
// 若 context 中无源信息，返回零值和 false。
func toolSourceFromContext(ctx context.Context) (toolSource, bool) {
	s, ok := ctx.Value(ctxKeyToolSource{}).(toolSource)
	return s, ok
}

// ToolSourceFromContext 从工具执行 context 中提取会话源信息。
// 信息由 AI 插件在 executeTool 时注入；无注入时返回零值和 false
// （如直接调用工具的测试场景）。供需要会话级状态的插件工具使用。
func ToolSourceFromContext(ctx context.Context) (ToolSource, bool) {
	s, ok := toolSourceFromContext(ctx)
	if !ok {
		return ToolSource{}, false
	}
	return ToolSource{
		UserID:   s.userID,
		ChatID:   s.chatID,
		Platform: s.platform,
		IsGroup:  s.isGroup,
	}, true
}
