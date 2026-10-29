package mcp

import (
	"time"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

// metrics.go — 外部工具服务器的 Prometheus 指标（namespace "mcp"）。
//
// 这组指标服务于两个目的：
//
//   - 运行健康：连接状态、连接/重连次数、每服务器工具数、软不可用调用次数；
//   - 抖动治理：工具列表变化按"采纳 / 抖动丢弃"分开计数。
//
// 工具段一变，其后的提示词前缀缓存全部失效。因此
// mcp_tool_list_changes_total{result="adopted"} 直接对应一次缓存失效，
// {result="jitter"} 则是稳定窗口拦下的、本会发生的失效。把两者与
// ai_llm_tokens_total{type="prompt_cached"} 的命中率并列，可定量判断稳定
// 策略是否真的减少了整段历史的重复计费。
//
// 指标只在真的发生时记账：无变化的刷新不产生任何样本，避免稀释信号。
var (
	mcpServerUp = inframetrics.MustRegisterOrGet(nil, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "mcp",
			Name:      "server_up",
			Help:      "外部工具服务器当前是否已连接（1=已连接，0=未连接）",
		},
		[]string{"server"},
	)).(*prometheus.GaugeVec)

	mcpConnects = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "connects_total",
			Help:      "连接（握手 + tools/list）尝试次数（result: ok/error）",
		},
		[]string{"server", "result"},
	)).(*prometheus.CounterVec)

	mcpReconnects = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "reconnects_total",
			Help:      "连接断开后触发的重连次数",
		},
		[]string{"server"},
	)).(*prometheus.CounterVec)

	mcpTools = inframetrics.MustRegisterOrGet(nil, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "mcp",
			Name:      "tools",
			Help:      "该服务器当前物化到目录的工具数量",
		},
		[]string{"server"},
	)).(*prometheus.GaugeVec)

	mcpToolListChanges = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "tool_list_changes_total",
			Help:      "工具列表变化处理结果（result: adopted 采纳并推进代数 / jitter 视为抖动丢弃）",
		},
		[]string{"server", "result"},
	)).(*prometheus.CounterVec)

	mcpRequests = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "requests_total",
			Help:      "JSON-RPC 请求次数（method: initialize/tools/list/tools/call，result: ok/error）",
		},
		[]string{"server", "method", "result"},
	)).(*prometheus.CounterVec)

	mcpRequestLatency = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "mcp",
			Name:      "request_duration_seconds",
			Help:      "JSON-RPC 请求耗时（含工具执行本身）",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"server", "method"},
	)).(*prometheus.HistogramVec)

	mcpNotifications = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "notifications_total",
			Help:      "收到的服务器通知次数（method 为通知方法名）",
		},
		[]string{"server", "method"},
	)).(*prometheus.CounterVec)

	mcpUnavailableCalls = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "unavailable_calls_total",
			Help:      "软不可用（连接断开）时被调用的工具次数——这是稳定优先取舍付出的代价",
		},
		[]string{"server"},
	)).(*prometheus.CounterVec)

	mcpProtocolNegotiations = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "protocol_negotiations_total",
			Help:      "成功建立连接时生效的 MCP 协议版本分布（version 为具体版本号）",
		},
		[]string{"server", "version"},
	)).(*prometheus.CounterVec)

	mcpProtocolFallbacks = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mcp",
			Name:      "protocol_fallbacks_total",
			Help:      "无状态流程探测未能成立（对端为握手时代或未声明无状态版本）而回退到 initialize 的次数",
		},
		[]string{"server"},
	)).(*prometheus.CounterVec)
)

// recordServerUp 记录服务器连接状态。
func recordServerUp(server string, up bool) {
	v := 0.0
	if up {
		v = 1
	}
	mcpServerUp.WithLabelValues(server).Set(v)
}

// recordServerTools 记录服务器当前物化的工具数量。
func recordServerTools(server string, n int) {
	mcpTools.WithLabelValues(server).Set(float64(n))
}

// recordConnect 记录一次连接尝试的结果。
func recordConnect(server, result string) {
	mcpConnects.WithLabelValues(server, result).Inc()
}

// recordReconnect 记录一次由断开触发的重连。
func recordReconnect(server string) {
	mcpReconnects.WithLabelValues(server).Inc()
}

// recordToolListChange 记录工具列表变化的处理结果（adopted / jitter）。
func recordToolListChange(server, result string) {
	mcpToolListChanges.WithLabelValues(server, result).Inc()
}

// recordRPCRequest 记录一次 JSON-RPC 请求的次数与耗时。
func recordRPCRequest(server, method string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	mcpRequests.WithLabelValues(server, method, result).Inc()
	mcpRequestLatency.WithLabelValues(server, method).Observe(d.Seconds())
}

// recordRPCNotification 记录一次服务器通知。
func recordRPCNotification(server, method string) {
	mcpNotifications.WithLabelValues(server, method).Inc()
}

// recordUnavailableCall 记录一次"软不可用时的调用"。
func recordUnavailableCall(server string) {
	mcpUnavailableCalls.WithLabelValues(server).Inc()
}

// recordProtocolNegotiation 记录一次成功连接后生效的协议版本。
func recordProtocolNegotiation(server, version string) {
	if version == "" {
		return
	}
	mcpProtocolNegotiations.WithLabelValues(server, version).Inc()
}

// recordProtocolFallback 记录一次"探测无状态流程失败、回退到握手流程"。
func recordProtocolFallback(server string) {
	mcpProtocolFallbacks.WithLabelValues(server).Inc()
}
