package metrics

import "github.com/prometheus/client_golang/prometheus"

// EventDroppedCounter exposes internal eventDropped metric for backward compatibility
// with older code that accessed the field directly.
//
// New code should prefer higher-level APIs (e.g. RecordEventDropped) instead of touching
// prometheus primitives directly.
func (mc *MetricsCollector) EventDroppedCounter() *prometheus.CounterVec {
	return mc.eventDropped
}
