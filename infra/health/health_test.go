package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockChecker 是一个用于测试的 mock checker
type mockChecker struct {
	name   string
	result CheckResult
	delay  time.Duration
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) CheckResult {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return CheckResult{
				Status: Unhealthy,
				Error:  "check cancelled",
			}
		}
	}
	return m.result
}

// TestNewCheck 测试创建健康检查
func TestNewCheck(t *testing.T) {
	check := NewCheck()
	require.NotNil(t, check)
	assert.Equal(t, 0, check.CheckerCount())
	assert.Equal(t, 5*time.Second, time.Duration(check.timeout.Load()))
}

// TestCheck_SetTimeout 测试设置超时
func TestCheck_SetTimeout(t *testing.T) {
	check := NewCheck()

	result := check.SetTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, time.Duration(check.timeout.Load()))
	assert.Equal(t, check, result) // 验证链式调用
}

// TestCheck_AddChecker 测试添加检查器
func TestCheck_AddChecker(t *testing.T) {
	check := NewCheck()

	checker1 := &mockChecker{name: "test1", result: CheckResult{Status: Healthy}}
	checker2 := &mockChecker{name: "test2", result: CheckResult{Status: Healthy}}

	check.AddChecker(checker1)
	assert.Equal(t, 1, check.CheckerCount())

	check.AddChecker(checker2)
	assert.Equal(t, 2, check.CheckerCount())

	assert.True(t, check.HasChecker("test1"))
	assert.True(t, check.HasChecker("test2"))
}

// TestCheck_RemoveChecker 测试移除检查器
func TestCheck_RemoveChecker(t *testing.T) {
	check := NewCheck()

	checker1 := &mockChecker{name: "test1", result: CheckResult{Status: Healthy}}
	checker2 := &mockChecker{name: "test2", result: CheckResult{Status: Healthy}}

	check.AddChecker(checker1)
	check.AddChecker(checker2)
	assert.Equal(t, 2, check.CheckerCount())

	check.RemoveChecker("test1")
	assert.Equal(t, 1, check.CheckerCount())
	assert.False(t, check.HasChecker("test1"))
	assert.True(t, check.HasChecker("test2"))

	// 移除不存在的检查器不应该出错
	check.RemoveChecker("nonexistent")
	assert.Equal(t, 1, check.CheckerCount())
}

// TestCheck_Check_AllHealthy 测试所有检查器都健康
func TestCheck_Check_AllHealthy(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name:   "checker1",
		result: CheckResult{Status: Healthy},
	})
	check.AddChecker(&mockChecker{
		name:   "checker2",
		result: CheckResult{Status: Healthy},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Equal(t, Healthy, response.Status)
	assert.Len(t, flattenGroups(response.Groups), 2)
	assert.Contains(t, flattenGroups(response.Groups), "checker1")
	assert.Contains(t, flattenGroups(response.Groups), "checker2")
	assert.Equal(t, Healthy, flattenGroups(response.Groups)["checker1"].Status)
	assert.Equal(t, Healthy, flattenGroups(response.Groups)["checker2"].Status)
	assert.NotZero(t, response.Time)
}

// TestCheck_Check_OneDegraded 测试一个检查器降级
func TestCheck_Check_OneDegraded(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name:   "healthy",
		result: CheckResult{Status: Healthy},
	})
	check.AddChecker(&mockChecker{
		name:   "degraded",
		result: CheckResult{Status: Degraded, Error: "slow response"},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Equal(t, Degraded, response.Status)
	assert.Len(t, flattenGroups(response.Groups), 2)
	assert.Equal(t, Healthy, flattenGroups(response.Groups)["healthy"].Status)
	assert.Equal(t, Degraded, flattenGroups(response.Groups)["degraded"].Status)
	assert.Equal(t, "slow response", flattenGroups(response.Groups)["degraded"].Error)
}

// TestCheck_Check_OneUnhealthy 测试一个检查器不健康
func TestCheck_Check_OneUnhealthy(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name:   "healthy",
		result: CheckResult{Status: Healthy},
	})
	check.AddChecker(&mockChecker{
		name:   "unhealthy",
		result: CheckResult{Status: Unhealthy, Error: "connection failed"},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Equal(t, Unhealthy, response.Status)
	assert.Len(t, flattenGroups(response.Groups), 2)
	assert.Equal(t, "connection failed", flattenGroups(response.Groups)["unhealthy"].Error)
}

// TestCheck_Check_UnhealthyOverridesDegraded 测试不健康覆盖降级状态
func TestCheck_Check_UnhealthyOverridesDegraded(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name:   "degraded",
		result: CheckResult{Status: Degraded},
	})
	check.AddChecker(&mockChecker{
		name:   "unhealthy",
		result: CheckResult{Status: Unhealthy},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	// Unhealthy 应该覆盖 Degraded
	assert.Equal(t, Unhealthy, response.Status)
}

// TestCheck_Check_WithMetadata 测试带元数据的检查
func TestCheck_Check_WithMetadata(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name: "with_metadata",
		result: CheckResult{
			Status: Healthy,
			Metadata: map[string]any{
				"version": "1.0.0",
				"uptime":  3600,
			},
		},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Equal(t, Healthy, response.Status)
	metadata := flattenGroups(response.Groups)["with_metadata"].Metadata
	assert.Equal(t, "1.0.0", metadata["version"])
	assert.Equal(t, 3600, metadata["uptime"])
}

// TestCheck_Check_WithTimeout 测试超时
func TestCheck_Check_WithTimeout(t *testing.T) {
	check := NewCheck().SetTimeout(100 * time.Millisecond)

	// 慢检查器（会超时）
	check.AddChecker(&mockChecker{
		name:   "slow",
		delay:  1 * time.Second,
		result: CheckResult{Status: Healthy},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Len(t, flattenGroups(response.Groups), 1)
	result := flattenGroups(response.Groups)["slow"]
	assert.Equal(t, Unhealthy, result.Status)
	assert.Contains(t, result.Error, "cancelled")
}

// TestCheck_Check_NoCheckers 测试没有检查器
func TestCheck_Check_NoCheckers(t *testing.T) {
	check := NewCheck()

	ctx := context.Background()
	response := check.Check(ctx)

	assert.Equal(t, Healthy, response.Status)
	assert.Equal(t, 0, len(flattenGroups(response.Groups)))
	assert.NotZero(t, response.Time)
}

// TestCheck_Check_DurationTracking 测试持续时间跟踪
func TestCheck_Check_DurationTracking(t *testing.T) {
	check := NewCheck()

	check.AddChecker(&mockChecker{
		name:   "with_delay",
		delay:  50 * time.Millisecond,
		result: CheckResult{Status: Healthy},
	})

	ctx := context.Background()
	response := check.Check(ctx)

	duration := flattenGroups(response.Groups)["with_delay"].Duration
	assert.GreaterOrEqual(t, duration, 50.0)
	assert.LessOrEqual(t, duration, 200.0)
}

// TestCheck_Check_ConcurrentExecution 测试并发执行
func TestCheck_Check_ConcurrentExecution(t *testing.T) {
	check := NewCheck()

	// 添加多个检查器，每个都有延迟
	for i := range 5 {
		check.AddChecker(&mockChecker{
			name:   "checker" + string(rune('0'+i)),
			delay:  50 * time.Millisecond,
			result: CheckResult{Status: Healthy},
		})
	}

	start := time.Now()
	ctx := context.Background()
	response := check.Check(ctx)
	elapsed := time.Since(start)

	// 并发执行，总时间应该远小于顺序执行
	assert.Len(t, flattenGroups(response.Groups), 5)
	assert.Less(t, elapsed, 150*time.Millisecond) // 应该在 100ms 左右，不是 250ms
}

// TestCheck_HTTPHandler 测试 HTTP 处理器
func TestCheck_HTTPHandler(t *testing.T) {
	tests := []struct {
		name           string
		checkers       []Checker
		expectedStatus int
		expectedHealth Status
	}{
		{
			name: "all healthy",
			checkers: []Checker{
				&mockChecker{name: "test1", result: CheckResult{Status: Healthy}},
				&mockChecker{name: "test2", result: CheckResult{Status: Healthy}},
			},
			expectedStatus: http.StatusOK,
			expectedHealth: Healthy,
		},
		{
			name: "one degraded",
			checkers: []Checker{
				&mockChecker{name: "test1", result: CheckResult{Status: Healthy}},
				&mockChecker{name: "test2", result: CheckResult{Status: Degraded}},
			},
			expectedStatus: http.StatusOK,
			expectedHealth: Degraded,
		},
		{
			name: "one unhealthy",
			checkers: []Checker{
				&mockChecker{name: "test1", result: CheckResult{Status: Healthy}},
				&mockChecker{name: "test2", result: CheckResult{Status: Unhealthy}},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: Unhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NewCheck()
			for _, c := range tt.checkers {
				check.AddChecker(c)
			}

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()

			check.HTTPHandler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var response CheckResponse
			err := json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedHealth, response.Status)
			assert.Len(t, flattenGroups(response.Groups), len(tt.checkers))
		})
	}
}

// TestCheck_ReadinessHandler 测试就绪性处理器
func TestCheck_ReadinessHandler(t *testing.T) {
	tests := []struct {
		name         string
		status       Status
		expectedCode int
	}{
		{
			name:         "healthy returns OK",
			status:       Healthy,
			expectedCode: http.StatusOK,
		},
		{
			name:         "degraded returns OK",
			status:       Degraded,
			expectedCode: http.StatusOK,
		},
		{
			name:         "unhealthy returns unavailable",
			status:       Unhealthy,
			expectedCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NewCheck()
			check.AddChecker(&mockChecker{
				name:   "test",
				result: CheckResult{Status: tt.status},
			})

			req := httptest.NewRequest(http.MethodGet, "/readiness", nil)
			w := httptest.NewRecorder()

			check.ReadinessHandler(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		})
	}
}

// TestCheck_LivenessHandler 测试存活性处理器
func TestCheck_LivenessHandler(t *testing.T) {
	tests := []struct {
		name         string
		status       Status
		expectedCode int
	}{
		{
			name:         "healthy returns OK",
			status:       Healthy,
			expectedCode: http.StatusOK,
		},
		{
			name:         "degraded returns OK",
			status:       Degraded,
			expectedCode: http.StatusOK,
		},
		{
			name:         "unhealthy returns unavailable",
			status:       Unhealthy,
			expectedCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NewCheck()
			check.AddChecker(&mockChecker{
				name:   "test",
				result: CheckResult{Status: tt.status},
			})

			req := httptest.NewRequest(http.MethodGet, "/liveness", nil)
			w := httptest.NewRecorder()

			check.LivenessHandler(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		})
	}
}

// TestCheck_HTTPHandler_ContextCancellation 测试 HTTP 处理器的上下文取消
func TestCheck_HTTPHandler_ContextCancellation(t *testing.T) {
	check := NewCheck()
	check.AddChecker(&mockChecker{
		name:   "slow",
		delay:  5 * time.Second,
		result: CheckResult{Status: Healthy},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	check.HTTPHandler(w, req)

	// 应该能够处理完成（尽管检查器可能超时）
	assert.NotZero(t, w.Code)
}

// TestCheck_ConcurrentAccess 测试并发访问
func TestCheck_ConcurrentAccess(t *testing.T) {
	check := NewCheck()

	// 并发添加检查器
	done := make(chan bool)
	for i := range 10 {
		go func(id int) {
			check.AddChecker(&mockChecker{
				name:   "checker" + string(rune('0'+id)),
				result: CheckResult{Status: Healthy},
			})
			done <- true
		}(i)
	}

	// 等待所有添加完成
	for range 10 {
		<-done
	}

	// 并发执行检查
	for range 5 {
		go func() {
			ctx := context.Background()
			response := check.Check(ctx)
			assert.NotNil(t, response)
			done <- true
		}()
	}

	// 等待所有检查完成
	for range 5 {
		<-done
	}

	// 并发移除检查器
	for i := range 5 {
		go func(id int) {
			check.RemoveChecker("checker" + string(rune('0'+id)))
			done <- true
		}(i)
	}

	// 等待所有移除完成
	for range 5 {
		<-done
	}

	// 验证最终状态
	assert.LessOrEqual(t, check.CheckerCount(), 10)
}

// BenchmarkCheck_Check 基准测试健康检查
func BenchmarkCheck_Check(b *testing.B) {
	check := NewCheck()
	check.AddChecker(&mockChecker{name: "test1", result: CheckResult{Status: Healthy}})
	check.AddChecker(&mockChecker{name: "test2", result: CheckResult{Status: Healthy}})
	check.AddChecker(&mockChecker{name: "test3", result: CheckResult{Status: Healthy}})

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = check.Check(ctx)
	}
}

// BenchmarkCheck_HTTPHandler 基准测试 HTTP 处理器
func BenchmarkCheck_HTTPHandler(b *testing.B) {
	check := NewCheck()
	check.AddChecker(&mockChecker{name: "test1", result: CheckResult{Status: Healthy}})
	check.AddChecker(&mockChecker{name: "test2", result: CheckResult{Status: Healthy}})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		check.HTTPHandler(w, req)
	}
}

// flattenGroups 将 groups 拍平为 name→CheckItem 的 map，简化测试断言。
func flattenGroups(groups []CheckGroup) map[string]CheckItem {
	m := make(map[string]CheckItem)
	for _, g := range groups {
		for _, item := range g.Checks {
			m[item.Name] = item
		}
	}
	return m
}
