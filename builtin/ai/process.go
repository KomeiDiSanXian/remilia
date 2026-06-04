// Package ai process.go — LLM 调用循环与错误格式化。
//
// 本文件包含：
//   - processWithTools: 主工具调用循环（流式 LLM 调用 + 工具执行 + 回填）
//   - runSingleRound: 单轮非流式 LLM 调用（供 Skill 内部使用）
//   - singleRoundResult: 单轮调用结果
//   - formatAIError: LLM 错误码到用户友好提示的映射
//
// processWithTools 是整个 AI 插件的核心编排逻辑，
// 在工具调用循环中交替调用 LLM 和执行工具，直至达到最大深度或无工具调用。
package ai

import (
	"context"
	"fmt"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// singleRoundResult 单轮非流式 LLM 调用的结果。
type singleRoundResult struct {
	Text      string
	ToolCalls []ToolCall
}

// runSingleRound 执行单轮非流式 LLM 调用，返回文本回复和工具调用。
// 不涉及 session 管理，纯函数式，供 executeSkill 内部循环使用。
func (p *Plugin) runSingleRound(ctx context.Context, messages []Message, tools []Tool) (*singleRoundResult, error) {
	req := &ChatRequest{
		Model:       p.cfg.Model,
		Messages:    messages,
		Tools:       tools,
		Temperature: p.cfg.Temperature,
		TopP:        p.cfg.TopP,
		MaxTokens:   p.cfg.MaxTokens,
	}

	resp, err := p.prov.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].ID == "" {
			resp.ToolCalls[i].ID = fmt.Sprintf("call_%s_%d", resp.ToolCalls[i].Name, i)
		}
	}

	return &singleRoundResult{
		Text:      resp.Content,
		ToolCalls: resp.ToolCalls,
	}, nil
}

// processWithTools 执行 AI 对话的工具调用循环。
//
// 循环逻辑：
//  1. 发送会话消息 + 工具列表到 LLM
//  2. LLM 返回文本回复和/或工具调用请求
//  3. 有工具调用时：
//     a. 追加 assistant 消息（含 tool_calls）
//     b. 逐个执行工具，结果追加为 tool 消息
//     c. 回到步骤 1（递归深度 +1）
//  4. 无工具调用时返回最终文本
//
// maxDepth 防止无限循环，默认最多 5 轮。
func (p *Plugin) processWithTools(ctx *eventctx.Context, session *Session) (string, error) {
	currentDepth := 0
	maxDepth := p.cfg.MaxDepth
	partialContent := ""

	for currentDepth < maxDepth {
		currentDepth++

		session.CallCount++

		req := &ChatRequest{
			Model:       p.cfg.Model,
			Messages:    session.Messages,
			Tools:       p.reg.List(),
			Temperature: p.cfg.Temperature,
			TopP:        p.cfg.TopP,
		}

		streamCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.APITimeout)
		streamCh, err := p.prov.ChatStream(streamCtx, req)
		if err != nil {
			cancel()
			return partialContent, fmt.Errorf("chat stream: %w", err)
		}

		var fullResponse strings.Builder
		var toolCalls []ToolCall

		for event := range streamCh {
			switch event.Type {
			case StreamEventText:
				fullResponse.WriteString(event.Content)
			case StreamEventToolCall:
				if event.ToolCall != nil && event.ToolCall.Name != "" {
					toolCalls = append(toolCalls, *event.ToolCall)
				}
			case StreamEventError:
				cancel()
				return partialContent, event.Err
			case StreamEventDone:
			}
		}
		cancel()

		responseText := fullResponse.String()

		for i := range toolCalls {
			if toolCalls[i].ID == "" {
				toolCalls[i].ID = fmt.Sprintf("call_%s_%d", toolCalls[i].Name, i)
			}
		}

		if len(toolCalls) == 0 {
			if responseText != "" {
				p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText})
			}
			return responseText, nil
		}

		p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText, ToolCalls: toolCalls})

		for _, tc := range toolCalls {
			session.ToolCount++
			toolCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.ToolTimeout)
			toolResult := p.executeTool(ctx, tc, toolCtx)
			cancel()
			p.sm.AppendMessage(session, Message{
				Role:       RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}

		partialContent = responseText
	}

	return partialContent, fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// formatAIError 将 provider 返回的错误转换为用户友好的提示。
//
// 常见错误映射：
//   - 401: API Key 无效或未配置
//   - 404: 模型名称错误或 API 地址不对
//   - 429: 速率限制
//   - timeout / context deadline: 请求超时（可检查网络或增大 timeout）
//     其他: 显示原始错误
func formatAIError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return "API 认证失败，请检查 api_key 配置"
	case strings.Contains(msg, "404"):
		return "API 地址或模型名称错误，请检查 base_url 和 model 配置"
	case strings.Contains(msg, "429"):
		return "请求过于频繁，请稍后再试"
	case strings.Contains(msg, "context deadline exceeded"):
		return "请求超时，请检查网络连接或增大超时配置"
	case strings.Contains(msg, "connection refused"):
		return "无法连接 API 服务器，请检查 base_url 配置"
	case strings.Contains(msg, "no such host"):
		return "API 域名解析失败，请检查 base_url 配置"
	default:
		return fmt.Sprintf("AI 处理出错: %v", err)
	}
}
