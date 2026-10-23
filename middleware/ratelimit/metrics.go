// metrics.go — 自适应限流器的 Prometheus 指标。
//
// 限流器默认启用：它拒绝请求时如果没有指标，用户侧只会看到"机器人不回消息"，
// 而日志里的 Warn 很容易被刷掉。拒绝按原因分桶（limit=并发已达上限，
// acquire=抢占槽位失败），另有当前生效上限的 gauge 便于回看调节轨迹。
package ratelimit

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	rejected = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "ratelimit_rejected_total",
			Help:      "自适应限流拒绝的请求数（reason: limit=并发达到当前上限，acquire=抢占槽位失败）",
		},
		[]string{"reason"},
	)).(*prometheus.CounterVec)

	limit = inframetrics.MustRegisterOrGet(nil, prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "remilia",
			Name:      "ratelimit_limit",
			Help:      "自适应限流当前生效的并发上限",
		},
	)).(prometheus.Gauge)
)

// recordRejected 记录一次拒绝。
func recordRejected(reason string) { rejected.WithLabelValues(reason).Inc() }

// setLimit 更新当前生效上限。
func setLimit(n int32) { limit.Set(float64(n)) }
