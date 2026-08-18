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
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

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

// runSingleRound 使用主模型执行单轮非流式 LLM 调用，返回文本回复和工具调用。
// 不涉及 session 管理，纯函数式，供 executeSkill 内部循环使用。
func (p *Plugin) runSingleRound(ctx context.Context, messages []Message, tools []Tool) (*singleRoundResult, error) {
	return p.runSingleRoundModel(ctx, p.cfg.Model, messages, tools)
}

// runSingleRoundModel 使用指定模型执行单轮非流式 LLM 调用。
// 模型为空时回退主模型；供校验/抽取等任务使用独立模型（多模型分层）。
func (p *Plugin) runSingleRoundModel(ctx context.Context, model string, messages []Message, tools []Tool) (*singleRoundResult, error) {
	req := &ChatRequest{
		Model:       model,
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
//  1. 工具总数超过 tool_select_max 时按用户消息本地检索 Top-K（selectToolsForTurn）
//  2. 发送会话消息 + 选中工具列表到 LLM
//  3. LLM 返回文本回复和/或工具调用请求
//  4. 有工具调用时：
//     a. 追加 assistant 消息（含 tool_calls）
//     b. 逐个执行工具，结果追加为 tool 消息
//     c. 失败工具按重试预算处理：连续失败第 2 次起注入反思指令，
//     达到 tool_retry_limit+1 次时优雅中止（见 retry.go）
//     d. 回到步骤 2（递归深度 +1）
//  5. 无工具调用时返回最终文本（含捕获的附件）
//
// maxDepth 防止无限循环，默认最多 5 轮。
func (p *Plugin) processWithTools(ctx *eventctx.Context, session *Session) (*ChatResult, error) {
	currentDepth := 0
	maxDepth := p.cfg.MaxDepth

	cs := &captureSender{}
	// 消息发送预算：一次运行内 send_message/send_to 的总发送次数上限，
	// 跨工具调用共享（含并行路径），超限报错回填给模型。
	budget := &sendBudget{limit: p.cfg.MaxSendsPerRound}

	// 全局 Timeout 中间件（cmd/bot 默认 30s）会给事件上下文注入单次
	// deadline，多轮工具循环（每轮 LLM 调用 + 工具执行）共享同一 deadline
	// 时，长任务会在中途被整段切断（如 send_message 分步任务第二轮超时）。
	// 这里以插件独立预算替换（turn_timeout，见 effectiveTurnTimeout）。
	restoreDeadline := p.liftEventDeadline(ctx)
	defer restoreDeadline()

	// 工具选择 — 工具较多时按当前用户消息本地检索 Top-K，
	// 替代旧的 LLM 单分类路由（零额外 LLM 调用，跨域任务自然覆盖多个分类）。
	activeTools := p.reg.List()
	activeTools = append(activeTools, p.buildUserSkillTools(session.UserID)...)
	// per-group 工具白名单过滤（/ai group set tools）
	if gp := p.groupPolicyFor(ctx); gp != nil {
		before := len(activeTools)
		activeTools = filterToolsByGroupPolicy(activeTools, gp)
		if len(activeTools) != before {
			logger.Debugf("[AI] Group policy filtered tools: %d→%d", before, len(activeTools))
		}
	}
	activeTools = p.selectToolsForTurn(ctx, session, activeTools)

	for currentDepth < maxDepth {
		currentDepth++

		// 中断检查点：用户新消息抢占时，未开始的轮次直接收尾。
		if session.Interrupted() {
			return &ChatResult{Text: cs.capturedText}, nil
		}

		session.Lock()
		session.CallCount++
		msgs := make([]Message, len(session.Messages))
		copy(msgs, session.Messages)
		session.Unlock()

		// 除当前轮（最后一条用户消息）外，历史消息中的附件二进制数据
		// 降级为文本占位，避免每轮向 LLM 重复上传图片/音频（内存与 token 浪费）。
		msgs = prepareRequestMessages(msgs)

		// 兜底修复工具调用序列：中断跳过的工具、进程异常退出或持久化损坏
		// 都可能让 assistant(tool_calls) 缺少对应 tool 消息（OpenAI/Anthropic
		// API 硬性约束，缺失即 400）。按需补占位 tool 消息，自愈会话历史。
		msgs = repairToolCallSequence(msgs)

		// 计划注入：存在进行中的任务计划时，把"当前计划+进度"作为 system 消息
		// 插在主系统提示之后，让模型有状态可依地推进多步任务。
		if planText := session.planText(); planText != "" {
			planMsg := Message{Role: RoleSystem, Content: "===== 当前执行计划 =====\n" + planText}
			idx := 0
			for i, m := range msgs {
				if m.Role != RoleSystem {
					idx = i
					break
				}
			}
			msgs = append(msgs[:idx], append([]Message{planMsg}, msgs[idx:]...)...)
		}

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

		// 重规划闭环：前序步骤全部终态、自身失败的前沿步骤 → 自动追加
		// 重规划指令（要求模型调整计划而非按旧计划继续），替代"软计划"
		// 依赖模型自觉的现状。同一条指令不重复追加。
		var replanMsg *Message
		if plan := session.planSnapshot(); plan != nil && plan.Active {
			if f := plan.firstFailedFrontier(); f != nil && !lastUserIsReplan(session) {
				m := buildReplanMessage(f)
				replanMsg = &m
			}
		}

		if len(toolCalls) == 0 {
			if responseText != "" {
				p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText})
			}
			if replanMsg != nil {
				p.sm.AppendMessage(session, *replanMsg)
			}
			return &ChatResult{
				Text:        responseText,
				Attachments: cs.capturedAttachments,
			}, nil
		}

		p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText, ToolCalls: toolCalls})

		// 并行执行本轮全部工具调用（tool_parallel 控制并发度，默认 4；
		// 审批/执行/追踪各自独立，结果按原始顺序回填）。
		results := p.executeToolCallsParallel(ctx, session, cs, budget, toolCalls)

		for i, tc := range toolCalls {
			// 中断抢占后未启动的工具调用：补占位 tool 消息，保证 assistant
			// 的每个 tool_call_id 都有对应 tool 响应（API 硬性约束，
			// 缺失会导致下次请求被 400 拒绝）。
			if results[i].skipped {
				p.sm.AppendMessage(session, Message{
					Role:       RoleTool,
					Content:    "（工具未执行：对话被新消息打断）",
					ToolCallID: tc.ID,
				})
				continue
			}
			toolResult := results[i].result
			p.sm.AppendMessage(session, Message{
				Role:       RoleTool,
				Content:    truncateToolResult(toolResult),
				ToolCallID: tc.ID,
			})

			// 计划创建时同步展示给用户（chat 内可见计划，后续步骤更新不打扰）。
			if tc.Name == planCreateToolName && !isToolErrorResult(toolResult) {
				p.replyAndRecord(ctx, platform.OutboundMessage{Text: toolResult})
			}

			// 失败重试预算与反思引导：
			//   - 失败结果已回填（模型天然可重试）
			//   - 同一工具连续失败第 2 次起，追加"反思指令"用户消息，
			//     强制模型先分析原因再采用不同策略（显式反思轮）
			//   - 连续失败达到 tool_retry_limit+1 次时优雅中止本轮，
			//     替代撞 max_depth 的裸错误
			if !isToolErrorResult(toolResult) {
				session.resetToolFailure(tc.Name)
				continue
			}
			fails := session.incrToolFailure(tc.Name)
			if fails > p.effectiveToolRetryLimit() {
				msg := buildRetryAbortMessage(tc.Name, fails, toolResult)
				p.sm.AppendMessage(session, Message{Role: RoleUser, Content: msg})
				return &ChatResult{Text: msg, Attachments: cs.capturedAttachments}, nil
			}
			if fails >= 2 {
				p.sm.AppendMessage(session, buildReflectionMessage(tc.Name, fails, toolResult))
			}
		}

		// 重规划指令在全部工具结果之后追加——assistant(tool_calls) 必须紧接
		// tool 消息（API 约束），指令若插在二者之间会使消息序列非法被拒绝。
		if replanMsg != nil {
			p.sm.AppendMessage(session, *replanMsg)
		}
	}

	return &ChatResult{Text: cs.capturedText}, fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// recordToolTrace 记录一次工具调用的追踪信息（耗时、参数摘要、失败标记）。
func (p *Plugin) recordToolTrace(session *Session, tc ToolCall, start time.Time, result string) {
	entry := ToolTraceEntry{
		Time:     start,
		ToolName: tc.Name,
		Args:     truncateRunes(summarizeArgs(tc.Arguments), 80),
		Duration: time.Since(start),
	}
	if isToolErrorResult(result) {
		entry.Err = truncateRunes(result, 120)
	}
	session.appendToolTrace(entry)
}

// toolExecResult 单个工具调用的并行执行结果。
type toolExecResult struct {
	// result 工具执行结果文本。
	result string
	// skipped 中断抢占后未启动的工具调用（不入历史、不计数）。
	skipped bool
}

// executeToolCallsParallel 并行执行一轮的全部工具调用（tool_parallel 并发度）。
// 结果按原始顺序返回；审批/执行/追踪相互独立；真实命令执行经 realCmdMu 串行化
// （syncer 非线程安全）。并发度 1 时退化为顺序执行（行为与旧版一致）。
func (p *Plugin) executeToolCallsParallel(ctx *eventctx.Context, session *Session, cs *captureSender, budget *sendBudget, calls []ToolCall) []toolExecResult {
	results := make([]toolExecResult, len(calls))
	parallel := p.cfg.ToolParallel
	if parallel <= 1 || len(calls) <= 1 {
		for i := range calls {
			results[i] = p.execOneTool(ctx, session, cs, budget, calls[i])
		}
		return results
	}
	if parallel > len(calls) {
		parallel = len(calls)
	}

	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i := range calls {
		if session.Interrupted() {
			// 抢占信号到达：未启动的工具调用直接跳过。
			results[i] = toolExecResult{skipped: true}
			continue
		}
		wg.Add(1)
		go func(i int, tc ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.execOneTool(ctx, session, cs, budget, tc)
		}(i, calls[i])
	}
	wg.Wait()
	return results
}

// execOneTool 执行单个工具调用（计数 + 权限 + 审批 + 执行 + 追踪）。
func (p *Plugin) execOneTool(ctx *eventctx.Context, session *Session, cs *captureSender, budget *sendBudget, tc ToolCall) toolExecResult {
	session.Lock()
	session.ToolCount++
	session.Unlock()

	// 审批前先做 RBAC 权限校验（工具声明 Permissions 时），避免
	// "先请求审批后告知无权"的坏体验；executeTool 内的校验保留为
	// 纵深防御（双保险）。
	if tool, ok := p.reg.Get(tc.Name); ok && len(tool.Permissions) > 0 && !p.hasToolPermission(ctx, tool.Permissions) {
		return toolExecResult{result: fmt.Sprintf("错误: 工具 %q 需要权限（%s），当前用户无权调用",
			tc.Name, strings.Join(tool.Permissions, ", "))}
	}

	// 命令执行审批：根据生效的审批模式（全局 tool_approval 或群策略
	// approval）决定是否需要用户批准后才执行工具。
	// 拒绝或超时时返回工具级结果（不中断整个对话）。
	needApproval := p.approvalModeFor(ctx, tc.Name)
	if needApproval {
		approved := p.requestApproval(ctx, tc.Name, p.approvalSummaryForTool(ctx, tc), p.effectiveApprovalTimeout())
		if !approved {
			return toolExecResult{result: fmt.Sprintf("工具 `%s` 已被用户拒绝执行（审批未通过）", tc.Name)}
		}
	}

	toolCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.ToolTimeout)
	defer cancel()
	toolCtx = WithPlanSession(toolCtx, session)
	// SendTo 能力仅在本次调用通过审批门后注入（sendToAllowed）；
	// 嵌套 Skill 工具调用继承同一 context，无法绕过审批。
	sender := &loopToolSender{ctx: ctx, p: p, sendToAllowed: needApproval, budget: budget}
	traceStart := time.Now()
	toolResult := p.executeTool(ctx, tc, toolCtx, cs, sender)
	p.recordToolTrace(session, tc, traceStart, toolResult)
	return toolExecResult{result: toolResult}
}

// lastUserIsReplan 判断会话最后一条用户消息是否已是重规划指令（防重复追加）。
func lastUserIsReplan(session *Session) bool {
	msgs := session.SnapshotMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return strings.HasPrefix(msgs[i].Content, "计划步骤")
		}
	}
	return false
}

// approvalSummaryForTool 生成工具审批展示用的参数摘要。
// send_to 在审批前预解析目标，审批消息中显示解析后的目标
// （如 张三（12345）），让批准者明确知道消息将发送给谁；
// 解析失败时回退原始参数摘要。
func (p *Plugin) approvalSummaryForTool(ctx *eventctx.Context, tc ToolCall) string {
	summary := summarizeArgs(tc.Arguments)
	if tc.Name != sendToToolName {
		return summary
	}
	raw, _ := tc.Arguments["target"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return summary
	}
	isGroup := false
	if v, ok := tc.Arguments["is_group"].(bool); ok {
		isGroup = v
	}
	sender := &loopToolSender{ctx: ctx, p: p}
	previewCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel()
	if _, display, err := sender.resolveTarget(previewCtx, raw, isGroup); err == nil {
		args := make(map[string]any, len(tc.Arguments))
		maps.Copy(args, tc.Arguments)
		args["target"] = display
		return summarizeArgs(args)
	}
	return summary
}

// summarizeArgs 生成工具参数摘要（按键排序、截断，用于审批展示，避免敏感信息泄漏）。
func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k == "arguments" {
			continue // 真实命令的原始参数串单独展示
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s := fmt.Sprintf("%v", args[k])
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return strings.Join(parts, ", ")
}

// approvalModeFor 判断指定工具是否需要审批（按生效的审批模式）。
func (p *Plugin) approvalModeFor(ctx *eventctx.Context, toolName string) bool {
	mode := p.effectiveApprovalMode(ctx)
	if mode == "" || mode == string(ApprovalOff) {
		// AlwaysRequireApproval 工具（如 send_to）不受 off 模式豁免，强制审批。
		if tool, ok := p.reg.Get(toolName); ok && tool.AlwaysRequireApproval {
			return true
		}
		return false
	}
	tool, ok := p.reg.Get(toolName)
	if !ok {
		// 工具不存在（如 Skill）时：always 模式审批，restricted 不审批
		return mode == string(ApprovalAlways)
	}
	return p.needsApproval(tool, mode)
}

// effectiveApprovalMode 返回生效的审批模式：群策略 > 全局配置 > 默认 off。
func (p *Plugin) effectiveApprovalMode(ctx *eventctx.Context) string {
	if gp := p.groupPolicyFor(ctx); gp != nil {
		if m := gp.EffectiveApproval(); m != "" {
			return m
		}
	}
	if p.cfg.ToolApproval != "" {
		return p.cfg.ToolApproval
	}
	return string(ApprovalOff)
}

// effectiveApprovalTimeout 返回生效的审批超时（群策略未配置时用全局）。
func (p *Plugin) effectiveApprovalTimeout() time.Duration {
	t := p.cfg.ApprovalTimeout
	if t <= 0 {
		t = 60 * time.Second
	}
	return t
}

// effectiveToolRetryLimit 返回工具失败重试预算（<=0 时用默认值 2）。
func (p *Plugin) effectiveToolRetryLimit() int {
	if p.cfg.ToolRetryLimit <= 0 {
		return 2
	}
	return p.cfg.ToolRetryLimit
}

// effectiveTurnTimeout 返回一次 AI 处理的独立时间预算。
// turn_timeout 未配置时自动推导：api_timeout × max(2, min(max_depth, 5))，
// 既为多轮工具任务留足余量，又避免 max_depth 过大时预算失控。
func (p *Plugin) effectiveTurnTimeout() time.Duration {
	if p.cfg.TurnTimeout > 0 {
		return p.cfg.TurnTimeout
	}
	api := p.cfg.APITimeout
	if api <= 0 {
		api = 60 * time.Second
	}
	depth := p.cfg.MaxDepth
	if depth <= 0 {
		depth = 5
	}
	return api * time.Duration(max(2, min(depth, 5)))
}

// liftEventDeadline 以插件独立预算替换事件上下文的 deadline。
// context.WithoutCancel 去掉全局 Timeout 中间件注入的 deadline 与取消信号
// （保留 values/tracing），再套上 effectiveTurnTimeout 的新预算。
// 返回恢复函数（恢复原 stdCtx）。
func (p *Plugin) liftEventDeadline(ctx *eventctx.Context) func() {
	orig := ctx.Context()
	turnCtx, cancel := context.WithTimeout(context.WithoutCancel(orig), p.effectiveTurnTimeout())
	ctx.SetStdContext(turnCtx)
	return func() {
		cancel()
		ctx.SetStdContext(orig)
	}
}

// groupPolicyFor 返回当前会话生效的群策略（仅群聊；私聊返回 nil 表示不受群策略约束）。
func (p *Plugin) groupPolicyFor(ctx *eventctx.Context) *GroupPolicy {
	if p.groupPolicies == nil {
		return nil
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		return nil
	}
	return p.groupPolicies.Effective(chat.ID)
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

// toolResultMissing 工具结果缺失时的占位回填文本（用于修复消息序列）。
const toolResultMissing = "（工具结果缺失，消息序列已修复）"

// repairToolCallSequence 修复消息序列中的工具调用完整性：
// 每条含 tool_calls 的 assistant 消息之后，必须为其中每个 tool_call_id
// 提供对应的 tool 消息（OpenAI/Anthropic API 硬性约束，缺失即 400）。
//
// 中断抢占跳过的工具、进程异常退出、持久化损坏或上下文裁剪
// 都可能导致序列残缺，这里双向自愈：
//   - 缺失的 tool 响应：插入占位 tool 消息（tool 消息必须紧跟在
//     assistant 之后，因此在遇到下一条非 tool 消息或序列末尾时补位）
//   - 孤儿的 tool 消息：前置 assistant(tool_calls) 被裁掉后残留的
//     tool 消息没有可响应的 tool_call_id，API 同样会以 400 拒绝，直接丢弃
//
// 返回新的消息切片，不修改原切片。
func repairToolCallSequence(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs)+4)
	var pending []string // 尚未响应的 tool_call_id
	for _, m := range msgs {
		if m.Role == RoleTool {
			// 只保留响应当前 assistant(tool_calls) 组的 tool 消息；
			// 找不到对应 tool_call_id 即为孤儿消息（如上下文裁剪切掉了
			// 前置 assistant），丢弃以免整个请求被 API 以 400 拒绝。
			idx := -1
			for i, id := range pending {
				if id == m.ToolCallID {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			pending = append(pending[:idx], pending[idx+1:]...)
			out = append(out, m)
			continue
		}

		if len(pending) > 0 {
			// 非 tool 消息出现：先把前面 assistant 缺失的工具响应补位插回
			// （插在本消息之前，确保 tool 消息紧随 assistant）。
			for _, id := range pending {
				out = append(out, Message{Role: RoleTool, Content: toolResultMissing, ToolCallID: id})
			}
			pending = nil
		}
		out = append(out, m)

		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			pending = nil
			seen := make(map[string]bool, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || seen[tc.ID] {
					continue
				}
				seen[tc.ID] = true
				pending = append(pending, tc.ID)
			}
		}
	}
	// 序列末尾仍未响应的 tool_call_id 补位
	for _, id := range pending {
		out = append(out, Message{Role: RoleTool, Content: toolResultMissing, ToolCallID: id})
	}
	return out
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
