package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status represents overall health.
type Status string

const (
	Healthy   Status = "healthy"
	Unhealthy Status = "unhealthy"
	Degraded  Status = "degraded"
)

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
	for name, checker := range h.checkers {
		checkers[name] = checker
	}
	h.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallStatus := Healthy

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
			if result.Status == Unhealthy {
				overallStatus = Unhealthy
			} else if result.Status == Degraded && overallStatus != Unhealthy {
				overallStatus = Degraded
			}
			mu.Unlock()
		}(name, checker)
	}

	wg.Wait()

	return CheckResponse{
		Status: overallStatus,
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

	if response.Status == Healthy {
		w.WriteHeader(http.StatusOK)
	} else {
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
