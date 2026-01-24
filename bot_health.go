package remilia

import (
	"context"
	"time"

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
	adapter Adapter
}

// NewAdapterHealthChecker 创建 Adapter 健康检查器
func NewAdapterHealthChecker(adapter Adapter) *AdapterHealthChecker {
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

// TokenManagerHealthChecker 检查 Token Manager 状态
type TokenManagerHealthChecker struct {
	bot *Bot
}

// NewTokenManagerHealthChecker 创建 Token Manager 健康检查器
func NewTokenManagerHealthChecker(bot *Bot) *TokenManagerHealthChecker {
	return &TokenManagerHealthChecker{bot: bot}
}

// Name 返回检查器名称
func (c *TokenManagerHealthChecker) Name() string {
	return "token_manager"
}

// Check 执行健康检查
func (c *TokenManagerHealthChecker) Check(_ context.Context) health.CheckResult {
	if c.bot.tokenManager == nil {
		// Token Manager 是可选的
		return health.CheckResult{
			Status: health.Healthy,
			Metadata: map[string]any{
				"enabled": false,
			},
		}
	}

	// Token Manager 存在即认为健康
	return health.CheckResult{
		Status: health.Healthy,
		Metadata: map[string]any{
			"enabled": true,
		},
	}
}
