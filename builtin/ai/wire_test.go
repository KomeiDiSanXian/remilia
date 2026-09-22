package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// 协议格式（工具声明序列化与调用解析）已归位到 builtin/ai/protocol。
// 这里保留包内测试惯用的名字，使既有断言直接观察真实序列化结果，
// 而不是另写一份等价实现。
type (
	openaiTool            = protocol.OpenAITool
	anthropicTool         = protocol.AnthropicTool
	openaiToolCall        = protocol.OpenAIToolCall
	anthropicContentBlock = protocol.AnthropicContentBlock
)

func toOpenAITools(actions []toolkit.Action) []openaiTool {
	return protocol.ToOpenAITools(wireSpecs(actions))
}

func toAnthropicTools(actions []toolkit.Action) []anthropicTool {
	return protocol.ToAnthropicTools(wireSpecs(actions))
}

func parseOpenAIToolCalls(raw []openaiToolCall) ([]protocol.ToolCall, error) {
	return protocol.ParseOpenAIToolCalls(raw)
}

func parseAnthropicToolCalls(blocks []anthropicContentBlock) []protocol.ToolCall {
	return protocol.ParseAnthropicToolCalls(blocks)
}
