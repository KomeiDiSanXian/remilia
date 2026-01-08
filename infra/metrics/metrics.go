package metrics

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector collects prometheus metrics.
type MetricsCollector struct {
	namespace string

	deadLetterQueueSize    prometheus.Gauge
	deadLetterConsumed     prometheus.Counter
	deadLetterConsumerTime prometheus.Histogram

	pluginHandlers   *prometheus.GaugeVec
	pluginMatchers   *prometheus.GaugeVec
	pluginLoadTime   *prometheus.HistogramVec
	pluginUnloadTime *prometheus.HistogramVec

	retryAttempts  *prometheus.CounterVec
	retrySuccesses prometheus.Counter
	retryFailures  prometheus.Counter
	retryDelay     prometheus.Histogram

	eventProcessed *prometheus.CounterVec
	eventDropped   *prometheus.CounterVec
	eventLatency   *prometheus.HistogramVec

	internalPoolGets uint64
	internalPoolPuts uint64
	internalPoolNews uint64
}

func NewMetricsCollector(namespace string) *MetricsCollector {
	if namespace == "" {
		namespace = "remilia"
	}

	mc := &MetricsCollector{namespace: namespace}

	mc.deadLetterQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "deadletter_queue_size",
		Help:      "Current number of items in dead letter queue",
	})

	mc.deadLetterConsumed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "deadletter_consumed_total",
		Help:      "Total number of dead letter items consumed",
	})

	mc.deadLetterConsumerTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "deadletter_consumer_duration_seconds",
		Help:      "Time spent consuming dead letter items",
		Buckets:   prometheus.DefBuckets,
	})

	mc.pluginHandlers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "plugin_handlers_total",
		Help:      "Number of handlers registered by plugin",
	}, []string{"plugin"})

	mc.pluginMatchers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "plugin_matchers_total",
		Help:      "Number of matchers registered by plugin",
	}, []string{"plugin"})

	mc.pluginLoadTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "plugin_load_duration_seconds",
		Help:      "Time spent loading plugin",
		Buckets:   prometheus.DefBuckets,
	}, []string{"plugin"})

	mc.pluginUnloadTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "plugin_unload_duration_seconds",
		Help:      "Time spent unloading plugin",
		Buckets:   prometheus.DefBuckets,
	}, []string{"plugin"})

	mc.retryAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_attempts_total",
		Help:      "Total number of retry attempts",
	}, []string{"attempt"})

	mc.retrySuccesses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_successes_total",
		Help:      "Total number of successful retries",
	})

	mc.retryFailures = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_failures_total",
		Help:      "Total number of failed retries (entered dead letter)",
	})

	mc.retryDelay = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "retry_delay_seconds",
		Help:      "Delay before retry attempt",
		Buckets:   []float64{0.1, 0.2, 0.5, 1, 2, 5, 10},
	})

	mc.eventProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_processed_total",
		Help:      "Total number of events processed",
	}, []string{"type", "source"})

	mc.eventDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_dropped_total",
		Help:      "Total number of events dropped",
	}, []string{"reason"})

	mc.eventLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "event_processing_duration_seconds",
		Help:      "Time spent processing event",
		Buckets:   prometheus.DefBuckets,
	}, []string{"type"})

	return mc
}

func (mc *MetricsCollector) SetDeadLetterQueueSize(size int) {
	mc.deadLetterQueueSize.Set(float64(size))
}

func (mc *MetricsCollector) RecordDeadLetterConsumed(duration time.Duration) {
	mc.deadLetterConsumed.Inc()
	mc.deadLetterConsumerTime.Observe(duration.Seconds())
}

func (mc *MetricsCollector) SetPluginHandlers(plugin string, count int) {
	mc.pluginHandlers.WithLabelValues(plugin).Set(float64(count))
}

func (mc *MetricsCollector) SetPluginMatchers(plugin string, count int) {
	mc.pluginMatchers.WithLabelValues(plugin).Set(float64(count))
}

func (mc *MetricsCollector) RecordPluginLoad(plugin string, duration time.Duration) {
	mc.pluginLoadTime.WithLabelValues(plugin).Observe(duration.Seconds())
}

func (mc *MetricsCollector) RecordPluginUnload(plugin string, duration time.Duration) {
	mc.pluginUnloadTime.WithLabelValues(plugin).Observe(duration.Seconds())
}

// FormatAttempt formats retry attempt label.
func FormatAttempt(attempt int) string {
	if attempt <= 0 {
		return "0"
	}
	if attempt >= 10 {
		return "10+"
	}
	return fmt.Sprint(attempt)
}

func (mc *MetricsCollector) RecordRetryAttempt(attempt int, delay time.Duration) {
	mc.retryAttempts.WithLabelValues(FormatAttempt(attempt)).Inc()
	mc.retryDelay.Observe(delay.Seconds())
}

func (mc *MetricsCollector) RecordRetrySuccess() { mc.retrySuccesses.Inc() }
func (mc *MetricsCollector) RecordRetryFailure() { mc.retryFailures.Inc() }
func (mc *MetricsCollector) RecordEventDropped(reason string) {
	mc.eventDropped.WithLabelValues(reason).Inc()
}

func (mc *MetricsCollector) RecordEventProcessed(eventType, source string, duration time.Duration) {
	mc.eventProcessed.WithLabelValues(eventType, source).Inc()
	mc.eventLatency.WithLabelValues(eventType).Observe(duration.Seconds())
}

type PoolMetricsSnapshot struct {
	Gets    uint64
	News    uint64
	HitRate float64
}

func (mc *MetricsCollector) GetPoolMetrics() PoolMetricsSnapshot {
	gets := atomic.LoadUint64(&mc.internalPoolGets)
	news := atomic.LoadUint64(&mc.internalPoolNews)

	hitRate := 0.0
	if gets > 0 {
		hitRate = float64(gets-news) / float64(gets)
	}

	return PoolMetricsSnapshot{Gets: gets, News: news, HitRate: hitRate}
}
