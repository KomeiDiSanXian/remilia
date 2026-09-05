package telemetry

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// commands 命令使用计数器（包级单例，幂等注册）。
var commands = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "remilia",
	Name:      "command_total",
	Help:      "命令使用次数（按命令名，子命令以父名计）",
}, []string{"command"})).(*prometheus.CounterVec)

// PrometheusMetrics 提供 Histogram/Counter 采集。
//
// 同一 namespace 的指标只注册一次：重复调用（插件重载、测试多次执行等）
// 复用已注册的采集器，避免 DefaultRegisterer 重复注册 panic。
func PrometheusMetrics(namespace string) context.Middleware {
	requests := inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "handler_requests_total",
		Help:      "Total handler requests",
	}, []string{"event"})).(*prometheus.CounterVec)
	latency := inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "handler_latency_seconds",
		Help:      "Handler latency in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"event"})).(*prometheus.HistogramVec)

	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			start := time.Now()
			err := next(ctx)
			el := time.Since(start)
			evt := ctx.GetEventType()
			requests.WithLabelValues(evt).Inc()
			latency.WithLabelValues(evt).Observe(el.Seconds())
			// 命令使用统计：命令规则在 handler 前已解析并缓存到 ctx。
			if parsed := ctx.GetParsedCommand(); parsed != nil {
				if name := commandName(parsed); name != "" {
					commands.WithLabelValues(name).Inc()
				}
			}
			return err
		}
	}
}

// commandName 从解析结果中提取命令名：优先 Definition.Name，
// 否则取命令路径首段（/ai reset → "ai"）。
func commandName(parsed *command.Parsed) string {
	if parsed.Definition != nil && parsed.Definition.Name != "" {
		return parsed.Definition.Name
	}
	if len(parsed.CommandPath) > 0 && parsed.CommandPath[0] != "" {
		return parsed.CommandPath[0]
	}
	return ""
}
