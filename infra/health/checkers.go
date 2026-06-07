package health

import (
	"context"
	"fmt"
	"runtime"
)

// EngineStats 是 health 包对 Engine 状态的最小视图。
// 上层（如 bot_health.go）在注册 checker 时传入实现此接口的对象。
// 这样 infra/health 就不再依赖上层的 core/engine 包，遵循依赖倒置原则。
type EngineStats interface {
	// GetMatcherCount 返回当前活跃的 Matcher 数量
	GetMatcherCount() int
	// GetTempMatcherCount 返回当前临时 Matcher 数量
	GetTempMatcherCount() int
	// GetMaxMatchers 返回 Matcher 数量上限
	GetMaxMatchers() int
}

// EngineHealthChecker 检查引擎的基本状态。
//
// 使用示例（在 bot 层注册）：
//
//	check.AddChecker(health.NewEngineHealthChecker(engine))
type EngineHealthChecker struct {
	engine EngineStats
}

// NewEngineHealthChecker 创建新的 EngineHealthChecker。
// engine 参数接受任何实现了 EngineStats 接口的对象（如 *engine.Engine）。
func NewEngineHealthChecker(engine EngineStats) *EngineHealthChecker {
	return &EngineHealthChecker{engine: engine}
}

// Name 返回检查器名称。
func (c *EngineHealthChecker) Name() string { return "engine" }

// Check 执行健康检查。
func (c *EngineHealthChecker) Check(_ context.Context) CheckResult {
	if c.engine == nil {
		return CheckResult{Status: Unhealthy, Error: "engine is nil"}
	}

	metadata := map[string]any{
		"matcher_count":      c.engine.GetMatcherCount(),
		"temp_matcher_count": c.engine.GetTempMatcherCount(),
		"max_matchers":       c.engine.GetMaxMatchers(),
	}
	return CheckResult{Status: Healthy, Metadata: metadata}
}

// RuntimeChecker 报告 Go 运行时状态（goroutines、内存）。
//
// goroutineThreshold: 超过此值标记为 Degraded，<=0 不限制。
type RuntimeChecker struct {
	goroutineThreshold int
}

// NewRuntimeChecker 创建运行时状态检查器。
// goroutineThreshold 为 goroutine 数量阈值，超过时标记 Degraded，<=0 不限制。
func NewRuntimeChecker(goroutineThreshold int) *RuntimeChecker {
	return &RuntimeChecker{goroutineThreshold: goroutineThreshold}
}

// Name 返回检查器名称。
func (c *RuntimeChecker) Name() string { return "runtime" }

// Check 执行健康检查。
func (c *RuntimeChecker) Check(_ context.Context) CheckResult {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	goroutines := runtime.NumGoroutine()
	metadata := map[string]any{
		"goroutines":        goroutines,
		"alloc_mb":          bToMb(m.Alloc),
		"total_alloc_mb":    bToMb(m.TotalAlloc),
		"sys_mb":            bToMb(m.Sys),
		"heap_inuse_mb":     bToMb(m.HeapInuse),
		"gc_cycles":         m.NumGC,
		"gc_pause_total_ms": m.PauseTotalNs / 1e6,
	}

	if c.goroutineThreshold > 0 && goroutines > c.goroutineThreshold {
		return CheckResult{
			Status:   Degraded,
			Error:    fmt.Sprintf("goroutine count %d exceeds threshold %d", goroutines, c.goroutineThreshold),
			Metadata: metadata,
		}
	}

	return CheckResult{Status: Healthy, Metadata: metadata}
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
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

// CheckProvider 插件可实现此接口让 bot 自动注册健康检查器。
//
// discoverAll 在 FreezeContainer 后遍历所有插件容器，
// 检查每个插件的导出 API 是否实现了此接口，
// 若是则将其所有 Checker 注册到 bot.HealthCheck()。
type CheckProvider interface {
	// HealthCheckers 返回该插件管理的所有健康检查器。
	// 通常在 Setup 阶段创建的 APIProbe 实例。
	HealthCheckers() []Checker
}

// DeadLetterQueueHealthChecker 检查死信队列积压情况和丢弃率。
//
// 使用示例：
//
//	check.AddChecker(health.NewDeadLetterQueueHealthChecker(dlqAdapter, 1000, 0.1))
type DeadLetterQueueHealthChecker struct {
	dlq            DLQStats
	maxQueueSize   int
	maxDroppedRate float64
}

// NewDeadLetterQueueHealthChecker 创建新的 DeadLetterQueueHealthChecker。
//
// 参数：
//   - dlq: 实现了 DLQStats 接口的对象（如 *dlq.DeadLetterQueue 通过适配器包装）
//   - maxQueueSize: 队列大小阈值（默认：1000）
//   - maxDroppedRate: 最大丢弃率阈值（默认：0.1 = 10%）
func NewDeadLetterQueueHealthChecker(dlq DLQStats, maxQueueSize int, maxDroppedRate float64) *DeadLetterQueueHealthChecker {
	if maxQueueSize <= 0 {
		maxQueueSize = 1000
	}
	if maxDroppedRate <= 0 {
		maxDroppedRate = 0.1
	}
	return &DeadLetterQueueHealthChecker{dlq: dlq, maxQueueSize: maxQueueSize, maxDroppedRate: maxDroppedRate}
}

// Name 返回检查器名称。
func (c *DeadLetterQueueHealthChecker) Name() string { return "dead_letter_queue" }

// Check 执行健康检查。
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
