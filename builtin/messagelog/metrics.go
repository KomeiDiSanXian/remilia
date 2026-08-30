package messagelog

import (
	"github.com/prometheus/client_golang/prometheus"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
)

// mlMetrics 包级单例指标（复用注册，避免重复注册 panic）。
//
// 指标命名前缀统一 messagelog_，与插件注册名保持一致。
type mlMetrics struct {
	recordsTotal         prometheus.Counter
	flushTotal           prometheus.Counter
	flushFailedTotal     prometheus.Counter
	queueDepth           prometheus.Gauge
	queueDroppedTotal    prometheus.Counter
	spoolSizeBytes       prometheus.Gauge
	spoolRecords         prometheus.Gauge
	spoolOldestAge       prometheus.Gauge
	spoolWriteFailures   prometheus.Counter
	dbWriteLatency       prometheus.Histogram
	cacheEntries         prometheus.Gauge
	cacheEvictions       prometheus.Counter
	recordedDroppedTotal prometheus.Counter
}

func mustRegister(reg prometheus.Registerer, c prometheus.Collector) prometheus.Collector {
	return inframetrics.MustRegisterOrGet(reg, c)
}

var mlMetricsInst = newMLMetrics()

func newMLMetrics() *mlMetrics {
	return &mlMetrics{
		recordsTotal: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_records_total", Help: "Messages recorded (enqueued to durable queue).",
		})).(prometheus.Counter),
		flushTotal: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_flush_total", Help: "Successful SQLite flush transactions.",
		})).(prometheus.Counter),
		flushFailedTotal: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_flush_failed_total", Help: "Failed SQLite flush transactions (records kept in spool).",
		})).(prometheus.Counter),
		queueDepth: mustRegister(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_queue_depth", Help: "Pending records in memory queue + unacked spool.",
		})).(prometheus.Gauge),
		queueDroppedTotal: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_queue_dropped_total", Help: "Records dropped (must stay 0 with spool enabled).",
		})).(prometheus.Counter),
		spoolSizeBytes: mustRegister(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_spool_size_bytes", Help: "Spool file size in bytes.",
		})).(prometheus.Gauge),
		spoolRecords: mustRegister(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_spool_records", Help: "Unacked records in spool.",
		})).(prometheus.Gauge),
		spoolOldestAge: mustRegister(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_spool_oldest_age", Help: "Age in seconds of the oldest unacked spool record.",
		})).(prometheus.Gauge),
		spoolWriteFailures: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_spool_write_failures", Help: "Spool append failures.",
		})).(prometheus.Counter),
		dbWriteLatency: mustRegister(nil, prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "messagelog_db_write_latency_seconds", Help: "SQLite flush transaction duration.",
			Buckets: prometheus.DefBuckets,
		})).(prometheus.Histogram),
		cacheEntries: mustRegister(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_cache_entries", Help: "Hot cache entries (bounded).",
		})).(prometheus.Gauge),
		cacheEvictions: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_cache_evictions_total", Help: "Hot cache evictions.",
		})).(prometheus.Counter),
		recordedDroppedTotal: mustRegister(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_recorded_dropped_total", Help: "Recorded events dropped (best-effort; rebuild via ScanAfter).",
		})).(prometheus.Counter),
	}
}
