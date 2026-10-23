// download_metrics.go — 附件下载耗时指标。
//
// 附件下载走的是外部图床：慢与失败会直接拖慢回合（等图）并触发重试。
// 这里只测 HTTP 往返（拿到响应头），不含落盘与解码，便于区分"网络慢"
// 与"写盘慢"。
package attachments

import (
	"time"

	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var downloadDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "messagelog",
		Name:      "attachments_download_duration_seconds",
		Help:      "附件下载 HTTP 往返耗时（result: ok/status/error）",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30},
	},
	[]string{"result"},
)).(*prometheus.HistogramVec)

// recordDownloadDuration 记录一次下载往返。
func recordDownloadDuration(result string, d time.Duration) {
	downloadDuration.WithLabelValues(result).Observe(d.Seconds())
}
