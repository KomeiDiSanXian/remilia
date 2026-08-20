package telemetry

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

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
			return err
		}
	}
}
