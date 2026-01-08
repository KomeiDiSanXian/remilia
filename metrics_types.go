package remilia

import "github.com/KomeiDiSanXian/remilia/infra/metrics"

// Re-export metrics types for backward compatibility.

type MetricsCollector = metrics.MetricsCollector

type PoolMetricsSnapshot = metrics.PoolMetricsSnapshot
