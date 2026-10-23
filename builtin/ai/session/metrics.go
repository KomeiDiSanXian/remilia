// metrics.go — 会话层 Prometheus 指标。
//
// 指标族（namespace "ai"）：
//   - ai_session_active                 当前驻留内存的会话数
//   - ai_session_save_failures_total    会话落库失败次数
//   - ai_context_trimmed_messages_total 被历史裁剪丢弃的消息条数
//
// 裁剪是提示词前缀缓存的主要断点之一：每裁一次，其后的历史前缀整体失效。
// 单看命中率无法区分"缓存结构不好"与"窗口太小、一直在裁"，因此单独计数。
package session

import (
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	activeSessions = inframetrics.MustRegisterOrGet(nil, prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "ai",
			Name:      "session_active",
			Help:      "当前驻留内存的会话数（含空闲未过期）",
		},
	)).(prometheus.Gauge)

	saveFailures = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "session_save_failures_total",
			Help:      "会话持久化失败次数（历史不会丢在内存里，但重启后会回退）",
		},
	)).(prometheus.Counter)

	trimmedMessages = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "ai",
			Name:      "context_trimmed_messages_total",
			Help:      "历史裁剪丢弃的消息条数（每次裁剪都会让其后前缀缓存失效）",
		},
	)).(prometheus.Counter)
)

// setActiveSessions 更新驻留会话数（调用方持有 SessionManager 的锁）。
func setActiveSessions(n int) { activeSessions.Set(float64(n)) }

// recordSaveFailure 记录一次会话落库失败。
func recordSaveFailure() { saveFailures.Inc() }

// recordTrimmed 记录历史裁剪丢弃的条数。
func recordTrimmed(n int) {
	if n > 0 {
		trimmedMessages.Add(float64(n))
	}
}
