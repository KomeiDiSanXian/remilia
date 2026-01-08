package remilia

import (
	"context"
	"fmt"
)

// --- built-in health checkers (stay in root package to avoid infra->remilia cycles) ---

// EngineHealthChecker checks Engine basic status.
type EngineHealthChecker struct {
	engine *Engine
}

func NewEngineHealthChecker(engine *Engine) *EngineHealthChecker {
	return &EngineHealthChecker{engine: engine}
}

func (c *EngineHealthChecker) Name() string { return "engine" }

func (c *EngineHealthChecker) Check(_ context.Context) HealthCheckResult {
	if c.engine == nil {
		return HealthCheckResult{Status: HealthStatusUnhealthy, Error: "engine is nil"}
	}

	metadata := map[string]any{"matcher_count": c.engine.GetMatcherCount()}
	return HealthCheckResult{Status: HealthStatusHealthy, Metadata: metadata}
}

// DeadLetterQueueHealthChecker checks DLQ backlog and drop rate.
type DeadLetterQueueHealthChecker struct {
	dlq            *DeadLetterQueue
	maxQueueSize   int
	maxDroppedRate float64
}

func NewDeadLetterQueueHealthChecker(dlq *DeadLetterQueue, maxQueueSize int, maxDroppedRate float64) *DeadLetterQueueHealthChecker {
	if maxQueueSize <= 0 {
		maxQueueSize = 1000
	}
	if maxDroppedRate <= 0 {
		maxDroppedRate = 0.1
	}
	return &DeadLetterQueueHealthChecker{dlq: dlq, maxQueueSize: maxQueueSize, maxDroppedRate: maxDroppedRate}
}

func (c *DeadLetterQueueHealthChecker) Name() string { return "dead_letter_queue" }

func (c *DeadLetterQueueHealthChecker) Check(_ context.Context) HealthCheckResult {
	if c.dlq == nil {
		return HealthCheckResult{Status: HealthStatusHealthy, Metadata: map[string]any{"enabled": false}}
	}

	stats := c.dlq.Stats()

	metadata := map[string]any{
		"queue_size": stats.QueueSize,
		"max_size":   stats.MaxSize,
		"processed":  stats.Processed,
		"dropped":    stats.Dropped,
		"workers":    stats.Workers,
	}

	totalItems := stats.Processed + stats.Dropped
	droppedRate := 0.0
	if totalItems > 0 {
		droppedRate = float64(stats.Dropped) / float64(totalItems)
	}
	metadata["dropped_rate"] = droppedRate

	if stats.QueueSize > c.maxQueueSize {
		return HealthCheckResult{
			Status:   HealthStatusDegraded,
			Error:    fmt.Sprintf("queue size %d exceeds threshold %d", stats.QueueSize, c.maxQueueSize),
			Metadata: metadata,
		}
	}

	if droppedRate > c.maxDroppedRate && totalItems > 100 {
		return HealthCheckResult{
			Status:   HealthStatusDegraded,
			Error:    fmt.Sprintf("dropped rate %.2f%% exceeds threshold %.2f%%", droppedRate*100, c.maxDroppedRate*100),
			Metadata: metadata,
		}
	}

	return HealthCheckResult{Status: HealthStatusHealthy, Metadata: metadata}
}
