// backpressure_metrics.go — 背压中间件的丢弃指标。
//
// 背压是唯一会"静默丢事件"的中间件：drop 直接返回错误、trywait 超时也直接
// 返回，事件不会再进入 handler。没有计数时，用户侧表现为"消息没反应"，
// 日志里只有一条 Warn。按策略分桶便于判断该调 limits 还是换策略。
package middleware

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var backpressureDropped = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "remilia",
		Name:      "backpressure_dropped_total",
		Help:      "背压中间件丢弃的事件数（policy: drop/block/trywait）",
	},
	[]string{"policy"},
)).(*prometheus.CounterVec)

// recordBackpressureDrop 记录一次背压丢弃。
func recordBackpressureDrop(policy string) {
	backpressureDropped.WithLabelValues(policy).Inc()
}
