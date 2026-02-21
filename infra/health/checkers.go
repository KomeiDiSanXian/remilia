package health

import (
	"context"
	"fmt"
)

// EngineStats 是 health 包对 Engine 状态的最小视图。
// 上层（如 bot_health.go）在注册 checker 时传入实现此接口的对象。
// 这样 infra/health 就不再依赖上层的 core/engine 包，遵循依赖倒置原则。
type EngineStats interface {
	// GetMatcherCount 返回当前活跃的 Matcher 数量
	GetMatcherCount() int
}

// EngineHealthChecker checks engine basic status.
//
// 使用示例（在 bot 层注册）：
//
//	check.AddChecker(health.NewEngineHealthChecker(engine))
type EngineHealthChecker struct {
	engine EngineStats
}

// NewEngineHealthChecker creates a new EngineHealthChecker.
// engine 参数接受任何实现了 EngineStats 接口的对象（如 *engine.Engine）。
func NewEngineHealthChecker(engine EngineStats) *EngineHealthChecker {
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

// DLQStats 是 health 包对死信队列统计的最小视图。
// infra/health 不再直接依赖 infra/dlq 具体类型。
type DLQStats interface {
	// Stats 返回队列的统计快照
	Stats() DLQStatsSnapshot
}

// DLQStatsSnapshot 队列统计快照，由调用方通过适配器填充。
// 定义在 health 包中，避免引入 infra/dlq 的具体类型。
type DLQStatsSnapshot struct {
	QueueSize int
	MaxSize   int
	Processed uint64
	Dropped   uint64
	Workers   int
}

// DeadLetterQueueHealthChecker checks DLQ backlog and drop rate.
//
// 使用示例：
//
//	check.AddChecker(health.NewDeadLetterQueueHealthChecker(dlqAdapter, 1000, 0.1))
type DeadLetterQueueHealthChecker struct {
	dlq            DLQStats
	maxQueueSize   int
	maxDroppedRate float64
}

// NewDeadLetterQueueHealthChecker creates a new DeadLetterQueueHealthChecker.
//
// Parameters:
//   - dlq: 实现了 DLQStats 接口的对象（如 *dlq.DeadLetterQueue 通过适配器包装）
//   - maxQueueSize: maximum queue size threshold (default: 1000)
//   - maxDroppedRate: maximum dropped rate threshold (default: 0.1 = 10%)
func NewDeadLetterQueueHealthChecker(dlq DLQStats, maxQueueSize int, maxDroppedRate float64) *DeadLetterQueueHealthChecker {
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
