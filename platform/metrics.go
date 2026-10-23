// metrics.go — 平台适配器可观测性。
//
// 适配器健康是"进程活着但收不到消息"这类故障的第一现场：断连、致命错误、
// 适配器 goroutine 退出都只发生在适配器内部。Registry 提供了 AdapterObserver
// 钩子，这里给出一个可直接接线的实现，配合
// platform.NewRegistry().WithObserver(platform.MetricsObserver()) 使用。
package platform

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	adapterUp = inframetrics.MustRegisterOrGet(nil, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "remilia",
			Name:      "adapter_up",
			Help:      "平台适配器运行状态（1=运行中，0=已停止）",
		},
		[]string{"platform"},
	)).(*prometheus.GaugeVec)

	adapterDisconnects = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "adapter_disconnects_total",
			Help:      "适配器意外断连次数（可恢复，Registry 会重建连接）",
		},
		[]string{"platform"},
	)).(*prometheus.CounterVec)

	adapterErrors = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "adapter_fatal_errors_total",
			Help:      "适配器以致命错误退出次数（进程还活着但该平台已停止工作）",
		},
		[]string{"platform"},
	)).(*prometheus.CounterVec)
)

// MetricsObserver 返回一个把适配器生命周期写入 Prometheus 的观察者。
//
// 实现应保持轻量（只做 label 查找与计数），与 AdapterObserver 的
// "回调内不做阻塞操作"约定一致。
func MetricsObserver() AdapterObserver { return metricsObserver{} }

type metricsObserver struct{}

// OnAdapterStarted 标记适配器运行中。
func (metricsObserver) OnAdapterStarted(p string) { adapterUp.WithLabelValues(p).Set(1) }

// OnAdapterStopped 标记适配器已停止。
func (metricsObserver) OnAdapterStopped(p string) { adapterUp.WithLabelValues(p).Set(0) }

// OnAdapterError 记录致命错误退出。
func (metricsObserver) OnAdapterError(p, _ string) {
	adapterUp.WithLabelValues(p).Set(0)
	adapterErrors.WithLabelValues(p).Inc()
}

// OnAdapterDisconnect 记录可恢复的意外断连。
func (metricsObserver) OnAdapterDisconnect(p string, _ error) {
	adapterDisconnects.WithLabelValues(p).Inc()
}
