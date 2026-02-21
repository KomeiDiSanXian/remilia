package health

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"time"
)

// Status represents overall health.
type Status string

const (
	// Healthy 完全健康 - 所有功能正常
	Healthy Status = "healthy"
	// Degraded 降级但可用 - 部分功能受影响，但核心功能正常
	Degraded Status = "degraded"
	// Unhealthy 不健康 - 核心功能受影响，但服务仍在运行
	Unhealthy Status = "unhealthy"
	// Critical 严重故障 - 服务即将停止或无法正常工作
	Critical Status = "critical"
)

// Level 健康级别（用于数值比较）
type Level int

const (
	HealthyLevel Level = iota
	DegradedLevel
	UnhealthyLevel
	CriticalLevel
)

// StatusToLevel 将状态转换为级别
func StatusToLevel(status Status) Level {
	switch status {
	case Healthy:
		return HealthyLevel
	case Degraded:
		return DegradedLevel
	case Unhealthy:
		return UnhealthyLevel
	case Critical:
		return CriticalLevel
	default:
		return UnhealthyLevel
	}
}

// LevelToStatus 将级别转换为状态
func LevelToStatus(level Level) Status {
	switch level {
	case HealthyLevel:
		return Healthy
	case DegradedLevel:
		return Degraded
	case UnhealthyLevel:
		return Unhealthy
	case CriticalLevel:
		return Critical
	default:
		return Unhealthy
	}
}

// Checker defines a single health check unit.
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// CheckResult is a single checker result.
type CheckResult struct {
	Status   Status         `json:"status"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Duration time.Duration  `json:"duration_ms"`
}

// Check manages multiple checkers and provides HTTP handlers.
type Check struct {
	checkers map[string]Checker
	mu       sync.RWMutex
	// timeout is applied to each checker.
	timeout time.Duration
}

func NewCheck() *Check {
	return &Check{
		checkers: make(map[string]Checker),
		timeout:  5 * time.Second,
	}
}

func (h *Check) SetTimeout(timeout time.Duration) *Check {
	h.timeout = timeout
	return h
}

func (h *Check) AddChecker(checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[checker.Name()] = checker
}

func (h *Check) RemoveChecker(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.checkers, name)
}

type CheckResponse struct {
	Status Status                 `json:"status"`
	Checks map[string]CheckResult `json:"checks"`
	Time   time.Time              `json:"time"`
}

func (h *Check) Check(ctx context.Context) CheckResponse {
	h.mu.RLock()
	checkers := make(map[string]Checker, len(h.checkers))
	maps.Copy(checkers, h.checkers)
	h.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallLevel := HealthyLevel // 使用级别而不是状态

	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, checker := range checkers {
		wg.Add(1)
		go func(name string, checker Checker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()

			start := time.Now()
			result := checker.Check(checkCtx)
			result.Duration = time.Since(start)

			mu.Lock()
			results[name] = result
			// 取最差的健康级别作为整体状态
			resultLevel := StatusToLevel(result.Status)
			if resultLevel > overallLevel {
				overallLevel = resultLevel
			}
			mu.Unlock()
		}(name, checker)
	}

	wg.Wait()

	return CheckResponse{
		Status: LevelToStatus(overallLevel),
		Checks: results,
		Time:   time.Now(),
	}
}

func (h *Check) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	switch response.Status {
	case Healthy:
		w.WriteHeader(http.StatusOK)
	case Degraded:
		w.WriteHeader(http.StatusOK)
	case Unhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (h *Check) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	// Degraded 表示"部分功能受影响但核心功能正常"，对 K8s 等编排系统仍应返回 200
	// 避免编排系统将仍可服务的实例错误地从流量中剔除
	switch response.Status {
	case Healthy, Degraded:
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (h *Check) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	if response.Status == Unhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	_ = json.NewEncoder(w).Encode(response)
}
