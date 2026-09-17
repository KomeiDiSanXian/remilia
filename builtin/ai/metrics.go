package ai

// metrics.go — AI 插件 Prometheus 指标。
//
// 指标族（namespace "ai"）：
//   - ai_llm_calls_total{model, result}       LLM 调用次数（result: ok/error/stopped）
//   - ai_llm_latency_seconds{model}           LLM 调用耗时（含流式全时长）
//   - ai_llm_tokens_total{model, type}        token 用量（type: prompt/completion/prompt_cached）
//
// prompt_cached 为命中提示词前缀缓存的输入 token 数，
// rate(prompt_cached)/rate(prompt) 即前缀缓存命中率。
//   - ai_tool_calls_total{tool, result}       工具调用次数（result: ok/error）
//   - ai_toolset_changes_total{reason}        工具集变更次数（reason: init/grow/decay）
//   - ai_toolset_size                        每轮发送的工具数量分布
//
// 工具集与提示词前缀：tools 排在请求最前面，集合一变其后的历史全部失去
// 前缀缓存。ai_toolset_changes_total 的增速（变更次数 / 请求次数）直接
// 反映抖动程度，与 ai_llm_tokens_total{type="prompt_cached"} 的命中率
// 一起看，可定量判断稳定策略是否真的减少了整段历史的重复计费。
//
// LLM 指标经 metricsProvider（Provider 装饰器，NewProvider 统一包装）采集；
// 流式调用的耗时覆盖整个流的消费过程，token 用量取自 Done 事件的 Usage。

import (
	"context"
	"time"

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
			Help:      "LLM token 用量（type: prompt/completion/prompt_cached）",
		},
		[]string{"model", "type"},
	)).(*prometheus.CounterVec)

	toolCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "tool_calls_total",
			Help:      "AI 工具调用次数（result: ok/error）",
		},
		[]string{"tool", "result"},
	)).(*prometheus.CounterVec)

	toolSetChanges = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "toolset_changes_total",
			Help:      "工具集变更次数（reason: init/grow/decay）",
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
)

// recordToolSet 记录一次工具集决策结果。
// changed 为 false 时只观测集合大小，不计变更次数（保持率不被"无变化轮次"稀释）。
func recordToolSet(reason string, changed bool, size int) {
	toolSetSize.Observe(float64(size))
	if changed {
		toolSetChanges.WithLabelValues(reason).Inc()
	}
}

// recordLLMCall 记录一次 LLM 调用结果与用量。
func recordLLMCall(model string, duration time.Duration, usage *TokenUsage, result string) {
	llmCalls.WithLabelValues(model, result).Inc()
	llmLatency.WithLabelValues(model).Observe(duration.Seconds())
	if usage != nil {
		if usage.PromptTokens > 0 {
			llmTokens.WithLabelValues(model, "prompt").Add(float64(usage.PromptTokens))
		}
		if usage.CompletionTokens > 0 {
			llmTokens.WithLabelValues(model, "completion").Add(float64(usage.CompletionTokens))
		}
		// 前缀缓存命中量（供应商返回时才记），用于计算缓存命中率
		if usage.CachedTokens > 0 {
			llmTokens.WithLabelValues(model, "prompt_cached").Add(float64(usage.CachedTokens))
		}
	}
}

// RecordToolCall 记录一次工具调用结果（供 executeTool 调用）。
func RecordToolCall(tool string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	toolCalls.WithLabelValues(tool, result).Inc()
}

// metricsProvider 装饰 Provider，采集 LLM 调用指标。
type metricsProvider struct {
	next         Provider
	defaultModel string
}

// NewProvider 包装：所有 LLM 调用自动计入指标。
func (m *metricsProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()
	model := requestModel(m.defaultModel, req.Model)
	resp, err := m.next.Chat(ctx, req)
	result := "ok"
	if err != nil {
		result = "error"
	}
	recordLLMCall(model, time.Since(start), respUsage(resp), result)
	return resp, err
}

func respUsage(resp *ChatResponse) *TokenUsage {
	if resp == nil {
		return nil
	}
	return resp.Usage
}

func (m *metricsProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	start := time.Now()
	model := requestModel(m.defaultModel, req.Model)

	inner, err := m.next.ChatStream(ctx, req)
	if err != nil {
		llmCalls.WithLabelValues(model, "error").Inc()
		return nil, err
	}

	outer := make(chan StreamEvent, 64)
	go func() {
		defer close(outer)
		var (
			usage *TokenUsage
			done  bool
		)
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
					recordLLMCall(model, time.Since(start), usage, result)
					return
				}
				switch ev.Type {
				case StreamEventDone:
					done = true
					usage = ev.Usage
				}
				select {
				case outer <- ev:
				case <-ctx.Done():
					// 下游停止消费：按停止收尾并排空 inner，
					// 避免 provider 的发送 goroutine 阻塞
					for range inner {
					}
					recordLLMCall(model, time.Since(start), usage, "stopped")
					return
				}
			case <-ctx.Done():
				recordLLMCall(model, time.Since(start), usage, "stopped")
				return
			}
		}
	}()
	return outer, nil
}
