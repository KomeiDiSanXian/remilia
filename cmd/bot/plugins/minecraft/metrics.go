package minecraft

import (
	"github.com/KomeiDiSanXian/remilia/infra/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

// minecraft 插件指标（模仿 builtin/messagelog 的注册惯例：
// 包级变量 + MustRegisterOrGet 幂等注册到 DefaultRegisterer，
// 由 main.go 的 /metrics 端点暴露）。
var (
	mcQueriesTotal = metrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "minecraft",
			Name:      "queries_total",
			Help:      "Minecraft 服务器查询次数（result: ok/offline/error；via: slp/raknet/api；edition: java/bedrock）",
		},
		[]string{"result", "via", "edition"},
	)).(*prometheus.CounterVec)

	mcQueryDuration = metrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "minecraft",
			Name:      "query_duration_seconds",
			Help:      "Minecraft 服务器查询耗时（含直连与 API 回退，不含缓存命中）",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20},
		},
		[]string{"edition"},
	)).(*prometheus.HistogramVec)

	mcCacheHitsTotal = metrics.MustRegisterOrGet(nil, prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "minecraft",
			Name:      "cache_hits_total",
			Help:      "查询结果缓存命中次数",
		},
	)).(prometheus.Counter)

	mcGS4Total = metrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "minecraft",
			Name:      "gs4_queries_total",
			Help:      "GS4 Query 尝试次数（result: ok/failed/skipped）",
		},
		[]string{"result"},
	)).(*prometheus.CounterVec)
)

// recordQuery 记录一次查询结果（result: ok/offline/error）。
func recordQuery(edition, via, result string) {
	mcQueriesTotal.WithLabelValues(result, via, edition).Inc()
}
