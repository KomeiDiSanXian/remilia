package engine

// metrics.go — 引擎层 Prometheus 指标（事件画像）。
//
// 两个指标刻意分开，避免把"平台入口流量"与"引擎内部产生的事件"混在一条
// 曲线里——两者的含义完全不同，混在一起后既看不出外部压力，也看不出插件
// 自己制造了多少处理量：
//
//	{namespace}_events_received_total{platform, kind}  平台入口事件
//	{namespace}_events_internal_total{kind}           内部事件（命令执行、定时任务等）
//
// 打点位置在 processEventGuarded —— 所有事件处理路径（ProcessEvent、
// ProcessPlatformEvent、ProcessPlatformEventEx、ProcessPlatformEventSync）
// 最终都会经过这里，因此 Router 转发（参考实现的常规路径）与直接调用引擎
// 两种用法都能被统计。
//
// 平台标识为 "synthetic"（插件用 platform.NewSyntheticEvent 自造）或为空
// （没有绑定平台事件）的走 internal 指标；其余按 platform/kind 计入入口指标。
//
// 每个事件上下文只会被处理一次，因此不需要额外的去重标记。

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	eventsReceived = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "events_received_total",
			Help:      "接收的平台事件数（按平台与事件类型；内部事件见 events_internal_total）",
		},
		[]string{"platform", "kind"},
	)).(*prometheus.CounterVec)

	eventsInternal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "events_internal_total",
			Help:      "引擎处理的内部事件数（按事件类型；synthetic 或未绑定平台的事件）",
		},
		[]string{"kind"},
	)).(*prometheus.CounterVec)

	execPoolFallbacks = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "remilia",
			Name:      "execpool_fallbacks_total",
			Help:      "ExecPool 满时回退到同步执行的事件数（回退会占用调用方 goroutine）",
		},
	)).(prometheus.Counter)

	execPoolDepth = inframetrics.MustRegisterOrGet(nil, prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "remilia",
			Name:      "execpool_queue_depth",
			Help:      "ExecPool 等待队列中的任务数",
		},
	)).(prometheus.Gauge)
)

// recordExecPoolFallback 记录一次"池满回退同步执行"。
func recordExecPoolFallback() { execPoolFallbacks.Inc() }

// recordExecPoolDepth 更新等待队列深度。
func recordExecPoolDepth(n int) { execPoolDepth.Set(float64(n)) }

// recordEventMetrics 记录一条进入引擎处理的事件。
//
// 平台事件与内部事件分别计数：内部事件同样值得观测（命令执行、定时任务、
// 计划推进都会占用处理能力），只是不能和平台入口流量混成一条曲线。
func recordEventMetrics(ctx *context.Context) {
	if ctx == nil {
		return
	}
	kind := ctx.GetEventType()
	if kind == "" {
		kind = "unknown"
	}

	switch plt := ctx.GetEventPlatform(); plt {
	case "", "synthetic":
		eventsInternal.WithLabelValues(kind).Inc()
	default:
		eventsReceived.WithLabelValues(plt, kind).Inc()
	}
}
