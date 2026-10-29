// process_steps.go — processWithTools 的子步骤。
//
// 主循环只保留回合级编排（预算、中断、深度控制），单轮内的事件归集、
// 请求组装、工具回填与空回复诊断各自成函数，便于单独推理与测试。
package ai

import (
	"context"
	"fmt"
	"strings"

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

// streamOutcome 一轮流式调用的归集结果。
//
// 它只承载"从流里读到了什么"，不含任何收尾策略：是否算错误、要不要重试、
// 如何组装最终回复都由调用方按上下文判定（见 processWithTools）。
type streamOutcome struct {
	text            string
	toolCalls       []protocol.ToolCall
	attachments     []platform.Attachment
	err             error
	doneReceived    bool
	finishReason    string
	reasoningChars  int
	reasoningTokens int
}

// turnRun 一次回合运行的可变状态，也是回合内各收尾步骤的接收者。
//
// 这些字段（会话、命令输出捕获器、跨轮累积的模型输出附件、当前输出预算）
// 在 processWithTools 的循环里每轮都要交给多个收尾函数。此前它们作为 6~7 个
// 位置参数逐个传递，调用点读起来是"一串类型各异的实参"，且顺序写错时编译器
// 不会报错（例如把 cs 与 outcome 的位置调换）。收成一个结构体后，收尾步骤
// 变成它的方法：调用点只出现真正"这一次不同"的东西（本轮 outcome、是否首轮）。
type turnRun struct {
	// sess 本回合的会话（消息历史、失败计数、计划状态）。
	sess *session.Session
	// cs 命令通道的输出捕获器：工具以真实命令执行时，其回复落在这里。
	cs *execution.CaptureSender
	// provAttachments 模型直接输出的附件，跨轮次累积（原生图像输出等）。
	provAttachments []platform.Attachment
	// maxTokens 当前输出预算：被截断且无可见产出时会临时提高一次。
	maxTokens int
	// p 装配根：收尾步骤仍要写会话历史、读配置（失败预算、空回复诊断）。
	p *Plugin
}

// attachments 合并"命令通道捕获"与"模型直接输出"的两路附件。
func (r *turnRun) attachments() []platform.Attachment {
	return runtime.MergeChatAttachments(r.cs.CapturedAttachments, r.provAttachments)
}

// capturedText 命令通道捕获到的文本（模型无输出时的兜底回复）。
func (r *turnRun) capturedText() string { return r.cs.CapturedText }

// consumeTurnStream 读完一轮流，把事件归集成 [streamOutcome]。
//
// 归集与判定分离的原因：流事件的语义（正文/推理/工具调用/附件/错误/结束）
// 与"这一轮算不算成功"是两件事。此前两者混在同一个 356 行的循环里，
// 使得任何一处收尾规则调整都要重读整段流程。
func consumeTurnStream(events <-chan protocol.StreamEvent) streamOutcome {
	var out streamOutcome
	var text strings.Builder
	for event := range events {
		switch event.Type {
		case protocol.StreamEventText:
			text.WriteString(event.Content)
		case protocol.StreamEventReasoning:
			out.reasoningChars += len(event.Content)
		case protocol.StreamEventToolCall:
			if event.ToolCall != nil && event.ToolCall.Name != "" {
				out.toolCalls = append(out.toolCalls, *event.ToolCall)
			}
		case protocol.StreamEventAttachment:
			if event.Attachment != nil {
				out.attachments = append(out.attachments, *event.Attachment)
			}
		case protocol.StreamEventError:
			// 部分 provider（如 Anthropic）在流被取消时会以错误事件收尾：
			// 是否主动停止在收尾处按 Interrupted 判定，此处仅记录。
			out.err = event.Err
		case protocol.StreamEventDone:
			out.doneReceived = true
			out.finishReason = event.FinishReason
			if event.Usage != nil {
				out.reasoningTokens = event.Usage.ReasoningTokens
			}
		}
	}
	out.text = text.String()
	return out
}

// assignToolCallIDs 为缺失 ID 的工具调用补一个稳定标识。
// OpenAI/Anthropic 都要求 tool_call 与后续 tool 消息通过 ID 配对。
func assignToolCallIDs(calls []protocol.ToolCall) {
	for i := range calls {
		if calls[i].ID == "" {
			calls[i].ID = fmt.Sprintf("call_%s_%d", calls[i].Name, i)
		}
	}
}

// buildReplanMessage 返回本轮应追加的重规划指令（无则 nil）。
//
// 重规划闭环：前序步骤全部终态、自身失败的前沿步骤 → 追加指令要求模型调整
// 计划而非按旧计划继续。同一条指令不重复追加。
func buildReplanMessage(sess *session.Session) *protocol.Message {
	plan := sess.PlanSnapshot()
	if plan == nil || !plan.Active {
		return nil
	}
	f := plan.FirstFailedFrontier()
	if f == nil || runtime.LastUserIsReplan(sess) {
		return nil
	}
	m := runtime.BuildReplanMessage(f)
	return &m
}

// buildTurnRequest 组装本轮的请求载荷。
//
// 顺序即前缀缓存的边界：System → 历史 → 动态上下文 → 计划 → 工具声明。
// 逐轮变化的内容一律挂在尾部（见 InjectDynamicContext 与计划注入），
// 这样同一回合的后续请求还能复用前面的字节。
//
// 请求副本在同一处完成三件事：附件保留策略、按引用水合二进制、图片计量。
// 三者都只作用于副本，不会写回会话历史。
//
// 会话、输出预算与回合上下文都来自 turnRun：它们在整个回合内不变，
// 只有 ctx（事件）与 dynamicContext 是本轮构造的。
func (p *Plugin) buildTurnRequest(ctx *eventctx.Context, run *turnRun, turnCtx context.Context,
	activeTools []toolkit.Action, dynamicContext string, firstRound bool) *protocol.ChatRequest {
	sess, maxTokens := run.sess, run.maxTokens
	sess.Lock()
	msgs := make([]protocol.Message, len(sess.Messages))
	copy(msgs, sess.Messages)
	sess.Unlock()

	// 附件保留策略：当前轮附件始终保留；历史图片在保留窗口（最近 N 条
	// user 消息 + 时间窗）内随请求发送，支持"继续追问图片细节"；
	// 更早/更老的图片降级为文本占位，避免无限上传（内存与 token 浪费）。
	// 只在首轮记录指标：保留策略每轮都会跑一遍，逐轮累加会把同一次剥离
	// 计成 2~3 次（工具轮的请求数）。
	msgs, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, runtime.Retention{
		MaxTurns:      p.cfg.ImageContextTurns,
		Window:        p.cfg.ImageContextWindow,
		MaxPerRequest: p.cfg.MaxImagesPerRequest,
	})
	if firstRound {
		recordImagesDropped("budget", budgetDropped)
		recordImagesDropped("stale", staleDropped)
		imagesInRequest.Observe(float64(countRequestImages(msgs)))
	}
	if budgetDropped > 0 && sess.MarkImageOverflowNotified() {
		logger.Warnf("[AI] %d historical image(s) dropped over budget (max_images_per_request=%d)",
			budgetDropped, p.cfg.MaxImagesPerRequest)
		ctx.ReplyText(fmt.Sprintf("本次对话图片较多，已保留最近 %d 张，更早的图片不再显示",
			p.cfg.MaxImagesPerRequest))
	}

	// 历史消息只存附件引用（AttachmentID / SourceURL），这里按引用把二进制补进
	// **请求副本**：会话本身不持有字节，所以历史里有多少图都不影响常驻内存。
	// 补齐结果同时写回会话缓存，同一回合的后续工具轮直接命中。
	if filled, unavailable := p.hydrateAttachmentParts(turnCtx, msgs, sess); filled > 0 || unavailable > 0 {
		logger.Debugf("[AI] hydrated %d attachment(s), %d unavailable (session %s)",
			filled, unavailable, sess.ID)
	}

	// 兜底修复工具调用序列：中断跳过的工具、进程异常退出或持久化损坏
	// 都可能让 assistant(tool_calls) 缺少对应 tool 消息（OpenAI/Anthropic
	// API 硬性约束，缺失即 400）。按需补占位 tool 消息，自愈会话历史。
	msgs = runtime.RepairToolCallSequence(msgs)
	msgs = runtime.InjectDynamicContext(msgs, dynamicContext)

	// 计划注入：存在进行中的任务计划时，把"当前计划+进度"附在消息序列末尾，
	// 而不是插到系统提示词之后，让模型有状态可依地推进多步任务，同时不使
	// 已完成的历史（含本回合的工具结果）失去缓存复用能力。
	//
	// 角色必须是 user 而不是 system：Anthropic 适配器只把数组内的第一条
	// system 放进顶级 system 字段，数组里的其余 system 消息会被整条丢弃
	// （见 protocol.toAnthropicMessages），若用 system 则计划对 Claude 完全
	// 不可见；尾部 user 消息对所有 provider 都成立，与反思/重规划指令同一约定。
	if text := sess.PlanText(); text != "" {
		msgs = append(msgs, protocol.Message{Role: protocol.RoleUser, Content: "===== 当前执行计划 =====\n" + text})
	}

	return &protocol.ChatRequest{
		Model:           p.cfg.Model,
		Messages:        msgs,
		Tools:           wireSpecs(activeTools),
		Temperature:     p.cfg.Temperature,
		TopP:            p.cfg.TopP,
		MaxTokens:       maxTokens,
		ReasoningEffort: p.cfg.ReasoningEffort,
	}
}

// recordToolResults 把本轮的并行执行结果回填进会话，并处理失败预算。
//
// 返回非 nil 表示应当立即收尾（连续失败达到 tool_retry_limit+1）。
func (r *turnRun) recordToolResults(ctx *eventctx.Context, toolCalls []protocol.ToolCall, results []runtime.ToolExecResult) *chatResult {
	sess := r.sess
	for i, tc := range toolCalls {
		// 中断抢占后未启动的工具调用：补占位 tool 消息，保证 assistant
		// 的每个 tool_call_id 都有对应 tool 响应（API 硬性约束，
		// 缺失会导致下次请求被 400 拒绝）。
		if results[i].Skipped {
			r.p.sm.AppendMessageNoPersist(sess, protocol.Message{
				Role:       protocol.RoleTool,
				Content:    "（工具未执行：对话被新消息打断）",
				ToolCallID: tc.ID,
			})
			continue
		}
		toolResult := results[i].Result
		r.p.sm.AppendMessageNoPersist(sess, protocol.Message{
			Role:       protocol.RoleTool,
			Content:    runtime.TruncateToolResult(toolResult),
			ToolCallID: tc.ID,
		})

		// 计划创建时同步展示给用户（chat 内可见计划，后续步骤更新不打扰）。
		// 发生在进行中的回合：QQ 单聊/群聊（频道除外）Markdown 场景附带
		// "查看计划/停止生成"指令按钮（见 maybeAttachQQPlanButtons），长任务
		// 期间可一键刷新进度或中断。
		if tc.Name == catalog.PlanCreateToolName && results[i].Err == nil {
			msg := r.p.formatReplyMessage(toolResult)
			r.p.replyAndRecord(ctx, r.p.maybeAttachQQPlanButtons(ctx, msg, true))
		}

		// 失败重试预算与反思引导：
		//   - 失败结果已回填（模型天然可重试）
		//   - 同一工具连续失败第 2 次起，追加"反思指令"用户消息，
		//     强制模型先分析原因再采用不同策略（显式反思轮）
		//   - 连续失败达到 tool_retry_limit+1 次时优雅中止本轮，
		//     替代撞 max_depth 的裸错误
		if results[i].Err == nil {
			sess.ResetToolFailure(tc.Name)
			continue
		}
		fails := sess.IncrToolFailure(tc.Name)
		if fails > runtime.EffectiveToolRetryLimit(runtimeLimits(r.p.cfg)) {
			msg := execution.BuildRetryAbortMessage(tc.Name, fails, toolResult)
			r.p.sm.AppendMessageNoPersist(sess, protocol.Message{Role: protocol.RoleUser, Content: msg})
			return &chatResult{Text: msg, Attachments: r.attachments()}
		}
		if fails >= 2 {
			r.p.sm.AppendMessageNoPersist(sess, execution.BuildReflectionMessage(tc.Name, fails, toolResult))
		}
	}
	return nil
}

// closeOnInterrupt 处理"被主动停止"的收尾：把已到手部分记入会话并作为最终
// 回复返回，不视为错误。返回 done=false 表示没有被中断，调用方继续常规路径。
func (r *turnRun) closeOnInterrupt(outcome streamOutcome) (*chatResult, bool) {
	sess := r.sess
	if !sess.Interrupted() || (outcome.doneReceived && outcome.err == nil) {
		return nil, false
	}
	if outcome.text != "" {
		r.p.sm.AppendMessageNoPersist(sess, protocol.Message{Role: protocol.RoleAssistant, Content: outcome.text})
	}
	text := outcome.text
	if text == "" {
		text = r.capturedText()
	}
	return &chatResult{Text: text, Attachments: r.attachments()}, true
}

// closeOutcome 描述"本轮已收尾"的两种可能结果。
type closeOutcome struct {
	result *chatResult
	err    error
}

// closeWithoutDone 处理"流已结束但没收到结束事件"的收尾。
//
// 返回 done=false 表示收到了正常结束事件，调用方继续按常规路径处理。
func (r *turnRun) closeWithoutDone(outcome streamOutcome) (closeOutcome, bool) {
	if outcome.doneReceived {
		return closeOutcome{}, false
	}
	attachments := r.attachments()
	if outcome.text != "" || r.capturedText() != "" || len(attachments) > 0 {
		text := outcome.text
		if text == "" {
			text = r.capturedText()
		}
		logger.Warnf("[AI] chat stream ended without done event (finish_reason=%q), using partial output", outcome.finishReason)
		return closeOutcome{result: &chatResult{Text: text, Attachments: attachments}}, true
	}
	return closeOutcome{
		result: &chatResult{},
		err:    fmt.Errorf("chat stream: 连接中断（未收到结束事件，finish_reason=%q）", outcome.finishReason),
	}, true
}

// escalateOnTruncation 判断本轮是否属于"思考耗尽输出预算"，需要提高 max_tokens 重试。
//
// 条件：正常结束、没有工具调用、没有正文、finish_reason=length，且回合内还没有
// 任何可见产出（发送工具未发出内容、没有附件）。返回新的预算与是否重试。
func (r *turnRun) escalateOnTruncation(outcome streamOutcome) (int, bool) {
	maxTokens := r.maxTokens
	if !outcome.doneReceived || len(outcome.toolCalls) > 0 || outcome.text != "" {
		return maxTokens, false
	}
	if outcome.finishReason != finishReasonLength || r.capturedText() != "" || len(r.provAttachments) > 0 {
		return maxTokens, false
	}
	next := escalateMaxTokens(maxTokens, r.p.cfg.MaxTokensCap)
	if next <= maxTokens {
		return maxTokens, false
	}
	logger.Warnf("[AI] no visible output before truncation (finish_reason=%q, "+
		"reasoning_chars=%d, reasoning_tokens=%d, max_tokens=%d); retrying with max_tokens=%d",
		outcome.finishReason, outcome.reasoningChars, outcome.reasoningTokens, maxTokens, next)
	return next, true
}

// finishWithoutToolCalls 收尾"本轮没有工具调用"的回合。
func (r *turnRun) finishWithoutToolCalls(outcome streamOutcome,
	replanMsg *protocol.Message, firstRound bool) (*chatResult, error) {
	sess := r.sess
	if outcome.text != "" {
		r.p.sm.AppendMessageNoPersist(sess, protocol.Message{Role: protocol.RoleAssistant, Content: outcome.text})
	}
	if replanMsg != nil {
		r.p.sm.AppendMessageNoPersist(sess, *replanMsg)
	}
	attachments := r.attachments()
	// 首轮即空回复（无文本、无工具调用、无附件、无发送工具捕获输出）：
	// 模型这次什么都没产出。仅限首轮：更深的轮次里用户可能已通过其他
	// 通道（如 create_plan 的计划消息）收到输出，此时按空回复报错会误伤。
	if outcome.text == "" && firstRound && r.capturedText() == "" && len(attachments) == 0 {
		return &chatResult{}, r.p.emptyCompletionError(outcome, r.maxTokens)
	}
	return &chatResult{Text: outcome.text, Attachments: attachments}, nil
}

// emptyCompletionError 把"首轮空回复"翻译成可定位的错误。
//
// 模型这次什么都没产出（如 finish_reason=length/content_filter，或思考型
// 模型的正文为空）。不能当成成功的空回答静默结束，否则用户只看到"发了消息
// 没反应"；报错让调用方给出可见反馈并留下日志。
func (p *Plugin) emptyCompletionError(outcome streamOutcome, maxTokens int) error {
	logger.Warnf("[AI] empty completion from model %q (finish_reason=%q, "+
		"reasoning_chars=%d, reasoning_tokens=%d, max_tokens=%d)",
		p.cfg.Model, outcome.finishReason, outcome.reasoningChars, outcome.reasoningTokens, maxTokens)
	// 有推理产出却没有任何正文：思考型模型把 max_tokens 花在了推理上，
	// 报错时给出可定位的原因（而非笼统的"空回复"）。
	if outcome.finishReason == finishReasonLength &&
		(outcome.reasoningChars > 0 || outcome.reasoningTokens > 0) {
		return fmt.Errorf(
			"模型思考过程耗尽了输出预算，未产出任何内容（finish_reason=length, reasoning_tokens=%d, max_tokens=%d）",
			outcome.reasoningTokens, maxTokens)
	}
	return fmt.Errorf("模型未返回任何内容（空回复，finish_reason=%q）", outcome.finishReason)
}
