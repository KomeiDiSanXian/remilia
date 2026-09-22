// Package ai process.go — 回合 LLM 调用循环与工具回填。
//
// 本文件包含：
//   - processWithTools: 主工具调用循环（流式 LLM 调用 + 工具执行 + 回填）
//   - execOneTool: 单个工具调用的策略评估与执行装配
//
// 单轮调用、消息载荷规整、错误文案、并行编排与运行预算的实现见 builtin/ai/runtime；
// 本文件只保留依赖会话、配置与执行路径的编排。
//
// processWithTools 是整个 AI 插件的核心编排逻辑，
// 在工具调用循环中交替调用 LLM 和执行工具，直至达到最大深度或无工具调用。
package ai

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ChatResult AI 对话的最终回复结果，包含文字和附件。
type ChatResult struct {
	Text        string
	Attachments []platform.Attachment
}

// runtimeClient 组装单轮非流式 LLM 调用客户端（请求形状与调用见 builtin/ai/runtime）。
func (p *Plugin) runtimeClient() runtime.Client {
	return runtime.Client{Cfg: p.cfg, Prov: p.prov}
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
func (p *Plugin) processWithTools(ctx *eventctx.Context, session *session.Session) (*ChatResult, error) {
	currentDepth := 0
	maxDepth := p.cfg.MaxDepth

	cs := &execution.CaptureSender{}
	// 模型直接输出的附件（原生图像输出等多模态响应）跨轮次累积，
	// 最终与工具捕获的附件合并后随回复发送。
	provAttachments := make([]platform.Attachment, 0, 4)
	// 消息发送预算：一次运行内 send_message/send_to 的总发送次数上限，
	// 跨工具调用共享（含并行路径），超限报错回填给模型。
	budget := &sendBudget{limit: p.cfg.MaxSendsPerRound}

	// 全局 Timeout 中间件（cmd/bot 默认 30s）会给事件上下文注入单次
	// deadline，多轮工具循环（每轮 LLM 调用 + 工具执行）共享同一 deadline
	// 时，长任务会在中途被整段切断（如 send_message 分步任务第二轮超时）。
	// 这里以插件独立预算替换（turn_timeout，见 effectiveTurnTimeout）。
	restoreDeadline := runtime.LiftEventDeadline(ctx, runtime.EffectiveTurnTimeout(p.cfg))
	defer restoreDeadline()

	// 回合中断感知上下文：RequestInterrupt（/ai stop 命令、用户新消息抢占）
	// 会取消该上下文，从而中止进行中的 LLM 流请求，使"停止生成"对单轮流
	// 同样生效（否则中断只在工具轮之间的检查点生效，单轮流需等流自然结束）。
	turnCtx, cancelTurnCtx := session.TurnCtx(ctx.Context())
	defer cancelTurnCtx()

	// 动作决策：候选发现（注册表 + 用户 Skill，经群策略与 RBAC）→ 选择
	// （工具较多时按当前用户消息本地检索 Top-K，替代旧的 LLM 单分类路由）
	// → 稳定策略。见 decision.go。
	activeTools := p.decideTurnActions(ctx, session, p.needAction())

	// 动态上下文（运行时/群聊窗口/长期记忆/相关历史）逐轮变化，统一挂到本轮
	// 用户消息尾部发送，不写进 System 消息（见 buildDynamicContext）。
	// 一个回合内只构建一次并复用：同一回合的各次请求（工具轮）因此拥有
	// 字节一致的前缀，本回合生成的工具结果能继续被前缀缓存复用。
	dynamicContext := p.buildDynamicContext(ctx, session)

	for currentDepth < maxDepth {
		currentDepth++

		// 中断检查点：用户新消息抢占时，未开始的轮次直接收尾。
		if session.Interrupted() {
			return &ChatResult{Text: cs.CapturedText, Attachments: provAttachments}, nil
		}

		session.Lock()
		session.CallCount++
		msgs := make([]protocol.Message, len(session.Messages))
		copy(msgs, session.Messages)
		session.Unlock()

		// 附件保留策略：当前轮附件始终保留；历史图片在保留窗口（最近 N 条
		// user 消息 + 时间窗）内随请求发送，支持"继续追问图片细节"；
		// 更早/更老的图片降级为文本占位，避免无限上传（内存与 token 浪费）。
		msgs, budgetDropped, _ := runtime.PrepareRequestMessages(msgs, runtime.Retention{
			MaxTurns:      p.cfg.ImageContextTurns,
			Window:        p.cfg.ImageContextWindow,
			MaxPerRequest: p.cfg.MaxImagesPerRequest,
		})
		if budgetDropped > 0 && session.MarkImageOverflowNotified() {
			logger.Warnf("[AI] %d historical image(s) dropped over budget (max_images_per_request=%d)",
				budgetDropped, p.cfg.MaxImagesPerRequest)
			ctx.ReplyText(fmt.Sprintf("本次对话图片较多，已保留最近 %d 张，更早的图片不再显示",
				p.cfg.MaxImagesPerRequest))
		}

		// 兜底修复工具调用序列：中断跳过的工具、进程异常退出或持久化损坏
		// 都可能让 assistant(tool_calls) 缺少对应 tool 消息（OpenAI/Anthropic
		// API 硬性约束，缺失即 400）。按需补占位 tool 消息，自愈会话历史。
		msgs = runtime.RepairToolCallSequence(msgs)
		msgs = runtime.InjectDynamicContext(msgs, dynamicContext)

		// 计划注入：存在进行中的任务计划时，把"当前计划+进度"作为 system 消息
		// 附在消息序列末尾（而不是插到系统提示词之后），让模型有状态可依地
		// 推进多步任务，同时不使已完成的历史（含本回合的工具结果）失去缓存
		// 复用能力。
		if text := session.PlanText(); text != "" {
			msgs = append(msgs, protocol.Message{Role: protocol.RoleSystem, Content: "===== 当前执行计划 =====\n" + text})
		}

		req := &protocol.ChatRequest{
			Model:       p.cfg.Model,
			Messages:    msgs,
			Tools:       wireSpecs(activeTools),
			Temperature: p.cfg.Temperature,
			TopP:        p.cfg.TopP,
			MaxTokens:   p.cfg.MaxTokens,
		}

		streamCtx, cancel := context.WithTimeout(turnCtx, p.cfg.APITimeout)
		streamCh, err := p.prov.ChatStream(streamCtx, req)
		if err != nil {
			cancel()
			if session.Interrupted() {
				// 主动停止：流尚未产生任何内容，按已捕获内容收尾，不报错。
				return &ChatResult{Text: cs.CapturedText, Attachments: provAttachments}, nil
			}
			return &ChatResult{Text: cs.CapturedText, Attachments: provAttachments}, fmt.Errorf("chat stream: %w", err)
		}

		var fullResponse strings.Builder
		var toolCalls []protocol.ToolCall
		var streamErr error
		doneReceived := false

		for event := range streamCh {
			switch event.Type {
			case protocol.StreamEventText:
				fullResponse.WriteString(event.Content)
			case protocol.StreamEventToolCall:
				if event.ToolCall != nil && event.ToolCall.Name != "" {
					toolCalls = append(toolCalls, *event.ToolCall)
				}
			case protocol.StreamEventAttachment:
				if event.Attachment != nil {
					provAttachments = append(provAttachments, *event.Attachment)
				}
			case protocol.StreamEventError:
				// 部分 provider（如 Anthropic）在流被取消时会以错误事件收尾：
				// 是否主动停止在收尾处按 Interrupted 判定，此处仅记录。
				streamErr = event.Err
			case protocol.StreamEventDone:
				doneReceived = true
			}
		}
		cancel()

		responseText := fullResponse.String()

		// 主动停止收尾：流因中断被取消（未收到 [DONE]，或 provider 以错误事件
		// 收尾）。把已到手部分记入会话并作为最终回复返回，不视为错误——停止
		// 由 /ai stop 命令或用户新消息抢占触发，二者语义一致。
		if session.Interrupted() && (!doneReceived || streamErr != nil) {
			if responseText != "" {
				p.sm.AppendMessage(session, protocol.Message{Role: protocol.RoleAssistant, Content: responseText})
			}
			text := responseText
			if text == "" {
				text = cs.CapturedText
			}
			return &ChatResult{Text: text, Attachments: runtime.MergeChatAttachments(cs.CapturedAttachments, provAttachments)}, nil
		}
		if streamErr != nil {
			return &ChatResult{Text: cs.CapturedText}, streamErr
		}

		for i := range toolCalls {
			if toolCalls[i].ID == "" {
				toolCalls[i].ID = fmt.Sprintf("call_%s_%d", toolCalls[i].Name, i)
			}
		}

		// 重规划闭环：前序步骤全部终态、自身失败的前沿步骤 → 自动追加
		// 重规划指令（要求模型调整计划而非按旧计划继续），替代"软计划"
		// 依赖模型自觉的现状。同一条指令不重复追加。
		var replanMsg *protocol.Message
		if plan := session.PlanSnapshot(); plan != nil && plan.Active {
			if f := plan.FirstFailedFrontier(); f != nil && !runtime.LastUserIsReplan(session) {
				m := runtime.BuildReplanMessage(f)
				replanMsg = &m
			}
		}

		if len(toolCalls) == 0 {
			if responseText != "" {
				p.sm.AppendMessage(session, protocol.Message{Role: protocol.RoleAssistant, Content: responseText})
			}
			if replanMsg != nil {
				p.sm.AppendMessage(session, *replanMsg)
			}
			return &ChatResult{
				Text:        responseText,
				Attachments: runtime.MergeChatAttachments(cs.CapturedAttachments, provAttachments),
			}, nil
		}

		p.sm.AppendMessage(session, protocol.Message{Role: protocol.RoleAssistant, Content: responseText, ToolCalls: toolCalls})

		// 并行执行本轮全部工具调用（tool_parallel 控制并发度，默认 4；
		// 审批/执行/追踪各自独立，结果按原始顺序回填）。
		results := runtime.ExecuteToolCallsParallel(toolCalls, p.cfg.ToolParallel, session.Interrupted,
			func(_ int, tc protocol.ToolCall) runtime.ToolExecResult {
				return p.execOneTool(ctx, session, cs, budget, tc)
			})

		for i, tc := range toolCalls {
			// 中断抢占后未启动的工具调用：补占位 tool 消息，保证 assistant
			// 的每个 tool_call_id 都有对应 tool 响应（API 硬性约束，
			// 缺失会导致下次请求被 400 拒绝）。
			if results[i].Skipped {
				p.sm.AppendMessage(session, protocol.Message{
					Role:       protocol.RoleTool,
					Content:    "（工具未执行：对话被新消息打断）",
					ToolCallID: tc.ID,
				})
				continue
			}
			toolResult := results[i].Result
			p.sm.AppendMessage(session, protocol.Message{
				Role:       protocol.RoleTool,
				Content:    runtime.TruncateToolResult(toolResult),
				ToolCallID: tc.ID,
			})

			// 计划创建时同步展示给用户（chat 内可见计划，后续步骤更新不打扰）。
			// 发生在进行中的回合：QQ 单聊/群聊（频道除外）Markdown 场景附带
			// "查看计划/停止生成"指令按钮（见 maybeAttachQQPlanButtons），长任务
			// 期间可一键刷新进度或中断。
			if tc.Name == catalog.PlanCreateToolName && results[i].Err == nil {
				msg := p.formatReplyMessage(toolResult)
				p.replyAndRecord(ctx, p.maybeAttachQQPlanButtons(ctx, msg, true))
			}

			// 失败重试预算与反思引导：
			//   - 失败结果已回填（模型天然可重试）
			//   - 同一工具连续失败第 2 次起，追加"反思指令"用户消息，
			//     强制模型先分析原因再采用不同策略（显式反思轮）
			//   - 连续失败达到 tool_retry_limit+1 次时优雅中止本轮，
			//     替代撞 max_depth 的裸错误
			if results[i].Err == nil {
				session.ResetToolFailure(tc.Name)
				continue
			}
			fails := session.IncrToolFailure(tc.Name)
			if fails > runtime.EffectiveToolRetryLimit(p.cfg) {
				msg := execution.BuildRetryAbortMessage(tc.Name, fails, toolResult)
				p.sm.AppendMessage(session, protocol.Message{Role: protocol.RoleUser, Content: msg})
				return &ChatResult{Text: msg, Attachments: runtime.MergeChatAttachments(cs.CapturedAttachments, provAttachments)}, nil
			}
			if fails >= 2 {
				p.sm.AppendMessage(session, execution.BuildReflectionMessage(tc.Name, fails, toolResult))
			}
		}

		// 重规划指令在全部工具结果之后追加——assistant(tool_calls) 必须紧接
		// tool 消息（API 约束），指令若插在二者之间会使消息序列非法被拒绝。
		if replanMsg != nil {
			p.sm.AppendMessage(session, *replanMsg)
		}
	}

	return &ChatResult{Text: cs.CapturedText, Attachments: runtime.MergeChatAttachments(cs.CapturedAttachments, provAttachments)},
		fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// execOneTool 执行单个工具调用（计数 + 权限 + 审批 + 执行 + 追踪）。
func (p *Plugin) execOneTool(ctx *eventctx.Context, session *session.Session, cs *execution.CaptureSender, budget *sendBudget, tc protocol.ToolCall) runtime.ToolExecResult {
	session.Lock()
	session.ToolCount++
	session.Unlock()

	// 策略评估（RBAC 权限 + 审批）由决策层给出结论：放行、拒绝文案与 SendTo 授权。
	// 拒绝按工具级结果返回（不中断整个对话）。
	verdict := p.decideToolInvocation(ctx, tc)
	if !verdict.allowed {
		return runtime.ToolExecResult{Result: verdict.rejectText, Err: verdict.err}
	}

	toolCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.ToolTimeout)
	defer cancel()
	toolCtx = runtime.WithPlanSession(toolCtx, session)
	// SendTo 能力仅在本次调用通过审批门后注入（sendToAllowed）；
	// 嵌套 Skill 工具调用继承同一 context，无法绕过审批。
	sender := &loopToolSender{ctx: ctx, p: p, sendToAllowed: verdict.sendToGranted, budget: budget}
	traceStart := time.Now()
	res := p.executeToolResult(ctx, tc, toolCtx, cs, sender)
	runtime.RecordToolTrace(session, tc, traceStart, res.Text, res.Err)
	return runtime.ToolExecResult{Result: res.Text, Err: res.Err}
}

// approvalSummaryForTool 生成工具审批展示用的参数摘要。
// send_to 在审批前预解析目标，审批消息中显示解析后的目标
// （如 张三（12345）），让批准者明确知道消息将发送给谁；
// 解析失败时回退原始参数摘要。
func (p *Plugin) approvalSummaryForTool(ctx *eventctx.Context, tc protocol.ToolCall) string {
	summary := runtime.SummarizeArgs(tc.Arguments)
	if tc.Name != catalog.SendToToolName {
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
		return runtime.SummarizeArgs(args)
	}
	return summary
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

// groupPolicyFor 返回当前会话生效的群策略（仅群聊；私聊返回 nil 表示不受群策略约束）。
func (a *adminState) groupPolicyFor(ctx *eventctx.Context) *GroupPolicy {
	if a.groupPolicies == nil {
		return nil
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		return nil
	}
	return a.groupPolicies.Effective(chat.ID)
}

// userSkillActions 构建当前会话用户的已启用 Skill 动作（模型可见视图）。
//
// 只产出描述与策略：Skill 的执行载荷由 skillReg 在调用时按名称解析
// （见 executeToolResult 的 skillInvoker 分支），因此这里不带 Execute。
func (c *catalogState) userSkillActions(userID string) []toolkit.Action {
	skills := c.skillReg.ListByOwner(userID)
	if len(skills) == 0 {
		return nil
	}
	actions := make([]toolkit.Action, 0, len(skills))
	for _, s := range skills {
		if !s.Enabled {
			continue
		}
		actions = append(actions, toolkit.ActionOf(toolkit.Tool{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.Parameters,
		}))
	}
	return actions
}

// wireSpecs 把动作收敛为协议层工具声明：只保留模型需要知道的字段。
func wireSpecs(actions []toolkit.Action) []protocol.ToolSpec {
	out := make([]protocol.ToolSpec, 0, len(actions))
	for _, a := range actions {
		out = append(out, protocol.ToolSpec{
			Name:        a.Spec.Name,
			Description: a.Spec.Description,
			Parameters:  a.Spec.Parameters,
		})
	}
	return out
}
