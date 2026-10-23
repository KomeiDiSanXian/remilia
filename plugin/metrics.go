// metrics.go — 插件加载指标。
//
// 加载失败在启动期只表现为一行日志（或一个返回错误）：批量加载时很容易被
// 后续插件的输出淹没。按阶段（config/setup）与原因（config/setup/timeout/panic）
// 分开计数，既能定位"卡在配置校验"还是"卡在 Setup 的外部调用"，也能区分
// 正常的业务错误与 panic。
package plugin

import (
	"context"
	"errors"
	"strings"
	"time"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	loadDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "remilia",
			Name:      "plugin_load_seconds",
			Help:      "插件加载耗时（phase: config=配置构造与校验，setup=Setup 与 matcher 注册）",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"plugin", "phase"},
	)).(*prometheus.HistogramVec)

	loadFailures = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "plugin_load_failures_total",
			Help:      "插件加载失败次数（reason: config/setup/timeout/panic）",
		},
		[]string{"plugin", "reason"},
	)).(*prometheus.CounterVec)
)

// recordLoadDuration 记录一个阶段的加载耗时。
func recordLoadDuration(plugin, phase string, d time.Duration) {
	loadDuration.WithLabelValues(plugin, phase).Observe(d.Seconds())
}

// recordLoadFailure 记录一次加载失败。
func recordLoadFailure(plugin, reason string) {
	loadFailures.WithLabelValues(plugin, reason).Inc()
}

// loadFailureReason 归类加载失败的原因。
func loadFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var panicErr *setupPanicError
	if errors.As(err, &panicErr) {
		return "panic"
	}
	if strings.Contains(err.Error(), "Setup panicked") {
		return "panic"
	}
	return "setup"
}
