package attachments

import (
	"github.com/prometheus/client_golang/prometheus"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
)

// attMetrics 附件生命周期指标（前缀 messagelog_attachments_）。
type attMetrics struct {
	downloadsTotal       prometheus.Counter
	downloadsReadyTotal  prometheus.Counter
	downloadsFailedTotal prometheus.Counter
	queueDepth           prometheus.Gauge
	diskUsageBytes       prometheus.Gauge
	pendingGauge         prometheus.Gauge
	readyGauge           prometheus.Gauge
	retryGauge           prometheus.Gauge
	lazyGauge            prometheus.Gauge
	failedGauge          prometheus.Gauge
	expiredGauge         prometheus.Gauge
	deletedGauge         prometheus.Gauge
	gcRunsTotal          prometheus.Counter
	gcDeletedTotal       prometheus.Counter
}

var metricsInst = newAttMetrics()

func register(reg prometheus.Registerer, c prometheus.Collector) prometheus.Collector {
	return inframetrics.MustRegisterOrGet(reg, c)
}

func newAttMetrics() *attMetrics {
	return &attMetrics{
		downloadsTotal: register(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_attachments_downloads_total", Help: "Attachment download attempts.",
		})).(prometheus.Counter),
		downloadsReadyTotal: register(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_attachments_downloads_ready_total", Help: "Attachment downloads succeeded.",
		})).(prometheus.Counter),
		downloadsFailedTotal: register(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_attachments_downloads_failed_total", Help: "Attachment download failures.",
		})).(prometheus.Counter),
		queueDepth: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_queue_depth", Help: "Pending attachment download tasks in memory queue.",
		})).(prometheus.Gauge),
		diskUsageBytes: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_disk_usage_bytes", Help: "Attachment store disk usage in bytes.",
		})).(prometheus.Gauge),
		pendingGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_pending", Help: "Attachment rows with status pending.",
		})).(prometheus.Gauge),
		readyGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_ready", Help: "Attachment rows with status ready.",
		})).(prometheus.Gauge),
		retryGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_pending_retry", Help: "Attachment rows with status pending_retry.",
		})).(prometheus.Gauge),
		lazyGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_pending_lazy", Help: "Attachment rows with status pending_lazy.",
		})).(prometheus.Gauge),
		failedGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_failed", Help: "Attachment rows with status failed.",
		})).(prometheus.Gauge),
		expiredGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_expired", Help: "Attachment rows with status expired.",
		})).(prometheus.Gauge),
		deletedGauge: register(nil, prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "messagelog_attachments_status_deleted", Help: "Attachment rows with status deleted.",
		})).(prometheus.Gauge),
		gcRunsTotal: register(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_attachments_gc_runs_total", Help: "Attachment GC runs.",
		})).(prometheus.Counter),
		gcDeletedTotal: register(nil, prometheus.NewCounter(prometheus.CounterOpts{
			Name: "messagelog_attachments_gc_deleted_total", Help: "Attachment binaries deleted by GC.",
		})).(prometheus.Counter),
	}
}
