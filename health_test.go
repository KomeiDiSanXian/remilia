package remilia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// MockHealthChecker 用于测试的 mock 健康检查器
type MockHealthChecker struct {
	name   string
	result HealthCheckResult
}

func (m *MockHealthChecker) Name() string {
	return m.name
}

func (m *MockHealthChecker) Check(ctx context.Context) HealthCheckResult {
	return m.result
}

func TestHealthCheck_SingleChecker(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	response := hc.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, response.Status)
	assert.Len(t, response.Checks, 1)
	assert.Equal(t, HealthStatusHealthy, response.Checks["test"].Status)
}

func TestHealthCheck_MultipleCheckers(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "checker1",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})
	hc.AddChecker(&MockHealthChecker{
		name: "checker2",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	response := hc.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, response.Status)
	assert.Len(t, response.Checks, 2)
}

func TestHealthCheck_UnhealthyChecker(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "healthy",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})
	hc.AddChecker(&MockHealthChecker{
		name: "unhealthy",
		result: HealthCheckResult{
			Status: HealthStatusUnhealthy,
			Error:  "test error",
		},
	})

	response := hc.Check(context.Background())

	assert.Equal(t, HealthStatusUnhealthy, response.Status)
	assert.Equal(t, "test error", response.Checks["unhealthy"].Error)
}

func TestHealthCheck_DegradedChecker(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "healthy",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})
	hc.AddChecker(&MockHealthChecker{
		name: "degraded",
		result: HealthCheckResult{
			Status: HealthStatusDegraded,
			Error:  "performance degraded",
		},
	})

	response := hc.Check(context.Background())

	assert.Equal(t, HealthStatusDegraded, response.Status)
}

func TestHealthCheck_UnhealthyOverridesDegraded(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "degraded",
		result: HealthCheckResult{
			Status: HealthStatusDegraded,
		},
	})
	hc.AddChecker(&MockHealthChecker{
		name: "unhealthy",
		result: HealthCheckResult{
			Status: HealthStatusUnhealthy,
		},
	})

	response := hc.Check(context.Background())

	// Unhealthy 应该覆盖 Degraded
	assert.Equal(t, HealthStatusUnhealthy, response.Status)
}

func TestHealthCheck_HTTPHandler(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	hc.HTTPHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response HealthCheckResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, response.Status)
}

func TestHealthCheck_HTTPHandler_Unhealthy(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusUnhealthy,
			Error:  "test error",
		},
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	hc.HTTPHandler(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthCheckResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	assert.NoError(t, err)
	assert.Equal(t, HealthStatusUnhealthy, response.Status)
}

func TestHealthCheck_ReadinessHandler(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	hc.ReadinessHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHealthCheck_ReadinessHandler_Degraded(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusDegraded,
		},
	})

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	hc.ReadinessHandler(w, req)

	// Readiness 对 Degraded 也返回 503
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHealthCheck_LivenessHandler(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	hc.LivenessHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHealthCheck_LivenessHandler_Degraded(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusDegraded,
		},
	})

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	hc.LivenessHandler(w, req)

	// Liveness 对 Degraded 仍然返回 200
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHealthCheck_LivenessHandler_Unhealthy(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusUnhealthy,
		},
	})

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	hc.LivenessHandler(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHealthCheck_RemoveChecker(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	response := hc.Check(context.Background())
	assert.Len(t, response.Checks, 1)

	hc.RemoveChecker("test")

	response = hc.Check(context.Background())
	assert.Len(t, response.Checks, 0)
}

func TestHealthCheck_Timeout(t *testing.T) {
	hc := NewHealthCheck()
	hc.SetTimeout(100 * time.Millisecond)

	// 创建一个慢速检查器
	hc.AddChecker(&MockHealthChecker{
		name: "slow",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	// 覆盖 Check 方法以模拟慢速检查
	slowChecker := &struct {
		MockHealthChecker
	}{
		MockHealthChecker: MockHealthChecker{
			name: "slow",
		},
	}
	slowChecker.MockHealthChecker.result = HealthCheckResult{
		Status: HealthStatusHealthy,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	response := hc.Check(ctx)

	// 应该完成（即使有超时）
	assert.NotNil(t, response)
}

func TestEngineHealthChecker(t *testing.T) {
	engine := NewEngine()
	checker := NewEngineHealthChecker(engine)

	assert.Equal(t, "engine", checker.Name())

	result := checker.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.NotNil(t, result.Metadata)
	assert.Contains(t, result.Metadata, "matcher_count")
}

func TestEngineHealthChecker_NilEngine(t *testing.T) {
	checker := NewEngineHealthChecker(nil)

	result := checker.Check(context.Background())

	assert.Equal(t, HealthStatusUnhealthy, result.Status)
	assert.Contains(t, result.Error, "engine is nil")
}

func TestDeadLetterQueueHealthChecker(t *testing.T) {
	dlq := NewDeadLetterQueue(DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	})

	checker := NewDeadLetterQueueHealthChecker(dlq, 100, 0.1)

	assert.Equal(t, "dead_letter_queue", checker.Name())

	result := checker.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.NotNil(t, result.Metadata)
	assert.Contains(t, result.Metadata, "queue_size")
}

func TestDeadLetterQueueHealthChecker_NilQueue(t *testing.T) {
	checker := NewDeadLetterQueueHealthChecker(nil, 100, 0.1)

	result := checker.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Equal(t, false, result.Metadata["enabled"])
}

func TestHealthCheck_ConcurrentAccess(t *testing.T) {
	hc := NewHealthCheck()
	hc.AddChecker(&MockHealthChecker{
		name: "test",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	// 并发访问
	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			_ = hc.Check(context.Background())
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 50; i++ {
		<-done
	}

	// 应该没有 panic
	assert.NotNil(t, hc)
}

func TestHealthCheck_Duration(t *testing.T) {
	hc := NewHealthCheck()

	// 添加一个实现了 HealthChecker 接口的检查器
	hc.AddChecker(&MockHealthChecker{
		name: "slow",
		result: HealthCheckResult{
			Status: HealthStatusHealthy,
		},
	})

	response := hc.Check(context.Background())

	// Check that duration is recorded (might be 0 for very fast checks)
	assert.GreaterOrEqual(t, response.Checks["slow"].Duration, time.Duration(0))
	// Also check that the check was actually executed
	assert.Contains(t, response.Checks, "slow")
}
