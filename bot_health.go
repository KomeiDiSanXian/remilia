package remilia

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/infra/health"
)

// BotStatusChecker 检查 Bot 的基本状态
type BotStatusChecker struct {
	bot *Bot
}

// NewBotStatusChecker 创建 Bot 状态检查器
func NewBotStatusChecker(bot *Bot) *BotStatusChecker {
	return &BotStatusChecker{bot: bot}
}

// Name 返回检查器名称
func (c *BotStatusChecker) Name() string {
	return "bot"
}

// Check 执行健康检查
func (c *BotStatusChecker) Check(_ context.Context) health.CheckResult {
	c.bot.mu.RLock()
	running := c.bot.running
	startTime := c.bot.startTime
	stopTime := c.bot.stopTime
	c.bot.mu.RUnlock()

	metadata := map[string]any{
		"name":    c.bot.config.Name,
		"version": c.bot.config.Version,
	}

	// Bot 未运行
	if !running {
		if !stopTime.IsZero() {
			metadata["stop_time"] = stopTime
			metadata["last_uptime"] = stopTime.Sub(startTime).String()
		}
		return health.CheckResult{
			Status:   health.Unhealthy,
			Error:    "bot not running",
			Metadata: metadata,
		}
	}

	// Bot 正在运行
	uptime := time.Since(startTime)
	metadata["start_time"] = startTime
	metadata["uptime"] = uptime.String()
	metadata["uptime_seconds"] = uptime.Seconds()

	// 检查生命周期状态
	if c.bot.lifecycle != nil {
		lifecycleState := c.bot.lifecycle.State()
		metadata["lifecycle_state"] = lifecycleState.String()

		// 如果生命周期不是 Running 状态，标记为 Degraded
		if lifecycleState.String() != "running" {
			return health.CheckResult{
				Status:   health.Degraded,
				Error:    "lifecycle not in running state",
				Metadata: metadata,
			}
		}
	}

	return health.CheckResult{
		Status:   health.Healthy,
		Metadata: metadata,
	}
}

// AdapterHealthChecker 检查 Adapter 状态
type AdapterHealthChecker struct {
	adapter engine.PlatformAdapter
}

// NewAdapterHealthChecker 创建 Adapter 健康检查器
func NewAdapterHealthChecker(adapter engine.PlatformAdapter) *AdapterHealthChecker {
	return &AdapterHealthChecker{adapter: adapter}
}

// Name 返回检查器名称
func (c *AdapterHealthChecker) Name() string {
	return "adapter"
}

// Check 执行健康检查
func (c *AdapterHealthChecker) Check(_ context.Context) health.CheckResult {
	if c.adapter == nil {
		return health.CheckResult{
			Status: health.Unhealthy,
			Error:  "adapter is nil",
		}
	}

	// Adapter 存在即认为健康
	// 如果需要更详细的检查，可以让 Adapter 接口实现 Healthchecker
	return health.CheckResult{
		Status: health.Healthy,
		Metadata: map[string]any{
			"type": "adapter",
		},
	}
}

// dlqStater is satisfied by any dlq.Queue[T].
type dlqStater interface {
	Stats() dlq.Stats
}

// DLQHealthAdapter 将任意 dlq.Queue[T] 适配为 health.DLQStats 接口。
//
// 这是解决 infra/health 不应直接依赖 infra/dlq 的适配器（Adapter Pattern）。
// infra/health 只知道 health.DLQStats 接口，实际的 DLQ 类型在上层（bot 层）注入。
//
// 使用示例：
//
//	q := dlq.NewPayloadQueue(dlq.PayloadConfig{...})
//	adapter := remilia.NewDLQHealthAdapter(q)
//	botHealth.AddChecker(health.NewDeadLetterQueueHealthChecker(adapter, 1000, 0.1))
type DLQHealthAdapter struct {
	q dlqStater
}

// NewDLQHealthAdapter 创建 DLQ 健康检查适配器。
// 接受任意实现了 Stats() dlq.Stats 的队列，例如 *dlq.PayloadQueue 或 *dlq.PlatformEventQueue。
func NewDLQHealthAdapter(q dlqStater) *DLQHealthAdapter {
	return &DLQHealthAdapter{q: q}
}

// Stats 实现 health.DLQStats 接口，将 dlq.Stats 转换为 health.DLQStatsSnapshot
func (a *DLQHealthAdapter) Stats() health.DLQStatsSnapshot {
	s := a.q.Stats()
	// dlq.Stats.Processed/Dropped 是 int64，转换为 uint64（值域安全：计数不会为负）
	processed := uint64(0)
	if s.Processed > 0 {
		processed = uint64(s.Processed)
	}
	dropped := uint64(0)
	if s.Dropped > 0 {
		dropped = uint64(s.Dropped)
	}
	return health.DLQStatsSnapshot{
		QueueSize: s.QueueSize,
		MaxSize:   s.MaxSize,
		Processed: processed,
		Dropped:   dropped,
		Workers:   s.Workers,
	}
}
