package remilia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// HealthStatus 健康状态
type HealthStatus string

const (
	// HealthStatusHealthy 健康状态
	HealthStatusHealthy HealthStatus = "healthy"
	// HealthStatusUnhealthy 不健康状态
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	// HealthStatusDegraded 降级状态（部分功能不可用）
	HealthStatusDegraded HealthStatus = "degraded"
)

// HealthChecker 健康检查器接口
//
// 实现此接口以提供自定义健康检查逻辑。
//
// 使用示例：
//
//	type DBHealthChecker struct {
//	    db *sql.DB
//	}
//
//	func (c *DBHealthChecker) Name() string {
//	    return "database"
//	}
//
//	func (c *DBHealthChecker) Check(ctx context.Context) HealthCheckResult {
//	    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
//	    defer cancel()
//
//	    if err := c.db.PingContext(ctx); err != nil {
//	        return HealthCheckResult{
//	            Status: HealthStatusUnhealthy,
//	            Error:  err.Error(),
//	        }
//	    }
//
//	    return HealthCheckResult{
//	        Status: HealthStatusHealthy,
//	    }
//	}
type HealthChecker interface {
	// Name 返回检查器的名称
	Name() string

	// Check 执行健康检查
	Check(ctx context.Context) HealthCheckResult
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	Status   HealthStatus   `json:"status"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Duration time.Duration  `json:"duration_ms"`
}

// HealthCheck 健康检查管理器
//
// 管理多个健康检查器，并提供 HTTP 端点。
//
// 使用示例：
//
//	hc := remilia.NewHealthCheck()
//	hc.AddChecker(&DBHealthChecker{db: db})
//	hc.AddChecker(&RedisHealthChecker{client: redis})
//
//	// 注册 HTTP 端点
//	http.HandleFunc("/health", hc.HTTPHandler)
//	http.HandleFunc("/health/ready", hc.ReadinessHandler)
//	http.HandleFunc("/health/live", hc.LivenessHandler)
type HealthCheck struct {
	checkers map[string]HealthChecker
	mu       sync.RWMutex

	// 配置
	timeout time.Duration // 单个检查器的超时时间
}

// NewHealthCheck 创建一个新的健康检查管理器
func NewHealthCheck() *HealthCheck {
	return &HealthCheck{
		checkers: make(map[string]HealthChecker),
		timeout:  5 * time.Second, // 默认 5 秒超时
	}
}

// SetTimeout 设置单个检查器的超时时间
func (h *HealthCheck) SetTimeout(timeout time.Duration) *HealthCheck {
	h.timeout = timeout
	return h
}

// AddChecker 添加健康检查器
func (h *HealthCheck) AddChecker(checker HealthChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[checker.Name()] = checker
	logrus.WithField("checker", checker.Name()).Debug("[HealthCheck] Checker added")
}

// RemoveChecker 移除健康检查器
func (h *HealthCheck) RemoveChecker(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.checkers, name)
	logrus.WithField("checker", name).Debug("[HealthCheck] Checker removed")
}

// Check 执行所有健康检查
//
// 返回整体健康状态和各检查器的详细结果。
// 如果任何检查器返回 Unhealthy，整体状态为 Unhealthy。
// 如果有检查器返回 Degraded，整体状态为 Degraded（除非有 Unhealthy）。
func (h *HealthCheck) Check(ctx context.Context) HealthCheckResponse {
	h.mu.RLock()
	checkers := make(map[string]HealthChecker, len(h.checkers))
	for name, checker := range h.checkers {
		checkers[name] = checker
	}
	h.mu.RUnlock()

	results := make(map[string]HealthCheckResult)
	overallStatus := HealthStatusHealthy

	// 并发执行所有检查
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, checker := range checkers {
		wg.Add(1)
		go func(name string, checker HealthChecker) {
			defer wg.Done()

			// 为每个检查器创建带超时的 context
			checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()

			start := time.Now()
			result := checker.Check(checkCtx)
			result.Duration = time.Since(start)

			mu.Lock()
			results[name] = result

			// 更新整体状态
			if result.Status == HealthStatusUnhealthy {
				overallStatus = HealthStatusUnhealthy
			} else if result.Status == HealthStatusDegraded && overallStatus != HealthStatusUnhealthy {
				overallStatus = HealthStatusDegraded
			}
			mu.Unlock()

		}(name, checker)
	}

	wg.Wait()

	return HealthCheckResponse{
		Status: overallStatus,
		Checks: results,
		Time:   time.Now(),
	}
}

// HealthCheckResponse 健康检查响应
type HealthCheckResponse struct {
	Status HealthStatus                 `json:"status"`
	Checks map[string]HealthCheckResult `json:"checks"`
	Time   time.Time                    `json:"time"`
}

// HTTPHandler 返回健康检查的 HTTP handler
//
// 响应格式：
//   - 200: 健康
//   - 503: 不健康
//   - 200: 降级（可选，根据需求决定返回码）
//
// 使用示例：
//
//	http.HandleFunc("/health", hc.HTTPHandler)
func (h *HealthCheck) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	// 根据状态设置 HTTP 状态码
	switch response.Status {
	case HealthStatusHealthy:
		w.WriteHeader(http.StatusOK)
	case HealthStatusDegraded:
		// 降级状态仍然返回 200，但在响应中标记
		w.WriteHeader(http.StatusOK)
	case HealthStatusUnhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("[HealthCheck] Failed to encode response")
	}
}

// ReadinessHandler 就绪检查 handler（用于 Kubernetes readiness probe）
//
// 检查服务是否准备好接收流量。
// 如果任何检查器返回 Unhealthy，返回 503。
//
// 使用示例：
//
//	http.HandleFunc("/health/ready", hc.ReadinessHandler)
func (h *HealthCheck) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	// Readiness: Degraded 也视为不就绪
	if response.Status == HealthStatusHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("[HealthCheck] Failed to encode response")
	}
}

// LivenessHandler 存活检查 handler（用于 Kubernetes liveness probe）
//
// 检查服务是否存活。
// 通常比 Readiness 更宽松，只检查关键组件。
//
// 使用示例：
//
//	http.HandleFunc("/health/live", hc.LivenessHandler)
func (h *HealthCheck) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	// Liveness: 只有 Unhealthy 才视为不存活
	if response.Status == HealthStatusUnhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("[HealthCheck] Failed to encode response")
	}
}

// --- 内置健康检查器 ---

// EngineHealthChecker Engine 健康检查器
//
// 检查 Engine 是否正常工作。
type EngineHealthChecker struct {
	engine *Engine
}

// NewEngineHealthChecker 创建 Engine 健康检查器
func NewEngineHealthChecker(engine *Engine) *EngineHealthChecker {
	return &EngineHealthChecker{engine: engine}
}

// Name 返回检查器名称
func (c *EngineHealthChecker) Name() string {
	return "engine"
}

// Check 执行检查
func (c *EngineHealthChecker) Check(_ context.Context) HealthCheckResult {
	if c.engine == nil {
		return HealthCheckResult{
			Status: HealthStatusUnhealthy,
			Error:  "engine is nil",
		}
	}

	// 检查 Engine 的基本状态
	matcherCount := c.engine.GetMatcherCount()

	metadata := map[string]any{
		"matcher_count": matcherCount,
	}

	// 可以添加更多检查逻辑
	// 例如：检查是否有临时 matcher 堆积过多

	return HealthCheckResult{
		Status:   HealthStatusHealthy,
		Metadata: metadata,
	}
}

// DeadLetterQueueHealthChecker 死信队列健康检查器
//
// 检查死信队列是否健康，如果队列堆积过多或丢弃率过高，返回降级状态。
type DeadLetterQueueHealthChecker struct {
	dlq            *DeadLetterQueue
	maxQueueSize   int     // 队列大小阈值（超过则降级）
	maxDroppedRate float64 // 最大丢弃率（超过则降级）
}

// NewDeadLetterQueueHealthChecker 创建死信队列健康检查器
func NewDeadLetterQueueHealthChecker(dlq *DeadLetterQueue, maxQueueSize int, maxDroppedRate float64) *DeadLetterQueueHealthChecker {
	if maxQueueSize <= 0 {
		maxQueueSize = 1000
	}
	if maxDroppedRate <= 0 {
		maxDroppedRate = 0.1 // 默认 10%
	}
	return &DeadLetterQueueHealthChecker{
		dlq:            dlq,
		maxQueueSize:   maxQueueSize,
		maxDroppedRate: maxDroppedRate,
	}
}

// Name 返回检查器名称
func (c *DeadLetterQueueHealthChecker) Name() string {
	return "dead_letter_queue"
}

// Check 执行检查
func (c *DeadLetterQueueHealthChecker) Check(_ context.Context) HealthCheckResult {
	if c.dlq == nil {
		return HealthCheckResult{
			Status: HealthStatusHealthy,
			Metadata: map[string]any{
				"enabled": false,
			},
		}
	}

	stats := c.dlq.Stats()

	metadata := map[string]any{
		"queue_size": stats.QueueSize,
		"max_size":   stats.MaxSize,
		"processed":  stats.Processed,
		"dropped":    stats.Dropped,
		"workers":    stats.Workers,
	}

	// 计算丢弃率
	totalItems := stats.Processed + stats.Dropped
	droppedRate := 0.0
	if totalItems > 0 {
		droppedRate = float64(stats.Dropped) / float64(totalItems)
	}
	metadata["dropped_rate"] = droppedRate

	// 检查队列大小
	if stats.QueueSize > c.maxQueueSize {
		return HealthCheckResult{
			Status:   HealthStatusDegraded,
			Error:    fmt.Sprintf("queue size %d exceeds threshold %d", stats.QueueSize, c.maxQueueSize),
			Metadata: metadata,
		}
	}

	// 检查丢弃率
	if droppedRate > c.maxDroppedRate && totalItems > 100 { // 至少有 100 个样本
		return HealthCheckResult{
			Status:   HealthStatusDegraded,
			Error:    fmt.Sprintf("dropped rate %.2f%% exceeds threshold %.2f%%", droppedRate*100, c.maxDroppedRate*100),
			Metadata: metadata,
		}
	}

	return HealthCheckResult{
		Status:   HealthStatusHealthy,
		Metadata: metadata,
	}
}
