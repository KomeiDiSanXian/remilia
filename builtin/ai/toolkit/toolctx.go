// toolctx.go — 工具执行上下文注入。
//
// 工具 Execute 的签名只接受 context 与参数，因此"这次调用发生在哪个会话、由谁发起、
// 用哪个平台发送器"必须经 context 传递。本文件定义这套注入约定，与
// [WithCallerInfo] / [WithToolSender] 同属工具调用契约：
//
//   - [ToolSource] 是公开视图（会话源信息，插件作者经 [ToolSourceFromContext] 读取）；
//   - 平台发送器单独经 [PlatformSenderFromContext] 取用，因为它只在需要主动推送
//     的工具（如定时提醒）里用得到，不进入公开视图。
//
// 工具回调不接触事件上下文，与 send_message / send_to 保持同一安全边界。
package toolkit

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ctxKeyToolSourceType 是 context 中存储工具调用源信息的键。
type ctxKeyToolSourceType struct{}

// toolSource 注入 carrier：公开视图 + 本次调用的平台发送器。
type toolSource struct {
	source ToolSource
	sender platform.Sender
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

// WithToolSource 注入会话源信息，平台发送器保持为空
// （测试或自定义执行方使用）。
func WithToolSource(ctx context.Context, src ToolSource) context.Context {
	return context.WithValue(ctx, ctxKeyToolSourceType{}, toolSource{source: src})
}

// WithToolInvocation 注入一次工具调用的会话源信息与平台发送器。
// 执行路径（executeToolResult）用它构造工具调用 context。
func WithToolInvocation(ctx context.Context, src ToolSource, sender platform.Sender) context.Context {
	return context.WithValue(ctx, ctxKeyToolSourceType{}, toolSource{source: src, sender: sender})
}

// toolSourceFromContext 从 context 中提取完整的调用源信息。
// 若 context 中无源信息，返回零值和 false。
func toolSourceFromContext(ctx context.Context) (toolSource, bool) {
	s, ok := ctx.Value(ctxKeyToolSourceType{}).(toolSource)
	return s, ok
}

// ToolSourceFromContext 从工具执行 context 中提取会话源信息。
// 信息由调用方在 executeToolResult 时注入；无注入时返回零值和 false
// （如直接调用工具的测试场景）。供需要会话级状态的插件工具使用。
func ToolSourceFromContext(ctx context.Context) (ToolSource, bool) {
	s, ok := toolSourceFromContext(ctx)
	if !ok {
		return ToolSource{}, false
	}
	return s.source, true
}

// PlatformSenderFromContext 从工具执行 context 中提取本次调用的平台发送器。
// 未注入源信息或发送器为空时返回 nil 与 false（如测试场景、平台不可用）。
func PlatformSenderFromContext(ctx context.Context) (platform.Sender, bool) {
	s, ok := toolSourceFromContext(ctx)
	if !ok || s.sender == nil {
		return nil, false
	}
	return s.sender, true
}
