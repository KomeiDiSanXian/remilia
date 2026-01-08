package remilia

// Backward-compatible infra facades.

import (
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/infra/pool"
)

// NOTE: For historical reasons, the public symbols (HealthCheck/MetricsCollector/etc.)
// are still provided by package remilia. We keep the concrete implementations in
// infra/* packages and only forward constructors here.

func NewHealthCheck() *health.HealthCheck { return health.NewHealthCheck() }

func NewMetricsCollector(namespace string) *metrics.MetricsCollector {
	return metrics.NewMetricsCollector(namespace)
}

func NewInstrumentedPool(newFunc func() any) *pool.InstrumentedPool {
	return pool.NewInstrumentedPool(newFunc)
}
