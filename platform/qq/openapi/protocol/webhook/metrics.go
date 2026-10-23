// metrics.go — Webhook 入口指标。
//
// 回调入口是"配置错了但进程看起来正常"的高发区：签名密钥不一致会稳定返回
// 401，QQ 侧则停止投递。把请求数按状态码分桶，能让这类问题在日志被刷掉后
// 仍然可见。
package webhook

import (
	"net/http"
	"strconv"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var requests = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "remilia",
		Name:      "webhook_requests_total",
		Help:      "Webhook 回调请求数（按平台与 HTTP 状态码；401 通常意味着签名密钥不一致）",
	},
	[]string{"platform", "status"},
)).(*prometheus.CounterVec)

// recordRequest 记录一次回调请求（status 为实际写出的状态码）。
func recordRequest(platform string, status int) {
	requests.WithLabelValues(platform, strconv.Itoa(status)).Inc()
}

// statusRecorder 记录 handler 实际写出的状态码（http.Error 与 WriteHeader 均经过它）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
