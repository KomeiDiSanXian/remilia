// metrics_defs.go — AI 插件 Prometheus 指标。
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

	attachmentHydrations = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "attachment_hydrations_total",
			Help:      "请求构建时按引用补齐附件二进制的次数（source: cache/storage/url）",
		},
		[]string{"source"},
	)).(*prometheus.CounterVec)

	attachmentUnavailable = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "attachment_unavailable_total",
			Help:      "按引用无法取回附件、降级为文本占位的次数（kind: image/audio）",
		},
		[]string{"kind"},
	)).(*prometheus.CounterVec)

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
