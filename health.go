package remilia

import (
	"time"
)

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string            `json:"status"`
	Uptime    time.Duration     `json:"uptime"`
	StartTime time.Time         `json:"start_time"`
	Checks    map[string]string `json:"checks"`
}

// HealthChecker 健康检查器
type HealthChecker struct {
	bot *Bot
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(bot *Bot) *HealthChecker {
	return &HealthChecker{
		bot: bot,
	}
}

// Check 执行健康检查
func (h *HealthChecker) Check() *HealthStatus {
	h.bot.mu.RLock()
	running := h.bot.running
	startTime := h.bot.startTime
	h.bot.mu.RUnlock()

	status := &HealthStatus{
		Status:    "healthy",
		StartTime: startTime,
		Checks:    make(map[string]string),
	}

	if !running {
		status.Status = "stopped"
		status.Checks["bot"] = "not running"
		return status
	}

	status.Uptime = h.bot.Uptime()
	status.Checks["bot"] = "running"

	// 检查 Engine 状态
	if h.bot.engine != nil {
		status.Checks["engine"] = "ready"
	} else {
		status.Status = "unhealthy"
		status.Checks["engine"] = "not initialized"
	}

	// 检查 Adapter 状态
	if h.bot.adapter != nil {
		status.Checks["adapter"] = "ready"
	} else {
		status.Status = "unhealthy"
		status.Checks["adapter"] = "not initialized"
	}

	// 检查生命周期管理器状态
	if h.bot.lifecycle != nil {
		lifecycleState := h.bot.lifecycle.State()
		status.Checks["lifecycle"] = lifecycleState.String()
	}

	return status
}
