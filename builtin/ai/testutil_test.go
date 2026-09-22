package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// toolCtxForTest 组装工具执行 context：会话源信息 + 插件当前能力端口。
// 用于直接调用工具 Execute 的测试，等价于 executeToolResult 在生产路径上的注入
// （不含平台发送器，需要推送的工具另有 toolSessionForTest）。
func toolCtxForTest(p *Plugin, src toolkit.ToolSource) context.Context {
	return catalog.WithCapabilities(toolkit.WithToolSource(context.Background(), src), p.toolCapabilities())
}

// toolSessionForTest 组装带平台发送器的工具执行 context（定时提醒等主动推送工具）。
func toolSessionForTest(p *Plugin, src toolkit.ToolSource, sender platform.Sender) context.Context {
	return catalog.WithCapabilities(toolkit.WithToolInvocation(context.Background(), src, sender), p.toolCapabilities())
}

// buildSendToolsForTest 按生产装配方式构建发送类动作（markdown 取自 p.cfg）。
func buildSendToolsForTest(p *Plugin) []toolkit.Tool {
	return catalog.BuildSendTools(catalog.SendOptions{Markdown: p.cfg != nil && p.cfg.Markdown})
}

// buildOutboundMessageForTest 按生产装配方式组装出站消息（markdown 取自 p.cfg）。
func buildOutboundMessageForTest(p *Plugin, args map[string]any) (platform.OutboundMessage, error) {
	return catalog.BuildOutboundMessage(args, p.cfg != nil && p.cfg.Markdown)
}
