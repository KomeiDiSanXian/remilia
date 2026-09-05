package ai

// metrics.go — AI 插件 Prometheus 指标。
//
// 指标族（namespace "ai"）：
//   - ai_llm_calls_total{model, result}       LLM 调用次数（result: ok/error/stopped）
//   - ai_llm_latency_seconds{model}           LLM 调用耗时（含流式全时长）
//   - ai_llm_tokens_total{model, type}        token 用量（type: prompt/completion）
//   - ai_tool_calls_total{tool, result}       工具调用次数（result: ok/error）
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
			Help:      "LLM token 用量（type: prompt/completion）",
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
)

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
