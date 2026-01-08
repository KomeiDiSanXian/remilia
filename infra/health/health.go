package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthStatus represents overall health.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
)

// HealthChecker defines a single health check unit.
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) HealthCheckResult
}

// HealthCheckResult is a single checker result.
type HealthCheckResult struct {
	Status   HealthStatus   `json:"status"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Duration time.Duration  `json:"duration_ms"`
}

// HealthCheck manages multiple checkers and provides HTTP handlers.
type HealthCheck struct {
	checkers map[string]HealthChecker
	mu       sync.RWMutex
	// timeout is applied to each checker.
	timeout time.Duration
}

func NewHealthCheck() *HealthCheck {
	return &HealthCheck{
		checkers: make(map[string]HealthChecker),
		timeout:  5 * time.Second,
	}
}

func (h *HealthCheck) SetTimeout(timeout time.Duration) *HealthCheck {
	h.timeout = timeout
	return h
}

func (h *HealthCheck) AddChecker(checker HealthChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[checker.Name()] = checker
}

func (h *HealthCheck) RemoveChecker(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.checkers, name)
}

type HealthCheckResponse struct {
	Status HealthStatus                 `json:"status"`
	Checks map[string]HealthCheckResult `json:"checks"`
	Time   time.Time                    `json:"time"`
}

func (h *HealthCheck) Check(ctx context.Context) HealthCheckResponse {
	h.mu.RLock()
	checkers := make(map[string]HealthChecker, len(h.checkers))
	for name, checker := range h.checkers {
		checkers[name] = checker
	}
	h.mu.RUnlock()

	results := make(map[string]HealthCheckResult)
	overallStatus := HealthStatusHealthy

	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, checker := range checkers {
		wg.Add(1)
		go func(name string, checker HealthChecker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()

			start := time.Now()
			result := checker.Check(checkCtx)
			result.Duration = time.Since(start)

			mu.Lock()
			results[name] = result
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

func (h *HealthCheck) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	switch response.Status {
	case HealthStatusHealthy:
		w.WriteHeader(http.StatusOK)
	case HealthStatusDegraded:
		w.WriteHeader(http.StatusOK)
	case HealthStatusUnhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (h *HealthCheck) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	if response.Status == HealthStatusHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (h *HealthCheck) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	if response.Status == HealthStatusUnhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	_ = json.NewEncoder(w).Encode(response)
}
