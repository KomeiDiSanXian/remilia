package outbound

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

// blockedTotal 记录被策略拦截的出站请求。
//
// 指标按 (component, reason) 分解：既要看"哪个功能被拦"，也要看"为什么被拦"，
// 否则误伤与真实攻击在监控上无法区分。测试断言请用 delta，不要断言绝对值
// （包级指标会跨用例累积）。
var blockedTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "outbound",
		Name:      "blocked_total",
		Help:      "出站策略拦截次数（component：调用方；reason：拦截原因）",
	},
	[]string{"component", "reason"},
)).(*prometheus.CounterVec)

// recordBlocked 累加一次拦截。
func recordBlocked(component string, reason Reason) {
	blockedTotal.WithLabelValues(component, string(reason)).Inc()
}

var (
	requests = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "outbound",
			Name:      "requests_total",
			Help:      "出站 HTTP 请求数（result: ok/status_4xx/status_5xx/error）",
		},
		[]string{"component", "result"},
	)).(*prometheus.CounterVec)

	requestDuration = inframetrics.MustRegisterOrGet(nil, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "outbound",
			Name:      "request_duration_seconds",
			Help:      "出站 HTTP 请求耗时（含重定向与重试后的最终一次）",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"component"},
	)).(*prometheus.HistogramVec)
)

// recordRequest 记录一次出站请求的结果与耗时。
func recordRequest(component string, d time.Duration, status int, err error) {
	result := "ok"
	switch {
	case err != nil:
		result = "error"
	case status >= 500:
		result = "status_5xx"
	case status >= 400:
		result = "status_4xx"
	}
	requests.WithLabelValues(component, result).Inc()
	requestDuration.WithLabelValues(component).Observe(d.Seconds())
}

// blockLogInterval 同一 (component, reason) 的告警最小间隔：策略拦截常成批
// 出现（模型一次发十几个抓取），全量打日志会把真正的告警淹掉。
const blockLogInterval = time.Minute

var (
	blockLogMu sync.Mutex
	blockLogAt = make(map[string]time.Time)
)

// logBlocked 限频输出告警日志。键空间有界（组件数 × 原因数）。
func logBlocked(component string, reason Reason, target string) {
	key := component + "|" + string(reason)
	now := time.Now()
	blockLogMu.Lock()
	if last, ok := blockLogAt[key]; ok && now.Sub(last) < blockLogInterval {
		blockLogMu.Unlock()
		return
	}
	blockLogAt[key] = now
	blockLogMu.Unlock()
	logger.Warnf("[outbound] %s 拦截 %s：%s（同类告警每分钟最多一条）", component, target, reason.message())
}
