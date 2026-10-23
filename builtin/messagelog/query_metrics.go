// query_metrics.go — 消息查询耗时指标。
//
// 群聊窗口与相关历史检索每轮都会查一次最近消息：热缓存命中时接近零成本，
// 退化到 SQLite 时则可能被写锁拖慢。按来源（memory/sqlite）分开看，才能
// 判断该调缓存还是查库。
package messagelog

import (
	"time"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var queryDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "messagelog",
		Name:      "query_duration_seconds",
		Help:      "最近消息查询耗时（source: memory=热缓存命中，sqlite=回源数据库）",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	},
	[]string{"source"},
)).(*prometheus.HistogramVec)

// recordQueryDuration 记录一次查询耗时。
func recordQueryDuration(source string, d time.Duration) {
	queryDuration.WithLabelValues(source).Observe(d.Seconds())
}
