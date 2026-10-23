// metrics.go — 访问凭据刷新指标。
//
// Token 刷新失败不会让进程退出，但会让平台侧停止投递/拒绝调用，表现为
// "机器人还活着但不说话"。失败次数必须可见。
package token

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var refreshFailures = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "remilia",
		Name:      "token_refresh_failures_total",
		Help:      "访问凭据刷新失败次数（进程仍在运行，但该平台的 API 调用会失败）",
	},
	[]string{"platform"},
)).(*prometheus.CounterVec)

// recordRefreshFailure 记录一次刷新失败。
func recordRefreshFailure(platform string) {
	refreshFailures.WithLabelValues(platform).Inc()
}
