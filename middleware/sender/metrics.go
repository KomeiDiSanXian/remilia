package sender

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics 包装 Sender，记录发送耗时、成功/失败次数到 Prometheus。
//
// namespace 用于区分不同业务线，如 "remilia"、"admin"。
//
// 指标：
//
//	{namespace}_send_duration_seconds — 发送耗时直方图
//	{namespace}_send_total — 发送次数计数器（标签：status=success|error）
func Metrics(namespace string) SenderDecorator {
	labels := prometheus.Labels{"namespace": namespace}

	duration := inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "send_duration_seconds",
		Help:      "消息发送耗时",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{})).(*prometheus.HistogramVec)

	total := inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "send_total",
		Help:      "消息发送总数",
	}, []string{"status"})).(*prometheus.CounterVec)

	return func(next platform.Sender) platform.Sender {
		return metricsSender{next: next, labels: labels, duration: duration, total: total}
	}
}

type metricsSender struct {
	next     platform.Sender
	labels   prometheus.Labels
	duration *prometheus.HistogramVec
	total    *prometheus.CounterVec
}

func (s metricsSender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	start := time.Now()
	res, err := s.next.Send(ctx, req)
	elapsed := time.Since(start)

	s.duration.WithLabelValues().Observe(elapsed.Seconds())

	if err != nil {
		s.total.WithLabelValues("error").Inc()
	} else {
		s.total.WithLabelValues("success").Inc()
	}

	return res, err
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
