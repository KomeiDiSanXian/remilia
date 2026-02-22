package metrics

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Collector collects prometheus metrics.
type Collector struct {
	namespace string
	registry  prometheus.Registerer // Custom registry to avoid global collisions

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

	// Bot 业务层指标
	botUptime          prometheus.Gauge         // Bot 运行状态（1=运行中，0=停止）
	commandInvocations *prometheus.CounterVec   // 命令调用次数，按命令名和状态分类
	messageSent        *prometheus.CounterVec   // 消息发送次数，按类型和状态分类
	messageLatency     *prometheus.HistogramVec // 消息发送延迟

	// Use atomic types for thread-safe access
	internalPoolGets atomic.Uint64
	internalPoolPuts atomic.Uint64
	internalPoolNews atomic.Uint64
}

// NewMetricsCollector 创建使用独立 Prometheus Registry 的指标收集器。
//
// 改进 3.10：不再使用 prometheus.DefaultRegisterer，而是为每个 Collector 创建
// 独立的 Registry，彻底避免同一进程多次调用时因 metric 名称重复导致的 panic。
//
// 如果需要将指标暴露到 /metrics 端点，请使用 Collector.Registry() 获取 registry
// 并注册到 http handler：
//
//	mc := metrics.NewMetricsCollector("mybot")
//	http.Handle("/metrics", promhttp.HandlerFor(mc.Registry(), promhttp.HandlerOpts{}))
func NewMetricsCollector(namespace string) *Collector {
	return NewMetricsCollectorWithRegistry(namespace, prometheus.NewRegistry())
}

// Registry 返回该 Collector 使用的 Prometheus Registry。
// 可用于将指标暴露到自定义的 /metrics HTTP 端点。
func (mc *Collector) Registry() prometheus.Gatherer {
	if g, ok := mc.registry.(prometheus.Gatherer); ok {
		return g
	}
	return prometheus.DefaultGatherer
}

// NewMetricsCollectorWithRegistry creates a new metrics collector with a custom registry
// This allows multiple collectors in tests or multi-engine scenarios without panic
func NewMetricsCollectorWithRegistry(namespace string, registry prometheus.Registerer) *Collector {
	if namespace == "" {
		namespace = "remilia"
	}
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	mc := &Collector{
		namespace: namespace,
		registry:  registry,
	}

	// Use the custom registry instead of promauto (which uses global registry)
	factory := promauto.With(registry)

	mc.deadLetterQueueSize = factory.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "deadletter_queue_size",
		Help:      "Current number of items in dead letter queue",
	})

	mc.deadLetterConsumed = factory.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "deadletter_consumed_total",
		Help:      "Total number of dead letter items consumed",
	})

	mc.deadLetterConsumerTime = factory.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "deadletter_consumer_duration_seconds",
		Help:      "Time spent consuming dead letter items",
		Buckets:   prometheus.DefBuckets,
	})

	mc.pluginHandlers = factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "plugin_handlers_total",
		Help:      "Number of handlers registered by plugin",
	}, []string{"plugin"})

	mc.pluginMatchers = factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "plugin_matchers_total",
		Help:      "Number of matchers registered by plugin",
	}, []string{"plugin"})

	mc.pluginLoadTime = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "plugin_load_duration_seconds",
		Help:      "Time spent loading plugin",
		Buckets:   prometheus.DefBuckets,
	}, []string{"plugin"})

	mc.pluginUnloadTime = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "plugin_unload_duration_seconds",
		Help:      "Time spent unloading plugin",
		Buckets:   prometheus.DefBuckets,
	}, []string{"plugin"})

	mc.retryAttempts = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_attempts_total",
		Help:      "Total number of retry attempts",
	}, []string{"attempt"})

	mc.retrySuccesses = factory.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_successes_total",
		Help:      "Total number of successful retries",
	})

	mc.retryFailures = factory.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "retry_failures_total",
		Help:      "Total number of failed retries (entered dead letter)",
	})

	mc.retryDelay = factory.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "retry_delay_seconds",
		Help:      "Delay before retry attempt",
		Buckets:   []float64{0.1, 0.2, 0.5, 1, 2, 5, 10},
	})

	mc.eventProcessed = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_processed_total",
		Help:      "Total number of events processed",
	}, []string{"type", "source"})

	mc.eventDropped = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_dropped_total",
		Help:      "Total number of events dropped",
	}, []string{"reason"})

	mc.eventLatency = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "event_processing_duration_seconds",
		Help:      "Time spent processing event",
		Buckets:   prometheus.DefBuckets,
	}, []string{"type"})

	// Bot 业务层指标
	mc.botUptime = factory.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bot_up",
		Help:      "Bot running status (1 = running, 0 = stopped)",
	})

	mc.commandInvocations = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "command_invocations_total",
		Help:      "Total number of command invocations",
	}, []string{"command", "status"})

	mc.messageSent = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "messages_sent_total",
		Help:      "Total number of messages sent",
	}, []string{"type", "status"})

	mc.messageLatency = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "message_send_duration_seconds",
		Help:      "Time spent sending a message",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"type"})

	return mc
}

func (mc *Collector) SetDeadLetterQueueSize(size int) {
	mc.deadLetterQueueSize.Set(float64(size))
}

func (mc *Collector) RecordDeadLetterConsumed(duration time.Duration) {
	mc.deadLetterConsumed.Inc()
	mc.deadLetterConsumerTime.Observe(duration.Seconds())
}

func (mc *Collector) SetPluginHandlers(plugin string, count int) {
	mc.pluginHandlers.WithLabelValues(plugin).Set(float64(count))
}

func (mc *Collector) SetPluginMatchers(plugin string, count int) {
	mc.pluginMatchers.WithLabelValues(plugin).Set(float64(count))
}

func (mc *Collector) RecordPluginLoad(plugin string, duration time.Duration) {
	mc.pluginLoadTime.WithLabelValues(plugin).Observe(duration.Seconds())
}

func (mc *Collector) RecordPluginUnload(plugin string, duration time.Duration) {
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

func (mc *Collector) RecordRetryAttempt(attempt int, delay time.Duration) {
	mc.retryAttempts.WithLabelValues(FormatAttempt(attempt)).Inc()
	mc.retryDelay.Observe(delay.Seconds())
}

func (mc *Collector) RecordRetrySuccess() { mc.retrySuccesses.Inc() }
func (mc *Collector) RecordRetryFailure() { mc.retryFailures.Inc() }
func (mc *Collector) RecordEventDropped(reason string) {
	mc.eventDropped.WithLabelValues(reason).Inc()
}

func (mc *Collector) RecordEventProcessed(eventType, source string, duration time.Duration) {
	mc.eventProcessed.WithLabelValues(eventType, source).Inc()
	mc.eventLatency.WithLabelValues(eventType).Observe(duration.Seconds())
}

type PoolMetricsSnapshot struct {
	Gets    uint64
	News    uint64
	HitRate float64
}

func (mc *Collector) GetPoolMetrics() PoolMetricsSnapshot {
	gets := mc.internalPoolGets.Load()
	news := mc.internalPoolNews.Load()

	hitRate := 0.0
	if gets > 0 {
		hitRate = float64(gets-news) / float64(gets)
	}

	return PoolMetricsSnapshot{Gets: gets, News: news, HitRate: hitRate}
}

// --- Bot 业务层指标 ---

// SetBotUp 设置 Bot 运行状态（true=运行中，false=已停止）
func (mc *Collector) SetBotUp(running bool) {
	if running {
		mc.botUptime.Set(1)
	} else {
		mc.botUptime.Set(0)
	}
}

// RecordCommandInvocation 记录命令调用
// status: "success" | "failure" | "rejected"
func (mc *Collector) RecordCommandInvocation(command, status string) {
	mc.commandInvocations.WithLabelValues(command, status).Inc()
}

// RecordMessageSent 记录消息发送
// msgType: "text" | "markdown" | "embed" 等
// status: "success" | "failure"
func (mc *Collector) RecordMessageSent(msgType, status string, duration time.Duration) {
	mc.messageSent.WithLabelValues(msgType, status).Inc()
	if duration > 0 {
		mc.messageLatency.WithLabelValues(msgType).Observe(duration.Seconds())
	}
}
