package engine

// metrics.go — 引擎层 Prometheus 指标（事件入口画像）。
//
// 指标：{namespace}_events_received_total{platform, kind}
// 在 ProcessPlatformEvent* 各入口统一打点，覆盖真实平台事件
//（引擎内部合成事件走 ProcessEvent，不计入入口画像）。

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus"
)

var eventsReceived = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "remilia",
		Name:      "events_received_total",
		Help:      "接收的平台事件数（按平台与事件类型）",
	},
	[]string{"platform", "kind"},
)).(*prometheus.CounterVec)

// recordEventReceived 记录一条平台事件入口。
func recordEventReceived(event platform.Event) {
	plt, kind := event.Platform(), string(event.Kind())
	if plt == "" {
		plt = "unknown"
	}
	if kind == "" {
		kind = "unknown"
	}
	eventsReceived.WithLabelValues(plt, kind).Inc()
}
