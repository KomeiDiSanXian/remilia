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
	"errors"
	"fmt"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// toolThreshold 触发两阶段路由的最小工具数。低于此值时直接发送全部工具。
const toolThreshold = 20

// ChatResult AI 对话的最终回复结果，包含文字和附件。
type ChatResult struct {
	Text        string
	Attachments []platform.Attachment
}

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
//  1. 如果工具总数超过 toolThreshold 且有多个分类，先进行一轮路由确定分类
//  2. 发送会话消息 + 对应分类的工具列表到 LLM
//  3. LLM 返回文本回复和/或工具调用请求
//  4. 有工具调用时：
//     a. 追加 assistant 消息（含 tool_calls）
//     b. 逐个执行工具，结果追加为 tool 消息
//     c. 回到步骤 2（递归深度 +1）
//  5. 无工具调用时返回最终文本（含捕获的附件）
//
// maxDepth 防止无限循环，默认最多 5 轮。
func (p *Plugin) processWithTools(ctx *eventctx.Context, session *Session) (*ChatResult, error) {
	currentDepth := 0
	maxDepth := p.cfg.MaxDepth

	cs := &captureSender{}

	// 工具分类路由 — 当工具数量超过阈值时先让 LLM 选择分类
	activeTools := p.reg.List()
	if len(activeTools) > toolThreshold {
		cats := collectToolCategories(activeTools)
		if len(cats) > 1 {
			if userContent := getLastUserMessage(session); userContent != "" {
				category, err := p.routeToolCategory(ctx.Context(), userContent, activeTools)
				if err != nil {
					logger.Debugf("[AI] Tool routing failed, using all tools: %v", err)
				} else {
					activeTools = filterToolsByCategory(activeTools, category)
					logger.Debugf("[AI] Routed to category %q, tools: %d→%d", category, len(p.reg.List()), len(activeTools))
				}
			}
		}
	}

	for currentDepth < maxDepth {
		currentDepth++

		session.Lock()
		session.CallCount++
		msgs := make([]Message, len(session.Messages))
		copy(msgs, session.Messages)
		session.Unlock()

		req := &ChatRequest{
			Model:       p.cfg.Model,
			Messages:    msgs,
			Tools:       activeTools,
			Temperature: p.cfg.Temperature,
			TopP:        p.cfg.TopP,
			MaxTokens:   p.cfg.MaxTokens,
		}

		streamCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.APITimeout)
		streamCh, err := p.prov.ChatStream(streamCtx, req)
		if err != nil {
			cancel()
			return &ChatResult{Text: cs.capturedText}, fmt.Errorf("chat stream: %w", err)
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
				return &ChatResult{Text: cs.capturedText}, event.Err
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
			return &ChatResult{
				Text:        responseText,
				Attachments: cs.capturedAttachments,
			}, nil
		}

		p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText, ToolCalls: toolCalls})

		for _, tc := range toolCalls {
			session.Lock()
			session.ToolCount++
			session.Unlock()
			toolCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.ToolTimeout)
			toolResult := p.executeTool(ctx, tc, toolCtx, cs)
			cancel()
			p.sm.AppendMessage(session, Message{
				Role:       RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}
	}

	return &ChatResult{Text: cs.capturedText}, fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// routeToolCategory 执行一轮非流式 LLM 调用，让 LLM 从工具分类中选择最合适的。
// 不修改 session，纯函数式。
func (p *Plugin) routeToolCategory(ctx context.Context, userContent string, allTools []Tool) (string, error) {
	cats := collectToolCategories(allTools)
	if len(cats) == 0 {
		return "", errors.New("no categories found")
	}
	routeTool := buildCategorySelectTool(cats)

	msgs := []Message{
		{Role: RoleSystem, Content: routeSystemPrompt},
		{Role: RoleUser, Content: userContent},
	}

	result, err := p.runSingleRound(ctx, msgs, []Tool{routeTool})
	if err != nil {
		return "", err
	}

	for _, tc := range result.ToolCalls {
		if tc.Name == categorySelectToolName {
			cat, _ := tc.Arguments["category"].(string)
			if cat != "" && containsCategory(cats, cat) {
				return cat, nil
			}
		}
	}

	return "", errors.New("unable to determine tool category")
}

// routeSystemPrompt 路由阶段的系统提示词。
const routeSystemPrompt = `你是一个工具分类助手。根据用户的问题，从可选类别中选择最合适的工具集类别。
如果问题涉及多个领域或不确定，请选择 "general"。
注意：你只需要使用 select_toolset 工具选择一个类别，不需要回答用户的问题。`

// collectToolCategories 收集所有工具的唯一分类。
func collectToolCategories(tools []Tool) []string {
	seen := make(map[string]struct{})
	var cats []string
	for _, t := range tools {
		cs := t.Categories
		if len(cs) == 0 {
			cs = []string{CategoryGeneral}
		}
		for _, c := range cs {
			if _, ok := seen[c]; !ok {
				seen[c] = struct{}{}
				cats = append(cats, c)
			}
		}
	}
	return cats
}

// containsCategory 检查分类是否在列表中。
func containsCategory(cats []string, target string) bool {
	for _, c := range cats {
		if c == target {
			return true
		}
	}
	return false
}

// filterToolsByCategory 按分类过滤工具列表。
// 一个工具只要在 Categories 中包含目标分类即视为匹配。
// 目标分类为 "general" 时匹配所有未显式设置分类的工具。
func filterToolsByCategory(tools []Tool, category string) []Tool {
	var out []Tool
	for _, t := range tools {
		if toolHasCategory(t, category) {
			out = append(out, t)
		}
	}
	return out
}

// toolHasCategory 判断工具是否属于指定分类。
// "general" 匹配所有 Categories 为空或包含 "general" 的工具。
func toolHasCategory(t Tool, category string) bool {
	if len(t.Categories) == 0 {
		return category == CategoryGeneral
	}
	for _, c := range t.Categories {
		if c == category {
			return true
		}
	}
	return false
}

// buildCategorySelectTool 构建用于路由阶段的选择分类工具。
func buildCategorySelectTool(categories []string) Tool {
	props := make(map[string]ToolParamSchema)
	props["category"] = ToolParamSchema{
		Type:        "string",
		Enum:        categories,
		Description: "根据用户问题选择的工具集类别",
	}

	return Tool{
		Name:        categorySelectToolName,
		Description: "根据用户的问题选择最合适的工具集类别",
		Parameters: ToolParamSchema{
			Type:       "object",
			Properties: props,
			Required:   []string{"category"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			cat, _ := args["category"].(string)
			return cat, nil
		},
	}
}

// getLastUserMessage 从 session 中提取最后一条用户消息的文本内容。
func getLastUserMessage(session *Session) string {
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == RoleUser {
			return session.Messages[i].Content
		}
	}
	return ""
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
