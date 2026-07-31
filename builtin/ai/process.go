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
	"slices"
	"strings"
	"time"

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

	// 工具分类路由 — 当工具数量超过阈值时先让 LLM 选择分类。
	// 路由决策按会话缓存（TTL 内且工具集未变化时复用），避免每条消息都多一次 LLM 调用。
	activeTools := p.reg.List()
	activeTools = append(activeTools, p.buildUserSkillTools(session.UserID)...)
	if len(activeTools) > toolThreshold {
		cats := collectToolCategories(activeTools)
		if len(cats) > 1 {
			if userContent := getLastUserMessage(session); userContent != "" {
				category, err := p.getOrRouteCategory(ctx, session, userContent, activeTools, cats)
				if err != nil {
					logger.Debugf("[AI] Tool routing failed, using all tools: %v", err)
				} else {
					before := len(activeTools)
					activeTools = filterToolsByCategory(activeTools, category)
					logger.Debugf("[AI] Routed to category %q, tools: %d→%d", category, before, len(activeTools))
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

		// 除当前轮（最后一条用户消息）外，历史消息中的附件二进制数据
		// 降级为文本占位，避免每轮向 LLM 重复上传图片/音频（内存与 token 浪费）。
		msgs = prepareRequestMessages(msgs)

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
				Content:    truncateToolResult(toolResult),
				ToolCallID: tc.ID,
			})
		}
	}

	return &ChatResult{Text: cs.capturedText}, fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// routeCacheTTL 工具分类路由决策在会话内的缓存有效期。
// 过期后下一条消息会重新路由，避免用户切换话题后被陈旧分类长期困住。
const routeCacheTTL = 10 * time.Minute

// getOrRouteCategory 获取本次消息应使用的工具分类。
//
// 优先复用会话内缓存的分类（TTL 内且工具集数量未变化），
// 命中则直接返回，避免每条消息都触发一次路由 LLM 调用。
// 未命中时执行 routeToolCategory 并把结果写入会话缓存。
func (p *Plugin) getOrRouteCategory(ctx *eventctx.Context, session *Session, userContent string, activeTools []Tool, cats []string) (string, error) {
	session.Lock()
	cached := session.routeCategory
	cachedAt := session.routeAt
	cachedCount := session.routeToolCount
	session.Unlock()

	if cached != "" && containsCategory(cats, cached) &&
		time.Since(cachedAt) <= routeCacheTTL && cachedCount == len(activeTools) {
		logger.Debugf("[AI] Reusing cached route category %q", cached)
		return cached, nil
	}

	category, err := p.routeToolCategory(ctx.Context(), userContent, activeTools)
	if err != nil {
		return "", err
	}

	session.Lock()
	session.routeCategory = category
	session.routeAt = time.Now()
	session.routeToolCount = len(activeTools)
	session.Unlock()
	return category, nil
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
	return slices.Contains(cats, target)
}

// filterToolsByCategory 按分类过滤工具列表。
// 一个工具只要在 Categories 中包含目标分类即视为匹配。
// 通用工具（Categories 为空或包含 "general"）始终保留，
// 作为分类路由缓存过期/误判时的兜底，保证基础工具永远可用。
func filterToolsByCategory(tools []Tool, category string) []Tool {
	var out []Tool
	for _, t := range tools {
		if toolHasCategory(t, category) || toolHasCategory(t, CategoryGeneral) {
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
	return slices.Contains(t.Categories, category)
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

// buildUserSkillTools 构建当前会话用户的已启用 Skill 列表，包装为 Tool。
func (p *Plugin) buildUserSkillTools(userID string) []Tool {
	skills := p.skillReg.ListByOwner(userID)
	if len(skills) == 0 {
		return nil
	}
	tools := make([]Tool, 0, len(skills))
	for _, s := range skills {
		if !s.Enabled {
			continue
		}
		skill := s
		tools = append(tools, Tool{
			Name:        skill.Name,
			Description: skill.Description,
			Parameters:  skill.Parameters,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				p.skillReg.IncrementUsage(skill.OwnerID, skill.Name)
				return p.executeSkill(ctx, skill, args)
			},
		})
	}
	return tools
}

// getLastUserMessage 从 session 中提取最后一条用户消息的文本内容。
func getLastUserMessage(session *Session) string {
	session.Lock()
	defer session.Unlock()
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == RoleUser {
			return session.Messages[i].Content
		}
	}
	return ""
}

// prepareRequestMessages 返回用于 LLM 请求的消息副本。
// 仅保留最后一条用户消息（当前轮）的附件二进制数据，其余历史消息中
// 的图片/音频内容被替换为文本占位，防止每轮对话重复上传附件。
func prepareRequestMessages(msgs []Message) []Message {
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			lastUserIdx = i
			break
		}
	}

	out := make([]Message, len(msgs))
	for i, m := range msgs {
		if i == lastUserIdx {
			out[i] = m
			continue
		}
		out[i] = stripBinaryParts(m)
	}
	return out
}

// stripBinaryParts 将消息中的多模态附件二进制内容替换为文本占位。
// 无 ContentParts 时原样返回。
func stripBinaryParts(m Message) Message {
	if len(m.ContentParts) == 0 {
		return m
	}
	parts := make([]string, 0, len(m.ContentParts))
	for _, p := range m.ContentParts {
		switch p.Type {
		case ContentPartText:
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		case ContentPartImage:
			parts = append(parts, "[历史图片内容已过期]")
		case ContentPartAudio:
			parts = append(parts, "[历史音频内容已过期]")
		}
	}
	m.ContentParts = nil
	m.Content = strings.Join(parts, "\n")
	return m
}

// maxToolResultLen 单条工具结果回填给 LLM 的最大字符数。
// 防止一条命令输出巨型结果撑爆上下文窗口。
const maxToolResultLen = 8000

// truncateToolResult 截断过长的工具结果，按 rune 截断避免劈开多字节字符。
func truncateToolResult(result string) string {
	runes := []rune(result)
	if len(runes) <= maxToolResultLen {
		return result
	}
	return string(runes[:maxToolResultLen]) + "\n…(工具结果过长已截断)"
}

// formatAIError 将 provider 返回的错误转换为用户友好的提示。
//
// 常见错误映射：
//   - 401: API Key 无效或未配置
//   - 404: 模型名称错误或 API 地址不对
//   - 429: 速率限制
//   - timeout / context deadline: 请求超时（可检查网络或增大 timeout）
//     其他: 记录详细日志后返回通用提示，避免向用户暴露可能包含
//     组织/计费信息的原始 API 错误体。
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
		logger.Warnf("[AI] Unhandled LLM error: %v", err)
		return "AI 处理出错，请稍后再试"
	}
}
