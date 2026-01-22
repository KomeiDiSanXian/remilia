package middleware

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PrometheusMetrics 提供 Histogram/Counter 采集
func PrometheusMetrics(namespace string) context.Middleware {
	requests := promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "handler_requests_total",
		Help:      "Total handler requests",
	}, []string{"event"})
	latency := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "handler_latency_seconds",
		Help:      "Handler latency in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"event"})

	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			start := time.Now()
			err := next(ctx)
			el := time.Since(start)
			evt := string(ctx.GetEventType())
			requests.WithLabelValues(evt).Inc()
			latency.WithLabelValues(evt).Observe(el.Seconds())
			return err
		}
	}
}
