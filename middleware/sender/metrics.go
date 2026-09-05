package sender

import (
	"context"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus"
)

// 出站发送指标（包级单例，幂等注册；供 Metrics 装饰器与 NewOutboundObserver 共用）。
var (
	sendDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "remilia",
		Name:      "send_duration_seconds",
		Help:      "消息发送耗时",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"platform"})).(*prometheus.HistogramVec)

	sendTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "remilia",
		Name:      "send_total",
		Help:      "消息发送总数",
	}, []string{"platform", "status"})).(*prometheus.CounterVec)
)

// Metrics 包装 Sender，记录发送耗时、成功/失败次数到 Prometheus。
//
// 指标：
//
//	remilia_send_duration_seconds — 发送耗时直方图（标签：platform）
//	remilia_send_total — 发送次数计数器（标签：platform, status）
//
// 注意：直接包装 Sender 会使 platform.Sender 上的可选接口断言失效
// （SessionNotifier、GroupManager 等）。出站指标的主流接线方式是
// [NewOutboundObserver]（经 ctx.Reply 的观察者机制，不包装 sender）。
func Metrics(namespace string) SenderDecorator {
	_ = namespace // 指标名固定为 remilia_ 前缀（与观察者共用同一族）
	return func(next platform.Sender) platform.Sender {
		return metricsSender{next: next, platform: "direct", duration: sendDuration, total: sendTotal}
	}
}

type metricsSender struct {
	next     platform.Sender
	platform string
	duration *prometheus.HistogramVec
	total    *prometheus.CounterVec
}

func (s metricsSender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	start := time.Now()
	res, err := s.next.Send(ctx, req)
	elapsed := time.Since(start)

	s.duration.WithLabelValues(s.platform).Observe(elapsed.Seconds())

	if err != nil {
		s.total.WithLabelValues(s.platform, "error").Inc()
	} else {
		s.total.WithLabelValues(s.platform, "success").Inc()
	}

	return res, err
}

// outboundObserver 通过 ctx.Reply 的观察者机制记录出站发送指标。
// 不包装 Sender，因此不影响平台可选接口断言；仅覆盖经 ctx.Reply* 的发送
// （插件绕过 ctx.Reply 直接调 Sender 的路径不会被观察到）。
type outboundObserver struct {
	platform string
}

// NewOutboundObserver 创建记录出站发送指标的 [eventctx.OutboundObserver]。
//
// 用法：中间件内按事件注入（platform 取当前事件的平台）：
//
//	ctx.Ext().SetTyped(eventctx.OutboundObserverExt{Observer: sender.NewOutboundObserver(plt)})
//
// 指标与 [Metrics] 共用同一族（remilia_send_total / remilia_send_duration_seconds）。
func NewOutboundObserver(plt string) eventctx.OutboundObserver {
	if plt == "" {
		plt = "unknown"
	}
	return outboundObserver{platform: plt}
}

func (o outboundObserver) OnOutbound(_ string, _ platform.SendRequest, _ platform.SendResult, err error) {
	status := "success"
	if err != nil {
		status = "error"
		logger.WithField("platform", o.platform).WithError(err).Debug("[Sender] outbound failed (observed)")
	}
	sendTotal.WithLabelValues(o.platform, status).Inc()
}

// Logging 包装 Sender，记录每次发送的耗时和结果到结构化日志。
func Logging() SenderDecorator {
	return func(next platform.Sender) platform.Sender {
		return loggingSender{next: next}
	}
}

type loggingSender struct {
	next platform.Sender
}

func (s loggingSender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	start := time.Now()
	res, err := s.next.Send(ctx, req)

	fields := logger.Fields{
		"target":  req.Target.ID,
		"latency": time.Since(start).Milliseconds(),
	}
	if req.Target.IsGroup {
		fields["group"] = req.Target.ID
	}
	if err != nil {
		logger.WithFields(fields).WithError(err).Warn("[Sender] Send failed")
	} else {
		logger.WithFields(fields).Debug("[Sender] Send success")
	}

	return res, err
}
