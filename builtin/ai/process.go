// process.go — 回合 LLM 调用循环与工具回填。
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

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// chatResult AI 对话的最终回复结果，包含文字和附件。
type chatResult struct {
	Text        string
	Attachments []platform.Attachment
}

// runtimeClient 组装单轮非流式 LLM 调用客户端（请求形状与调用见 builtin/ai/runtime）。
// kind 标注调用用途（skill/verify/extract），只影响指标与日志，不改变请求内容。
func (p *Plugin) runtimeClient(kind protocol.CallKind) runtime.Client {
	return runtime.Client{
		Temperature:     p.cfg.Temperature,
		TopP:            p.cfg.TopP,
		MaxTokens:       p.cfg.MaxTokens,
		ReasoningEffort: p.cfg.ReasoningEffort,
		Kind:            kind,
		Prov:            p.prov,
	}
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
//
// 单轮内的"读流"与"收尾策略"分开实现：事件归集见 consumeTurnStream，
// 请求组装见 buildTurnRequest，工具结果回填见 recordToolResults。
func (p *Plugin) processWithTools(ctx *eventctx.Context, sess *session.Session) (*chatResult, error) {
	// 回合内会追加多条中间消息（assistant/tool/反思/重规划），统一走
	// AppendMessageNoPersist，回合结束（含提前 return / 出错 / 中断）在这里
	// 一次性落库：把每条工具结果一次整记录 upsert 降为每回合一次，消除写放大。
	// 用户消息等跨回合的即时持久化仍由 handler / generateVerified 负责。
	defer p.sm.SaveSession(sess)
	// 回合结束回收"本来就不会再随请求发送"的历史图片/音频字节：
	// 请求构建只保留 image_context_window 内的附件（runtime.PrepareRequestMessages），
	// 更早的二进制留在会话里只是白占内存。降级后的文本与请求侧逐字节一致
	// （共用 session.StripBinaryParts），不会额外打断前缀缓存。
	defer p.releaseStaleAttachmentBytes(sess)

	maxDepth := p.cfg.MaxDepth
	// 回合级可变状态：会话、命令输出捕获器、跨轮累积的模型输出附件、
	// 当前输出预算。它们同时被多个收尾步骤读写，因此收进 turnRun（见
	// process_steps.go），而不是作为长参数列表逐个传递。
	run := &turnRun{
		p:    p,
		sess: sess,
		cs:   &execution.CaptureSender{},
		// 模型直接输出的附件（原生图像输出等多模态响应）跨轮次累积，
		// 最终与工具捕获的附件合并后随回复发送。
		provAttachments: make([]platform.Attachment, 0, 4),
		maxTokens:       p.cfg.MaxTokens,
	}
	// 消息发送预算：一次运行内 send_message/send_to 的总发送次数上限，
	// 跨工具调用共享（含并行路径），超限报错回填给模型。
	budget := &sendBudget{limit: p.cfg.MaxSendsPerRound}

	// 全局 Timeout 中间件（cmd/bot 默认 30s）会给事件上下文注入单次
	// deadline，多轮工具循环（每轮 LLM 调用 + 工具执行）共享同一 deadline
	// 时，长任务会在中途被整段切断（如 send_message 分步任务第二轮超时）。
	// 这里以插件独立预算替换（turn_timeout，见 effectiveTurnTimeout）。
	restoreDeadline := runtime.LiftEventDeadline(ctx, runtime.EffectiveTurnTimeout(runtimeLimits(p.cfg)))
	defer restoreDeadline()

	// 回合中断感知上下文：RequestInterrupt（/ai stop 命令、用户新消息抢占）
	// 会取消该上下文，从而中止进行中的 LLM 流请求，使"停止生成"对单轮流
	// 同样生效（否则中断只在工具轮之间的检查点生效，单轮流需等流自然结束）。
	turnCtx, cancelTurnCtx := sess.TurnCtx(ctx.Context())
	defer cancelTurnCtx()
	// 标注调用用途与会话范围：ai_llm_tokens_total 的 kind 标签据此区分
	// "对话回合"与辅助调用，单次调用日志则带上会话 ID，便于按会话还原
	// 前缀缓存未命中的轮次（见 metrics.go）。
	turnCtx = protocol.WithCallKind(protocol.WithCallScope(turnCtx, sess.ID), protocol.CallKindChat)

	// 动作决策：候选发现（注册表 + 用户 Skill，经群策略与 RBAC）→ 选择
	// （工具较多时按当前用户消息本地检索 Top-K，替代旧的 LLM 单分类路由）
	// → 稳定策略。见 decision.go。
	activeTools := p.decideTurnActions(ctx, sess, p.needAction(ctx, sess))

	// 动态上下文（运行时/群聊窗口/长期记忆/相关历史）逐轮变化，统一挂到本轮
	// 用户消息尾部发送，不写进 System 消息（见 buildDynamicContext）。
	// 一个回合内只构建一次并复用：同一回合的各次请求（工具轮）因此拥有
	// 字节一致的前缀，本回合生成的工具结果能继续被前缀缓存复用。
	dynamicContext := p.buildDynamicContext(ctx, sess)

	// 每个回合最多提升一次预算，避免反复重试放大思考成本。
	budgetEscalated := false

	for currentDepth := 1; currentDepth <= maxDepth; currentDepth++ {
		// 中断检查点：用户新消息抢占时，未开始的轮次直接收尾。
		if sess.Interrupted() {
			return &chatResult{Text: run.capturedText(), Attachments: run.provAttachments}, nil
		}

		sess.Lock()
		sess.CallCount++
		sess.Unlock()
		req := p.buildTurnRequest(ctx, run, turnCtx, activeTools, dynamicContext, currentDepth == 1)

		streamCtx, cancel := context.WithTimeout(turnCtx, p.cfg.APITimeout)
		streamCh, err := p.prov.ChatStream(streamCtx, req)
		if err != nil {
			cancel()
			if sess.Interrupted() {
				// 主动停止：流尚未产生任何内容，按已捕获内容收尾，不报错。
				return &chatResult{Text: run.capturedText(), Attachments: run.provAttachments}, nil
			}
			return &chatResult{Text: run.capturedText(), Attachments: run.provAttachments}, fmt.Errorf("chat stream: %w", err)
		}

		outcome := consumeTurnStream(streamCh)
		cancel()
		run.provAttachments = append(run.provAttachments, outcome.attachments...)

		// 主动停止收尾：流因中断被取消（未收到结束事件，或 provider 以错误
		// 事件收尾）。把已到手部分记入会话并作为最终回复返回，不视为错误——
		// 停止由 /ai stop 命令或用户新消息抢占触发，二者语义一致。
		if result, done := run.closeOnInterrupt(outcome); done {
			return result, nil
		}
		if outcome.err != nil {
			return &chatResult{Text: run.capturedText()}, outcome.err
		}
		// 未被抢占却收不到结束事件：传输被中断/取消。部分 provider（如
		// OpenAI 兼容实现）在 ctx 取消或连接中断时直接关闭 channel，不补发
		// 错误事件（见 protocol/provider_openai.go 的 sendEvent）。
		// 已有可见产出（正文片段 / 发送工具已发出的内容或附件）时按已到手部分
		// 收尾，不丢弃；完全没有任何产出时这不是"模型正常返回空"，必须报错——
		// 否则整回合会静默收尾，用户看不到任何回复，日志也停在调用之前
		// （正是"没有后续生成/发送记录"的现象）。
		if result, done := run.closeWithoutDone(outcome); done {
			return result.result, result.err
		}

		// 输出预算被思考过程耗尽：思考型模型还没轮到输出正文就把 max_tokens
		// 用完，直接以 finish_reason=length 收尾，用户侧表现为"提问后没有任何
		// 回复"。这类截断是可恢复的——提高输出预算重试同一轮即可（消息序列
		// 尚未改动，重试等价于再发一次同样的请求）。上限由 max_tokens_cap
		// 控制，设成 <= max_tokens 即关闭该重试。
		//
		// 重试不限于首轮：工具轮之后同样可能整轮被截断，而此时回合内还没有
		// 任何可见产出（发送工具未发出内容、没有附件）。
		if !budgetEscalated {
			if next, ok := run.escalateOnTruncation(outcome); ok {
				budgetEscalated = true
				run.maxTokens = next
				continue
			}
		}

		assignToolCallIDs(outcome.toolCalls)
		replanMsg := buildReplanMessage(sess)

		if len(outcome.toolCalls) == 0 {
			return run.finishWithoutToolCalls(outcome, replanMsg, currentDepth == 1)
		}

		p.sm.AppendMessageNoPersist(sess, protocol.Message{
			Role: protocol.RoleAssistant, Content: outcome.text, ToolCalls: outcome.toolCalls,
		})

		// 并行执行本轮全部工具调用（tool_parallel 控制并发度，默认 4；
		// 审批/执行/追踪各自独立，结果按原始顺序回填）。
		results := runtime.ExecuteToolCallsParallel(outcome.toolCalls, p.cfg.ToolParallel, sess.Interrupted,
			func(_ int, tc protocol.ToolCall) runtime.ToolExecResult {
				return p.execOneTool(ctx, turnCtx, sess, run.cs, budget, tc)
			})

		if abort := run.recordToolResults(ctx, outcome.toolCalls, results); abort != nil {
			return abort, nil
		}

		// 重规划指令在全部工具结果之后追加——assistant(tool_calls) 必须紧接
		// tool 消息（API 约束），指令若插在二者之间会使消息序列非法被拒绝。
		if replanMsg != nil {
			p.sm.AppendMessageNoPersist(sess, *replanMsg)
		}
	}

	return &chatResult{Text: run.capturedText(), Attachments: run.attachments()},
		fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// countRequestImages 统计一次请求实际附带的历史图片数（多模态 ContentParts）。
func countRequestImages(msgs []protocol.Message) int {
	n := 0
	for _, m := range msgs {
		n += runtime.CountImageParts(m.ContentParts)
	}
	return n
}

// effectiveToolBudget 单个工具调用的执行预算。
// 默认 tool_timeout；工具实为技能（Skill）时取 max(tool_timeout, skill_timeout)——
// 技能内部还有自己的多轮 LLM 循环，skill_timeout（默认 60s）比 tool_timeout
// （默认 30s）更有意义；此前 skillCtx 从 toolCtx 派生，skill_timeout 配得
// 再大也会被 30s 静默截断。
func (p *Plugin) effectiveToolBudget(ctx *eventctx.Context, tc protocol.ToolCall) (d time.Duration) {
	d = p.cfg.ToolTimeout
	if _, ok := p.skillReg.GetSystem(tc.Name); ok {
		if p.cfg.SkillTimeout > d {
			d = p.cfg.SkillTimeout
		}
		return d
	}
	if skill, ok := p.skillReg.GetByOwner(ctx.GetSenderInfo().ID, tc.Name); ok && skill.Enabled {
		if p.cfg.SkillTimeout > d {
			d = p.cfg.SkillTimeout
		}
		return d
	}
	return d
}

// finishReasonLength 提供商给出的"输出被 token 上限截断"结束原因。
const finishReasonLength = "length"

// escalateMaxTokens 计算截断重试提升后的输出预算：在当前预算上翻倍，但不
// 超过 cap。cap 不大于当前预算（或当前预算非正）时原样返回，表示不重试。
func escalateMaxTokens(current, cap int) int {
	if current <= 0 || cap <= current {
		return current
	}
	next := min(current*2, cap)
	return next
}

// execOneTool 执行单个工具调用（计数 + 权限 + 审批 + 执行 + 追踪）。
//
// turnCtx 是回合中断感知上下文：工具超时与审批等待都从它派生，
// /ai stop / 用户新消息抢占对已启动的工具与审批等待同样生效
// （此前工具只用事件 ctx 的 per-tool 超时，中断无法中止在跑工具）。
func (p *Plugin) execOneTool(ctx *eventctx.Context, turnCtx context.Context, sess *session.Session, cs *execution.CaptureSender, budget *sendBudget, tc protocol.ToolCall) runtime.ToolExecResult {
	// 任务级预算：超限时终止任务并拒绝本次调用（模型只能在剩余预算内动作）。
	if reason := p.chargeTaskBudget(sess); reason != "" {
		p.terminateTaskForBudget(sess, reason)
		err := fmt.Errorf("任务预算超限：%s", reason)
		return runtime.ToolExecResult{
			Result: "错误: " + err.Error() + "；任务已终止，请拆分为更小的任务后重试",
			Err:    err,
		}
	}

	sess.Lock()
	sess.ToolCount++
	sess.Unlock()

	// 策略评估（RBAC 权限 + 审批）由决策层给出结论：放行、拒绝文案与 SendTo 授权。
	// 拒绝按工具级结果返回（不中断整个对话）。解析出的动作快照继续传给执行器，
	// Provider 在审批期间更新工具时，本次审批与执行仍针对同一份定义。
	target := p.executionToolSnapshot(tc.Name)
	verdict := p.decideToolInvocationWithSnapshot(ctx, turnCtx, tc, target)
	if !verdict.allowed {
		return runtime.ToolExecResult{Result: verdict.rejectText, Err: verdict.err}
	}

	toolCtx, cancel := context.WithTimeout(turnCtx, p.effectiveToolBudget(ctx, tc))
	defer cancel()
	// SendTo 能力仅在本次调用通过审批门后注入（sendToAllowed）；
	// 嵌套 Skill 工具调用继承同一份 Invocation，无法绕过审批。
	sender := &loopToolSender{ctx: ctx, p: p, sendToAllowed: verdict.sendToGranted, budget: budget}
	// 计划端口由装配侧直接计算并随 Invocation 传入：runtime 只承载与具体动作
	// 无关的机制，不反向依赖内置动作包（catalog）。
	inv := p.newToolInvocation(ctx, sender, p.planAccessFor(ctx, sess))
	traceStart := time.Now()
	res := p.executeToolResultWithSnapshot(ctx, tc, executionTarget{Ctx: toolCtx, CS: cs, Inv: inv}, target)
	runtime.RecordToolTrace(sess, tc, traceStart, res.Text, res.Err)
	recordToolExecution(tc.Name, time.Since(traceStart), res.Err)
	return runtime.ToolExecResult{Result: res.Text, Err: res.Err}
}

// approvalSummaryForAction 生成工具审批展示用的参数摘要。
//
// 声明了 GrantsSendTo 的动作（会向其他会话推送，如 send_to）在审批前预解析目标，
// 审批消息中显示解析后的目标（如 张三（12345）），让批准者明确知道消息将发送给
// 谁——判定依据是动作的策略声明，不是写死的动作名。未声明者直接返回参数摘要，
// 解析失败也回退摘要。
//
// action/ok 由调用方从执行快照取得：与权限判定、真正执行必须看到同一份定义。
func (p *Plugin) approvalSummaryForAction(ctx *eventctx.Context, tc protocol.ToolCall, action toolkit.Action, ok bool) string {
	summary := runtime.SummarizeArgs(tc.Arguments)
	if !ok || !action.Policy.GrantsSendTo {
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
	if _, display, err := sender.ResolveTarget(previewCtx, raw, isGroup); err == nil {
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
	return string(approvalOff)
}

// groupPolicyFor 返回当前会话生效的群策略（仅群聊；私聊返回 nil 表示不受群策略约束）。
func (a *adminState) groupPolicyFor(ctx *eventctx.Context) *groupPolicy {
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
