package health_test

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/stretchr/testify/assert"
)

// TestHealthLevels 测试健康级别
func TestHealthLevels(t *testing.T) {
	tests := []struct {
		status        health.Status
		expectedLevel health.Level
	}{
		{health.Healthy, health.HealthyLevel},
		{health.Degraded, health.DegradedLevel},
		{health.Unhealthy, health.UnhealthyLevel},
		{health.Critical, health.CriticalLevel},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			level := health.StatusToLevel(tt.status)
			assert.Equal(t, tt.expectedLevel, level)

			// 反向转换
			status := health.LevelToStatus(level)
			assert.Equal(t, tt.status, status)
		})
	}
}

// TestLevelComparison 测试级别比较
func TestLevelComparison(t *testing.T) {
	assert.True(t, health.HealthyLevel < health.DegradedLevel)
	assert.True(t, health.DegradedLevel < health.UnhealthyLevel)
	assert.True(t, health.UnhealthyLevel < health.CriticalLevel)
}

// MockChecker 模拟检查器
type MockChecker struct {
	name   string
	status health.Status
}

func (m *MockChecker) Name() string {
	return m.name
}

func (m *MockChecker) Check(ctx context.Context) health.CheckResult {
	return health.CheckResult{
		Status: m.status,
	}
}

// TestCheckAggregation 测试状态聚合
func TestCheckAggregation(t *testing.T) {
	tests := []struct {
		name           string
		checkers       []health.Checker
		expectedStatus health.Status
	}{
		{
			name: "all healthy",
			checkers: []health.Checker{
				&MockChecker{"checker1", health.Healthy},
				&MockChecker{"checker2", health.Healthy},
			},
			expectedStatus: health.Healthy,
		},
		{
			name: "one degraded",
			checkers: []health.Checker{
				&MockChecker{"checker1", health.Healthy},
				&MockChecker{"checker2", health.Degraded},
			},
			expectedStatus: health.Degraded,
		},
		{
			name: "one unhealthy",
			checkers: []health.Checker{
				&MockChecker{"checker1", health.Healthy},
				&MockChecker{"checker2", health.Unhealthy},
			},
			expectedStatus: health.Unhealthy,
		},
		{
			name: "one critical overrides all",
			checkers: []health.Checker{
				&MockChecker{"checker1", health.Healthy},
				&MockChecker{"checker2", health.Degraded},
				&MockChecker{"checker3", health.Critical},
			},
			expectedStatus: health.Critical,
		},
		{
			name: "worst status wins",
			checkers: []health.Checker{
				&MockChecker{"checker1", health.Degraded},
				&MockChecker{"checker2", health.Unhealthy},
				&MockChecker{"checker3", health.Degraded},
			},
			expectedStatus: health.Unhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := health.NewCheck()
			for _, checker := range tt.checkers {
				check.AddChecker(checker)
			}

			result := check.Check(context.Background())
			assert.Equal(t, tt.expectedStatus, result.Status)
		})
	}
}
