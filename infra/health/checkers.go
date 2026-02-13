package health

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
)

// EngineHealthChecker checks engine basic status.
type EngineHealthChecker struct {
	engine *engine.Engine
}

// NewEngineHealthChecker creates a new EngineHealthChecker.
func NewEngineHealthChecker(engine *engine.Engine) *EngineHealthChecker {
	return &EngineHealthChecker{engine: engine}
}

// Name returns the checker name.
func (c *EngineHealthChecker) Name() string { return "engine" }

// Check performs the health check.
func (c *EngineHealthChecker) Check(_ context.Context) CheckResult {
	if c.engine == nil {
		return CheckResult{Status: Unhealthy, Error: "engine is nil"}
	}

	metadata := map[string]any{"matcher_count": c.engine.GetMatcherCount()}
	return CheckResult{Status: Healthy, Metadata: metadata}
}

// DeadLetterQueueHealthChecker checks DLQ backlog and drop rate.
type DeadLetterQueueHealthChecker struct {
	dlq            *dlq.DeadLetterQueue
	maxQueueSize   int
	maxDroppedRate float64
}

// NewDeadLetterQueueHealthChecker creates a new DeadLetterQueueHealthChecker.
//
// Parameters:
//   - dlq: the DeadLetterQueue to check
//   - maxQueueSize: maximum queue size threshold (default: 1000)
//   - maxDroppedRate: maximum dropped rate threshold (default: 0.1 = 10%)
func NewDeadLetterQueueHealthChecker(dlq *dlq.DeadLetterQueue, maxQueueSize int, maxDroppedRate float64) *DeadLetterQueueHealthChecker {
	if maxQueueSize <= 0 {
		maxQueueSize = 1000
	}
	if maxDroppedRate <= 0 {
		maxDroppedRate = 0.1
	}
	return &DeadLetterQueueHealthChecker{dlq: dlq, maxQueueSize: maxQueueSize, maxDroppedRate: maxDroppedRate}
}

// Name returns the checker name.
func (c *DeadLetterQueueHealthChecker) Name() string { return "dead_letter_queue" }

// Check performs the health check.
func (c *DeadLetterQueueHealthChecker) Check(_ context.Context) CheckResult {
	if c.dlq == nil {
		return CheckResult{Status: Healthy, Metadata: map[string]any{"enabled": false}}
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
		return CheckResult{
			Status:   Degraded,
			Error:    fmt.Sprintf("queue size %d exceeds threshold %d", stats.QueueSize, c.maxQueueSize),
			Metadata: metadata,
		}
	}

	if droppedRate > c.maxDroppedRate && totalItems > 100 {
		return CheckResult{
			Status:   Degraded,
			Error:    fmt.Sprintf("dropped rate %.2f%% exceeds threshold %.2f%%", droppedRate*100, c.maxDroppedRate*100),
			Metadata: metadata,
		}
	}

	return CheckResult{Status: Healthy, Metadata: metadata}
}
