// metrics.go — AI 插件 Prometheus 指标。
//
// 指标族（namespace "ai"）：
//   - ai_llm_calls_total{model, result}       LLM 调用次数（result: ok/error/stopped）
//   - ai_llm_latency_seconds{model}           LLM 调用耗时（含流式全时长）
//   - ai_llm_ttft_seconds{model, kind}        流式首字延迟（发起请求 → 第一个内容事件）
//   - ai_llm_tokens_per_second{model, kind}   流式生成速度（completion / 首字到结束）
//   - ai_llm_tokens_total{model, type, kind} token 用量（type: prompt/completion/prompt_cached/reasoning，
//     kind: chat/judge/verify/extract/summary/skill/service/other）
//
// 总耗时把排队、prefill 与逐 token 生成混在一个数里：同样 8 秒，可能是首字等了
// 7 秒（容量/排队），也可能是首字 1 秒、生成 7 秒（输出长/模型慢）。ttft 与
// tokens_per_second 拆开这两段，后者刻意不含排队与 prefill，才能和首字延迟
// 分开归因。两者只对产生内容事件的流式调用采样，非流式调用观测不到首字。
//
// prompt_cached 为命中提示词前缀缓存的输入 token 数，
// rate(prompt_cached)/rate(prompt) 即前缀缓存命中率。
// reasoning 为思考型模型的推理 token 数（已计入 completion），
// rate(reasoning)/rate(completion) 接近 1 说明该端点把思考也算进输出预算。
//
// kind 把"对话回合"与"辅助调用"分开：判定/校验/抽取/总结这类一次性调用的
// 提示词天然没有可复用的历史前缀，命中率接近 0，混在一个计数器里会把主对话
// 的命中率拉低（也掩盖真实波动）。整体与分用途命中率的查询：
//
//	# 整体
//	sum(rate(ai_llm_tokens_total{type="prompt_cached"}[1h]))
//	  / sum(rate(ai_llm_tokens_total{type="prompt"}[1h]))
//	# 分用途（注意分母只取同一 kind，否则比值的意义会被稀释）
//	sum by (kind) (rate(ai_llm_tokens_total{type="prompt_cached"}[1h]))
//	  / sum by (kind) (rate(ai_llm_tokens_total{type="prompt"}[1h]))
//
// 每次调用（含失败/中断）另有一条 Debug 日志，形如
// "llm call kind=chat scope=<会话ID> model=... msgs=.. tools=.. prompt=.. cached=.. hit=..% ..."，
// 用于按会话、按回合还原"哪些请求没有命中"：指标只有聚合值，定位前缀
// 具体在哪一轮断开需要单次调用的原始数据。日志不含任何消息内容。
//   - ai_tool_calls_total{tool, result}       工具调用次数（result: ok/error）
//   - ai_toolset_changes_total{reason}        工具集变更次数
//     （reason: init/grow/shrink/decay；仅在集合真的变化时计数，keep 不计数）
//   - ai_toolset_size                        每轮发送的工具数量分布
//   - ai_catalog_changes_total{reason}        目录代数推进次数（reason: add/remove/update）
//   - ai_catalog_generation                   当前目录内容版本
//   - ai_tasks_created_total                  任务创建次数（新计划取代旧任务同样计数）
//   - ai_task_transitions_total{status}       任务进入各目标状态的次数
//     （status: running/succeeded/failed/cancelled/interrupted 等）
//
// 目录版本会在成员、动作定义、策略或来源身份变化时推进；每次推进都会让
// 会话级选择缓存失效。ai_catalog_changes_total 的变化率可与
// mcp_tool_list_changes_total 对照，定位动态目录变化来源。
//
// 工具集与提示词前缀：tools 排在请求最前面，集合一变其后的历史全部失去
// 前缀缓存。ai_toolset_changes_total 的增速（变更次数 / 请求次数）直接
// 反映抖动程度，与 ai_llm_tokens_total{type="prompt_cached"} 的命中率
// 一起看，可定量判断稳定策略是否真的减少了整段历史的重复计费。
//
// LLM 指标经 metricsProvider（Provider 装饰器，newProvider 统一包装）采集；
// 流式调用的耗时覆盖整个流的消费过程，token 用量取自 Done 事件的 Usage。
// 装饰器留在本包而非协议层：协议层只描述线格式，不承担可观测性依赖。
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
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	llmCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "llm_calls_total",
			Help:      "LLM 调用次数（result: ok/error/stopped）",
		},
		[]string{"model", "result"},
	)).(*prometheus.CounterVec)

	llmLatency = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "llm_latency_seconds",
			Help:      "LLM 调用耗时（流式为整个流的消费时长）",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"model"},
	)).(*prometheus.HistogramVec)

	llmTokens = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "llm_tokens_total",
			Help:      "LLM token 用量（type: prompt/completion/prompt_cached/reasoning；kind: 调用用途）",
		},
		[]string{"model", "type", "kind"},
	)).(*prometheus.CounterVec)

	llmTTFT = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "llm_ttft_seconds",
			Help:      "流式调用首字延迟（发起请求到第一个内容事件；非流式调用不采样）",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 10, 15, 20, 30, 60},
		},
		[]string{"model", "kind"},
	)).(*prometheus.HistogramVec)

	llmSpeed = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "llm_tokens_per_second",
			Help:      "流式生成速度（completion token / 首字到结束，不含排队与 prefill）",
			Buckets:   []float64{1, 2, 5, 10, 20, 30, 40, 50, 60, 80, 100, 150, 200},
		},
		[]string{"model", "kind"},
	)).(*prometheus.HistogramVec)

	toolCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "tool_calls_total",
			Help:      "AI 工具调用次数（result: ok/error）",
		},
		[]string{"tool", "result"},
	)).(*prometheus.CounterVec)

	toolDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "tool_duration_seconds",
			Help:      "工具执行耗时（顶层调用，含工具内部 IO 与技能子循环）",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"tool"},
	)).(*prometheus.HistogramVec)

	toolTimeouts = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "tool_timeouts_total",
			Help:      "工具执行超时次数（与业务失败区分：超时说明预算不够或工具卡住）",
		},
		[]string{"tool"},
	)).(*prometheus.CounterVec)

	toolSetChanges = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "toolset_changes_total",
			Help:      "工具集变更次数（reason: init/grow/shrink/decay）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	toolSetSize = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "toolset_size",
			Help:      "每轮发送给 LLM 的工具数量",
			Buckets:   []float64{1, 3, 5, 8, 12, 16, 20, 24, 32, 48, 64},
		},
	)).(prometheus.Histogram)

	catalogChanges = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "catalog_changes_total",
			Help:      "目录代数推进次数（reason: add/remove/update；每次推进都会让会话级选择缓存失效）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	catalogGeneration = inframetrics.MustRegisterOrGet(nil, prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "ai",
			Name:      "catalog_generation",
			Help:      "当前目录内容版本（成员、动作定义、策略或来源身份）",
		},
	)).(prometheus.Gauge)

	taskCreated = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "tasks_created_total",
			Help:      "任务创建次数（新计划取代旧任务同样计数）",
		},
	)).(prometheus.Counter)

	taskTransitions = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "task_transitions_total",
			Help:      "任务进入各目标状态的次数",
		},
		[]string{"status"},
	)).(*prometheus.CounterVec)

	llmErrors = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "llm_errors_total",
			Help:      "LLM 调用失败次数（class: rate_limit/auth/context_length/timeout/canceled/server/client/network/other）",
		},
		[]string{"model", "class"},
	)).(*prometheus.CounterVec)

	llmFinish = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "llm_finish_total",
			Help:      "流式调用的结束原因分布（stop/length/content_filter/tool_calls/other；截断与空回复看这里）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	llmRetries = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "llm_retries_total",
			Help:      "LLM 请求重试次数（reason: rate_limit/timeout/server/network）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	turnTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "turn_total",
			Help:      "对话回合数（result: ok/empty/error/interrupted）",
		},
		[]string{"result"},
	)).(*prometheus.CounterVec)

	turnDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "turn_duration_seconds",
			Help:      "对话回合耗时（用户可感知的完整等待时长，含校验重生成）",
			Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300},
		},
	)).(prometheus.Histogram)

	turnLLMCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "turn_llm_calls",
			Help:      "单个回合内的 LLM 调用次数（工具轮与校验重生成都计入）",
			Buckets:   []float64{1, 2, 3, 4, 6, 8, 12, 16, 24, 32},
		},
	)).(prometheus.Histogram)

	turnToolCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "turn_tool_calls",
			Help:      "单个回合内的工具调用次数",
			Buckets:   []float64{0, 1, 2, 3, 5, 8, 12, 20, 32, 50},
		},
	)).(prometheus.Histogram)

	approvals = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "approval_total",
			Help:      "工具审批结果（result: approved/denied/timeout/interrupted）",
		},
		[]string{"result"},
	)).(*prometheus.CounterVec)

	approvalWait = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "approval_wait_seconds",
			Help:      "等待人工审批的时长",
			Buckets:   []float64{1, 5, 10, 20, 30, 45, 60, 120},
		},
	)).(prometheus.Histogram)

	imagesDropped = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "images_dropped_total",
			Help:      "历史图片被剥离的次数（reason: stale=保留窗口/时间窗过期，budget=单请求预算超限）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	imagesInRequest = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "images_in_request",
			Help:      "单次请求实际附带的历史图片数",
			Buckets:   []float64{0, 1, 2, 3, 4, 6, 8, 12},
		},
	)).(prometheus.Histogram)

	memoryExtract = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "memory_extract_total",
			Help:      "事实抽取运行次数（result: ok/error/skipped）",
		},
		[]string{"result"},
	)).(*prometheus.CounterVec)

	memoryExtractFacts = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "memory_extract_facts",
			Help:      "单次抽取写入的事实条数",
			Buckets:   []float64{0, 1, 2, 3, 5, 8, 13, 21},
		},
	)).(prometheus.Histogram)

	memoryFacts = inframetrics.MustRegisterOrGet(nil, prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "ai",
			Name:      "memory_facts_cached",
			Help:      "已加载作用域内的长期记忆事实总数（缓存口径）",
		},
	)).(prometheus.Gauge)

	ragQueryTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "rag_queries_total",
			Help:      "相关历史检索次数（result: hit/empty）",
		},
		[]string{"result"},
	)).(*prometheus.CounterVec)

	ragDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "ai",
			Name:      "rag_duration_seconds",
			Help:      "相关历史检索耗时（含 embedding 精排）",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
	)).(prometheus.Histogram)

	toolSelect = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "tool_select_total",
			Help:      "工具选择路径（path: bypass=工具数未超阈值，retrieval=本地检索 Top-K）",
		},
		[]string{"path"},
	)).(*prometheus.CounterVec)
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

	var prompt, completion, reasoning, cached int
	if r.usage != nil {
		prompt, completion = r.usage.PromptTokens, r.usage.CompletionTokens
		reasoning, cached = r.usage.ReasoningTokens, r.usage.CachedTokens
		if prompt > 0 {
			llmTokens.WithLabelValues(meta.model, "prompt", kind).Add(float64(prompt))
		}
		if completion > 0 {
			llmTokens.WithLabelValues(meta.model, "completion", kind).Add(float64(completion))
		}
		// 推理 token（思考型模型单独返回时才记，已含在 completion 内），
		// 用于区分"输出预算被思考耗尽"与真正的空回复。
		if reasoning > 0 {
			llmTokens.WithLabelValues(meta.model, "reasoning", kind).Add(float64(reasoning))
		}
		// 前缀缓存命中量（供应商返回时才记），用于计算缓存命中率
		if cached > 0 {
			llmTokens.WithLabelValues(meta.model, "prompt_cached", kind).Add(float64(cached))
		}
	}
	logLLMCall(meta, r.duration, r.ttft, speed, prompt, completion, reasoning, cached, r.result)
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

// logLLMCall 每次调用写一条 Debug 日志（含失败/中断）。
// 指标只有聚合值，定位"哪一轮、哪个会话没有命中前缀缓存"需要单次调用数据；
// 日志只含计数与标识，不含任何消息内容（提示词/工具结果不落日志）。
func logLLMCall(meta llmCallMeta, duration, ttft time.Duration, speed float64,
	prompt, completion, reasoning, cached int, result string) {
	hit := 0.0
	if prompt > 0 {
		hit = float64(cached) / float64(prompt) * 100
	}
	logger.Debugf("[AI] llm call kind=%s scope=%s model=%s msgs=%d tools=%d "+
		"prompt=%d cached=%d hit=%.1f%% completion=%d reasoning=%d "+
		"ttft=%dms tok_s=%.1f result=%s duration=%dms",
		meta.kind, meta.scope, meta.model, meta.messages, meta.tools,
		prompt, cached, hit, completion, reasoning,
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

// metricsProvider 装饰 Provider，采集 LLM 调用指标。
type metricsProvider struct {
	next         protocol.Provider
	defaultModel string
}

// Chat 包装：所有 LLM 调用自动计入指标。
func (m *metricsProvider) Chat(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	meta := llmCallMetaOf(ctx, req, m.defaultModel)
	resp, err := m.next.Chat(ctx, req)
	result := "ok"
	if err != nil {
		result = "error"
	}
	var usage *protocol.TokenUsage
	if resp != nil {
		usage = resp.Usage
	}
	recordLLMCall(meta, llmCallResult{duration: time.Since(start), usage: usage, result: result, err: err})
	return resp, err
}

// ChatStream 包装流式调用：耗时覆盖整个流的消费过程。
func (m *metricsProvider) ChatStream(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
	start := time.Now()
	meta := llmCallMetaOf(ctx, req, m.defaultModel)

	inner, err := m.next.ChatStream(ctx, req)
	if err != nil {
		recordLLMCall(meta, llmCallResult{duration: time.Since(start), result: "error", err: err})
		return nil, err
	}

	outer := make(chan protocol.StreamEvent, 64)
	go func() {
		defer close(outer)
		var (
			usage        *protocol.TokenUsage
			done         bool
			finishReason string
			streamErr    error
			// firstAt 第一个内容事件（正文/推理/工具调用/附件）的时间：
			// 推理片段也算首字——它代表模型真正开始产出，而不是首包/心跳。
			firstAt time.Time
		)
		// record 统一收尾：每次调用只计一次，ttft 只在观测到首字时才有值。
		record := func(result string) {
			var ttft time.Duration
			if !firstAt.IsZero() {
				ttft = firstAt.Sub(start)
			}
			// 只有按失败收尾时才计入错误分类；主动停止不算错误。
			var recErr error
			if result == "error" {
				recErr = streamErr
				if recErr == nil {
					recErr = errStreamEndedWithoutDone
				}
			}
			recordLLMCall(meta, llmCallResult{
				duration: time.Since(start),
				ttft:     ttft,
				usage:    usage,
				result:   result,
				finish:   finishReason,
				err:      recErr,
			})
		}
		for {
			select {
			case ev, open := <-inner:
				if !open {
					// 流关闭但未收到 Done（provider 内部异常收尾）
					result := "stopped"
					if done {
						result = "ok"
					} else if ctx.Err() == nil {
						result = "error"
					}
					record(result)
					return
				}
				switch ev.Type {
				case protocol.StreamEventText, protocol.StreamEventReasoning,
					protocol.StreamEventToolCall, protocol.StreamEventAttachment:
					if firstAt.IsZero() {
						firstAt = time.Now()
					}
				case protocol.StreamEventDone:
					done = true
					usage = ev.Usage
					finishReason = ev.FinishReason
				case protocol.StreamEventError:
					streamErr = ev.Err
				}
				select {
				case outer <- ev:
				case <-ctx.Done():
					// 下游停止消费：按停止收尾并排空 inner，
					// 避免 provider 的发送 goroutine 阻塞
					for range inner {
					}
					record("stopped")
					return
				}
			case <-ctx.Done():
				// 上游/回合取消且暂无事件可转发：同样排空 inner，避免 provider
				// 的发送 goroutine 因无人接收而永久阻塞泄漏。
				for range inner {
				}
				record("stopped")
				return
			}
		}
	}()
	return outer, nil
}
