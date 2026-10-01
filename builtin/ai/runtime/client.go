// Package runtime 回合运行时的可复用逻辑：单轮 LLM 调用、回答校验与事实抽取。
//
// 这里只承载不依赖 AI 运行时状态的部分。装配侧（builtin/ai）把采样参数、
// 运行预算、提供商、记忆写入、作用域键与生命周期等依赖显式注入，本包不持有
// *Plugin，也不依赖插件配置包；回合编排（主工具循环、消息入口）仍留在装配侧。
package runtime

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// SingleRoundResult 单轮非流式 LLM 调用的结果。
type SingleRoundResult struct {
	// Text 模型返回的文本内容。
	Text string
	// ToolCalls 模型请求的工具调用（无则为空）。
	ToolCalls []protocol.ToolCall
}

// Client 单轮非流式 LLM 调用：请求形状（温度、核采样、最大 token）由采样参数决定。
type Client struct {
	// Temperature 采样温度。
	Temperature float64
	// TopP 核采样阈值。
	TopP float64
	// MaxTokens 单次请求的输出上限。
	MaxTokens int
	// ReasoningEffort 思考程度（reasoning_effort）；空 = 不携带该字段。
	// 校验/抽取这类"短、结构化"的输出通常不需要思考，可由装配侧设 none。
	ReasoningEffort string
	// Prov LLM 提供商。
	Prov protocol.Provider
}

// SingleRound 使用指定模型执行单轮非流式 LLM 调用。
// model 由调用方决定（主模型，或抽取/校验专用的分层模型）。
// tools 为已收敛好的协议层工具声明，可为空。
func (c Client) SingleRound(ctx context.Context, model string, messages []protocol.Message, tools []protocol.ToolSpec) (*SingleRoundResult, error) {
	req := &protocol.ChatRequest{
		Model:           model,
		Messages:        messages,
		Tools:           tools,
		Temperature:     c.Temperature,
		TopP:            c.TopP,
		MaxTokens:       c.MaxTokens,
		ReasoningEffort: c.ReasoningEffort,
	}

	resp, err := c.Prov.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	// 调用 ID 缺失时补齐（部分兼容端点不回填），保证工具结果能按 ID 回填。
	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].ID == "" {
			resp.ToolCalls[i].ID = fmt.Sprintf("call_%s_%d", resp.ToolCalls[i].Name, i)
		}
	}

	return &SingleRoundResult{
		Text:      resp.Content,
		ToolCalls: resp.ToolCalls,
	}, nil
}
