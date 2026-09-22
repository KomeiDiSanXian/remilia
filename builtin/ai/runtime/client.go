// Package runtime 回合运行时的可复用逻辑：单轮 LLM 调用、回答校验与事实抽取。
//
// 这里只承载不依赖 AI 运行时状态的部分。装配侧（builtin/ai）把配置、提供商、
// 记忆写入、作用域键与生命周期等依赖显式注入，本包不持有 *Plugin，也不反向
// 读取运行时字段；回合编排（主工具循环、消息入口）仍留在装配侧。
package runtime

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// SingleRoundResult 单轮非流式 LLM 调用的结果。
type SingleRoundResult struct {
	// Text 模型返回的文本内容。
	Text string
	// ToolCalls 模型请求的工具调用（无则为空）。
	ToolCalls []protocol.ToolCall
}

// Client 单轮非流式 LLM 调用：请求形状（温度、核采样、最大 token）由配置决定。
type Client struct {
	// Cfg 插件配置（提供采样参数）。
	Cfg *config.Config
	// Prov LLM 提供商。
	Prov protocol.Provider
}

// SingleRound 使用指定模型执行单轮非流式 LLM 调用。
// model 由调用方决定（主模型，或抽取/校验专用的分层模型）。
// tools 为已收敛好的协议层工具声明，可为空。
func (c Client) SingleRound(ctx context.Context, model string, messages []protocol.Message, tools []protocol.ToolSpec) (*SingleRoundResult, error) {
	req := &protocol.ChatRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.Cfg.Temperature,
		TopP:        c.Cfg.TopP,
		MaxTokens:   c.Cfg.MaxTokens,
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
