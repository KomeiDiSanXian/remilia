// metrics_record.go — 指标记录函数：把运行时事件翻译成指标更新。
//
// 指标定义见 metrics_defs.go，Provider 装饰器见 metrics_provider.go。
package ai

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// recordToolSet 记录一次工具集决策结果。
// changed 为 false 时只观测集合大小，不计变更次数（保持率不被"无变化轮次"稀释）。
func recordToolSet(reason string, changed bool, size int) {
	toolSetSize.Observe(float64(size))
	if changed {
		toolSetChanges.WithLabelValues(reason).Inc()
	}
}

// recordTurn 记录一个对话回合的结果与规模（用户可感知的单位是回合，不是单次调用）。
func recordTurn(result string, duration time.Duration, llmCalls, toolCalls int) {
	turnTotal.WithLabelValues(result).Inc()
	turnDuration.Observe(duration.Seconds())
	turnLLMCalls.Observe(float64(llmCalls))
	turnToolCalls.Observe(float64(toolCalls))
}

// recordToolExecution 记录一次顶层工具执行：耗时，以及"超时"与业务失败的区分。
func recordToolExecution(tool string, duration time.Duration, err error) {
	toolDuration.WithLabelValues(tool).Observe(duration.Seconds())
	if errors.Is(err, context.DeadlineExceeded) {
		toolTimeouts.WithLabelValues(tool).Inc()
	}
}

// recordApproval 记录一次人工审批的结果与等待时长。
func recordApproval(outcome string, waited time.Duration) {
	approvals.WithLabelValues(outcome).Inc()
	approvalWait.Observe(waited.Seconds())
}

// recordImagesDropped 记录被剥离的历史图片（stale=保留窗口过期，budget=单请求超限）。
func recordImagesDropped(reason string, n int) {
	if n > 0 {
		imagesDropped.WithLabelValues(reason).Add(float64(n))
	}
}

// recordAttachmentHydration 记录一次按引用补齐附件二进制。
func recordAttachmentHydration(source string) {
	attachmentHydrations.WithLabelValues(source).Inc()
}

// recordAttachmentUnavailable 记录一次附件无法按引用取回（已降级为文本占位）。
func recordAttachmentUnavailable(kind string) {
	attachmentUnavailable.WithLabelValues(kind).Inc()
}

// recordMemoryExtract 记录一次事实抽取；只在成功时观测写入条数。
func recordMemoryExtract(result string, facts int) {
	memoryExtract.WithLabelValues(result).Inc()
	if result == "ok" {
		memoryExtractFacts.Observe(float64(facts))
	}
}

// setMemoryFactsCached 更新"已加载作用域内的事实总数"（缓存口径）。
func setMemoryFactsCached(n int) { memoryFacts.Set(float64(n)) }

// recordRAG 记录一次相关历史检索：有命中/空结果 + 耗时。
func recordRAG(d time.Duration, text string) {
	result := "empty"
	if strings.TrimSpace(text) != "" {
		result = "hit"
	}
	ragQueryTotal.WithLabelValues(result).Inc()
	ragDuration.Observe(d.Seconds())
}

// recordToolSelectPath 记录工具选择走的是短路还是本地检索。
func recordToolSelectPath(path string) { toolSelect.WithLabelValues(path).Inc() }

// recordLLMRetry 记录一次协议层重试（由 protocol.SetRetryObserver 回调）。
func recordLLMRetry(reason string) { llmRetries.WithLabelValues(reason).Inc() }

// recordCatalogGeneration 记录一次目录内容版本推进：计数器按原因累加，gauge
// 反映当前版本。变化率即"会让会话缓存失效"的事件率。
func recordCatalogGeneration(reason string, generation uint64) {
	catalogChanges.WithLabelValues(reason).Inc()
	catalogGeneration.Set(float64(generation))
}

// llmCallMeta 一次 LLM 调用的观测维度：指标标签（model/kind）与单次日志字段。
type llmCallMeta struct {
	model    string
	kind     protocol.CallKind
	scope    string
	messages int
	tools    int
}

// llmCallMetaOf 从 ctx 标注与请求本身收集观测维度。
// kind 未标注时落为 other（见 protocol.CallKindOther），便于发现漏标调用点。
func llmCallMetaOf(ctx context.Context, req *protocol.ChatRequest, defaultModel string) llmCallMeta {
	meta := llmCallMeta{
		kind:  protocol.CallKindOf(ctx),
		scope: protocol.CallScopeOf(ctx),
	}
	if req != nil {
		meta.model = protocol.RequestModel(defaultModel, req.Model)
		meta.messages = len(req.Messages)
		meta.tools = len(req.Tools)
	}
	return meta
}

// llmCallResult 一次调用的收尾数据。
type llmCallResult struct {
	duration time.Duration
	ttft     time.Duration
	usage    *protocol.TokenUsage
	result   string
	finish   string
	err      error
}

// recordLLMCall 记录一次 LLM 调用结果与用量。
//
// ttft 为流式调用的首字延迟（0 表示未知/非流式）：它同时决定是否采样
// TTFT 与生成速度——非流式调用观测不到首字，把端到端耗时当生成速度会污染
// 直方图（短输出的"速度"会低得离谱）。
func recordLLMCall(meta llmCallMeta, r llmCallResult) {
	llmCalls.WithLabelValues(meta.model, r.result).Inc()
	llmLatency.WithLabelValues(meta.model).Observe(r.duration.Seconds())

	if r.err != nil {
		llmErrors.WithLabelValues(meta.model, llmErrorClass(r.err)).Inc()
	}
	if r.finish != "" {
		llmFinish.WithLabelValues(r.finish).Inc()
	}

	kind := string(meta.kind)
	speed := 0.0
	if r.ttft > 0 {
		llmTTFT.WithLabelValues(meta.model, kind).Observe(r.ttft.Seconds())
		if r.usage != nil {
			speed = decodeSpeed(r.duration, r.ttft, r.usage.CompletionTokens)
			if speed > 0 {
				llmSpeed.WithLabelValues(meta.model, kind).Observe(speed)
			}
		}
	}

	var tokens tokenBreakdown
	if r.usage != nil {
		tokens.prompt, tokens.completion = r.usage.PromptTokens, r.usage.CompletionTokens
		tokens.reasoning, tokens.cached = r.usage.ReasoningTokens, r.usage.CachedTokens
		if tokens.prompt > 0 {
			llmTokens.WithLabelValues(meta.model, "prompt", kind).Add(float64(tokens.prompt))
		}
		if tokens.completion > 0 {
			llmTokens.WithLabelValues(meta.model, "completion", kind).Add(float64(tokens.completion))
		}
		// 推理 token（思考型模型单独返回时才记，已含在 completion 内），
		// 用于区分"输出预算被思考耗尽"与真正的空回复。
		if tokens.reasoning > 0 {
			llmTokens.WithLabelValues(meta.model, "reasoning", kind).Add(float64(tokens.reasoning))
		}
		// 前缀缓存命中量（供应商返回时才记），用于计算缓存命中率
		if tokens.cached > 0 {
			llmTokens.WithLabelValues(meta.model, "prompt_cached", kind).Add(float64(tokens.cached))
		}
	}
	logLLMCall(meta, r.duration, r.ttft, speed, tokens, r.result)
}

// llmErrorClass 把一次失败的 LLM 调用归类。主要依据 APIError.StatusCode：
// 429 与 401 的处理方式完全不同（退避 vs 换 key），400 里的上下文超长要缩
// 上下文而不是重试，5xx 才是端点故障。
func llmErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := errors.AsType[*protocol.APIError](err); ok {
		switch {
		case apiErr.StatusCode == http.StatusTooManyRequests:
			return "rate_limit"
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return "auth"
		case apiErr.StatusCode == http.StatusRequestTimeout || apiErr.StatusCode == http.StatusRequestEntityTooLarge:
			return "timeout"
		case apiErr.StatusCode >= 500:
			return "server"
		case isContextLengthError(apiErr.Body):
			return "context_length"
		default:
			return "client"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return "network"
	}
	return "other"
}

// isContextLengthError 识别"输入超出上下文窗口"的客户端错误：各家文案不同，
// 只做小写包含匹配（DeepSeek/OpenAI 均含 context length / maximum context）。
func isContextLengthError(body string) bool {
	b := strings.ToLower(body)
	for _, marker := range []string{"context_length", "context length", "maximum context", "too many tokens", "prompt is too long"} {
		if strings.Contains(b, marker) {
			return true
		}
	}
	return false
}

// decodeSpeed 计算流式生成速度（token/s）：只用首字之后的时长，排除排队与
// prefill。任一前提不成立时返回 0（调用方据此跳过采样）：
//   - ttft 未知（非流式调用、流未产出内容）
//   - 没有 completion token 数（provider 未返回 usage）
//   - 解码窗口 < 10ms：末 token 与首 token 几乎同时落地，定时器精度会把
//     1 token 放大成几千 tok/s 的伪样本，污染直方图
func decodeSpeed(duration, ttft time.Duration, completionTokens int) float64 {
	if ttft <= 0 || completionTokens <= 0 {
		return 0
	}
	decode := duration - ttft
	if decode < 10*time.Millisecond {
		return 0
	}
	return float64(completionTokens) / decode.Seconds()
}

// tokenBreakdown 一次调用的 token 用量拆分（日志与命中率用）。
//
// 四个字段同源（都来自 usage）、同型（int），作为相邻位置参数时顺序写错不会
// 被编译器发现——而"把 cached 传成 completion"会让日志里的前缀缓存命中率
// 看起来完全正常。收成结构体后每个数字都带名字。
type tokenBreakdown struct {
	prompt     int
	completion int
	reasoning  int
	cached     int
}

// hitPercent 返回前缀缓存命中率（无输入 token 时为 0）。
func (b tokenBreakdown) hitPercent() float64 {
	if b.prompt <= 0 {
		return 0
	}
	return float64(b.cached) / float64(b.prompt) * 100
}

// logLLMCall 每次调用写一条 Debug 日志（含失败/中断）。
// 指标只有聚合值，定位"哪一轮、哪个会话没有命中前缀缓存"需要单次调用数据；
// 日志只含计数与标识，不含任何消息内容（提示词/工具结果不落日志）。
func logLLMCall(meta llmCallMeta, duration, ttft time.Duration, speed float64,
	tokens tokenBreakdown, result string) {
	logger.Debugf("[AI] llm call kind=%s scope=%s model=%s msgs=%d tools=%d "+
		"prompt=%d cached=%d hit=%.1f%% completion=%d reasoning=%d "+
		"ttft=%dms tok_s=%.1f result=%s duration=%dms",
		meta.kind, meta.scope, meta.model, meta.messages, meta.tools,
		tokens.prompt, tokens.cached, tokens.hitPercent(), tokens.completion, tokens.reasoning,
		ttft.Milliseconds(), speed, result, duration.Milliseconds())
}

// recordToolCall 记录一次工具调用结果（作为观测回调注入动作调用器）。
func recordToolCall(tool string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	toolCalls.WithLabelValues(tool, result).Inc()
}

// errStreamEndedWithoutDone 流关闭但没收到结束事件时的兜底错误（用于错误分类）。
var errStreamEndedWithoutDone = errors.New("stream closed without done event")

// recordTaskCreated 记录一次任务创建。
func recordTaskCreated() { taskCreated.Inc() }

// recordTaskStatus 记录任务进入目标状态。
func recordTaskStatus(s task.Status) {
	taskTransitions.WithLabelValues(string(s)).Inc()
}
