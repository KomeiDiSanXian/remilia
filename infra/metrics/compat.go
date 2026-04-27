package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// EventDroppedCounter exposes internal eventDropped metric for backward compatibility
// with older code that accessed the field directly.
//
// New code should prefer higher-level APIs (e.g. RecordEventDropped) instead of touching
// prometheus primitives directly.
func (mc *Collector) EventDroppedCounter() *prometheus.CounterVec {
	return mc.eventDropped
}

// MustRegisterOrGet 注册 Prometheus 收集器，若已存在则返回已注册的实例。
// 可用于在多个实例间安全共享同一指标定义，避免重复注册 panic。
// reg 为 nil 时使用 prometheus.DefaultRegisterer。
//
// 使用示例：
//
//	gauge := prometheus.NewGauge(...)
//	gauge = MustRegisterOrGet(reg, gauge).(prometheus.Gauge)
func MustRegisterOrGet(reg prometheus.Registerer, c prometheus.Collector) prometheus.Collector {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if err := reg.Register(c); err != nil {
		if are, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
			return are.ExistingCollector
		}
		panic(err)
	}
	return c
}
