package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// testPlanAccess 把会话直接暴露为 catalog.PlanAccess：完成闸门默认放行，
// 需要闸门行为的用例走装配侧实现（taskPlanAccess）。
type testPlanAccess struct {
	*session.Session
}

// CompletionGuard 默认放行，保证既有计划工具用例与生产闸门解耦。
func (testPlanAccess) CompletionGuard() error { return nil }

// planAccessForTest 包装会话为测试用计划端口。
func planAccessForTest(sess *session.Session) catalog.PlanAccess {
	return testPlanAccess{Session: sess}
}

// openAIProviderForTest / anthropicProviderForTest 用插件配置构造协议层提供商：
// 与装配侧（providerOptions）走同一映射，用例因此不必各自拼 ProviderOptions。
func openAIProviderForTest(cfg *config.Config) (protocol.Provider, error) {
	return protocol.NewOpenAIProvider(providerOptions(cfg))
}

func anthropicProviderForTest(cfg *config.Config) (protocol.Provider, error) {
	return protocol.NewAnthropicProvider(providerOptions(cfg))
}

// testPlanInvocation 构造只带计划端口的调用上下文（仅测试用）。
func testPlanInvocation(plans catalog.PlanAccess) *toolkit.Invocation {
	inv := &toolkit.Invocation{}
	catalog.AttachPlans(inv, plans)
	return inv
}

// toolCtxForTest 组装工具调用上下文：会话源信息 + 插件当前能力端口。
// 用于直接调用工具 Execute 的测试，等价于 executeToolResult 在生产路径上的注入
// （不含平台发送器，需要推送的工具另有 toolSessionForTest）。
func toolCtxForTest(p *Plugin, src toolkit.ToolSource) *toolkit.Invocation {
	return toolInvocationForTest(p, src, nil)
}

// toolSessionForTest 组装带平台发送器的工具调用上下文（定时提醒等主动推送工具）。
func toolSessionForTest(p *Plugin, src toolkit.ToolSource, sender platform.Sender) *toolkit.Invocation {
	return toolInvocationForTest(p, src, sender)
}

func toolInvocationForTest(p *Plugin, src toolkit.ToolSource, sender platform.Sender) *toolkit.Invocation {
	inv := &toolkit.Invocation{Source: src}
	catalog.AttachCapabilities(inv, p.toolCapabilities(sender))
	return inv
}

// buildSendToolsForTest 按生产装配方式构建发送类动作（markdown 取自 p.cfg）。
func buildSendToolsForTest(p *Plugin) []toolkit.Tool {
	return catalog.BuildSendTools(catalog.SendOptions{Markdown: p.cfg != nil && p.cfg.Markdown})
}

// buildOutboundMessageForTest 按生产装配方式组装出站消息（markdown 取自 p.cfg）。
func buildOutboundMessageForTest(p *Plugin, args map[string]any) (platform.OutboundMessage, error) {
	return catalog.BuildOutboundMessage(args, p.cfg != nil && p.cfg.Markdown)
}
