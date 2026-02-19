package health

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineHealthChecker_Name(t *testing.T) {
	checker := NewEngineHealthChecker(nil)
	assert.Equal(t, "engine", checker.Name())
}

func TestEngineHealthChecker_Check_NilEngine(t *testing.T) {
	checker := NewEngineHealthChecker(nil)
	ctx := stdctx.Background()
	result := checker.Check(ctx)
	assert.Equal(t, Unhealthy, result.Status)
	assert.Equal(t, "engine is nil", result.Error)
}

func TestEngineHealthChecker_Check_HealthyEngine(t *testing.T) {
	eng := engine.NewEngine()
	require.NotNil(t, eng)
	checker := NewEngineHealthChecker(eng)
	ctx := stdctx.Background()
	result := checker.Check(ctx)
	assert.Equal(t, Healthy, result.Status)
	assert.Empty(t, result.Error)
	assert.NotNil(t, result.Metadata)
	matcherCount, ok := result.Metadata["matcher_count"]
	assert.True(t, ok)
	assert.GreaterOrEqual(t, matcherCount, 0)
}

func TestEngineHealthChecker_Check_WithMatchers(t *testing.T) {
	t.Skip("engine matcher setup requires more complex initialization")
}

func TestDeadLetterQueueHealthChecker_Name(t *testing.T) {
	checker := NewDeadLetterQueueHealthChecker(nil, 1000, 0.1)
	assert.Equal(t, "dead_letter_queue", checker.Name())
}

func TestDeadLetterQueueHealthChecker_Check_NilDLQ(t *testing.T) {
	checker := NewDeadLetterQueueHealthChecker(nil, 1000, 0.1)
	ctx := stdctx.Background()
	result := checker.Check(ctx)
	assert.Equal(t, Healthy, result.Status)
	assert.NotNil(t, result.Metadata)
	assert.False(t, result.Metadata["enabled"].(bool))
}

func TestDeadLetterQueueHealthChecker_Check_HealthyDLQ(t *testing.T) {
	dlqInstance := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{MaxSize: 100, Workers: 2})
	require.NotNil(t, dlqInstance)
	checker := NewDeadLetterQueueHealthChecker(dlqInstance, 50, 0.1)
	ctx := stdctx.Background()
	result := checker.Check(ctx)
	assert.Equal(t, Healthy, result.Status)
	assert.Empty(t, result.Error)
	assert.NotNil(t, result.Metadata)
	metadata := result.Metadata
	assert.Contains(t, metadata, "queue_size")
	assert.Contains(t, metadata, "max_size")
	assert.Contains(t, metadata, "processed")
	assert.Contains(t, metadata, "dropped")
	assert.Contains(t, metadata, "workers")
	assert.Contains(t, metadata, "dropped_rate")
}

func TestDeadLetterQueueHealthChecker_Check_QueueSizeExceedsThreshold(t *testing.T) {
	dlqInstance := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{MaxSize: 10, Workers: 1})
	require.NotNil(t, dlqInstance)
	for i := range 8 {
		dlqInstance.Enqueue(dlq.DeadLetterItem{Event: &dto.Payload{ID: dto.EventID("test-" + string(rune('0'+i)))}})
	}
	checker := NewDeadLetterQueueHealthChecker(dlqInstance, 5, 0.1)
	ctx := stdctx.Background()
	result := checker.Check(ctx)
	assert.Equal(t, Degraded, result.Status)
	assert.Contains(t, result.Error, "queue size")
	assert.Contains(t, result.Error, "exceeds threshold")
}

func TestDeadLetterQueueHealthChecker_DefaultThresholds(t *testing.T) {
	tests := []struct {
		name              string
		maxQueueSize      int
		maxDroppedRate    float64
		expectedQueueSize int
		expectedDropRate  float64
	}{
		{"zero values use defaults", 0, 0, 1000, 0.1},
		{"negative values use defaults", -1, -0.5, 1000, 0.1},
		{"custom values", 500, 0.05, 500, 0.05},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewDeadLetterQueueHealthChecker(nil, tt.maxQueueSize, tt.maxDroppedRate)
			assert.Equal(t, tt.expectedQueueSize, checker.maxQueueSize)
			assert.Equal(t, tt.expectedDropRate, checker.maxDroppedRate)
		})
	}
}

func TestEngineHealthChecker_Integration(t *testing.T) {
	eng := engine.NewEngine()
	require.NotNil(t, eng)
	check := NewCheck()
	check.AddChecker(NewEngineHealthChecker(eng))
	ctx := stdctx.Background()
	response := check.Check(ctx)
	assert.Equal(t, Healthy, response.Status)
	assert.Contains(t, response.Checks, "engine")
	engineResult := response.Checks["engine"]
	assert.Equal(t, Healthy, engineResult.Status)
	// Just verify matcher_count exists, don't check specific value
	_, ok := engineResult.Metadata["matcher_count"]
	assert.True(t, ok)
}

func TestDeadLetterQueueHealthChecker_Integration(t *testing.T) {
	dlqInstance := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{MaxSize: 100, Workers: 2})
	require.NotNil(t, dlqInstance)
	dlqInstance.Start()
	defer func() {
		ctx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
		defer cancel()
		_ = dlqInstance.Shutdown(ctx)
	}()
	check := NewCheck()
	check.AddChecker(NewDeadLetterQueueHealthChecker(dlqInstance, 50, 0.1))
	ctx := stdctx.Background()
	response := check.Check(ctx)
	assert.Equal(t, Healthy, response.Status)
	assert.Contains(t, response.Checks, "dead_letter_queue")
	dlqResult := response.Checks["dead_letter_queue"]
	assert.Equal(t, Healthy, dlqResult.Status)
	assert.Contains(t, dlqResult.Metadata, "queue_size")
	assert.Contains(t, dlqResult.Metadata, "workers")
}

func TestMultipleCheckers_Integration(t *testing.T) {
	eng := engine.NewEngine()
	require.NotNil(t, eng)
	dlqInstance := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{MaxSize: 100, Workers: 2})
	require.NotNil(t, dlqInstance)
	dlqInstance.Start()
	defer func() {
		ctx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
		defer cancel()
		_ = dlqInstance.Shutdown(ctx)
	}()
	check := NewCheck()
	check.AddChecker(NewEngineHealthChecker(eng))
	check.AddChecker(NewDeadLetterQueueHealthChecker(dlqInstance, 50, 0.1))
	ctx := stdctx.Background()
	response := check.Check(ctx)
	assert.Equal(t, Healthy, response.Status)
	assert.Len(t, response.Checks, 2)
	assert.Contains(t, response.Checks, "engine")
	assert.Contains(t, response.Checks, "dead_letter_queue")
	for name, result := range response.Checks {
		assert.Equal(t, Healthy, result.Status, "checker %s should be healthy", name)
	}
}

func BenchmarkEngineHealthChecker(b *testing.B) {
	eng := engine.NewEngine()
	checker := NewEngineHealthChecker(eng)
	ctx := stdctx.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = checker.Check(ctx)
	}
}

func BenchmarkDeadLetterQueueHealthChecker(b *testing.B) {
	dlqInstance := dlq.NewDeadLetterQueue(dlq.DeadLetterQueueConfig{MaxSize: 100, Workers: 2})
	checker := NewDeadLetterQueueHealthChecker(dlqInstance, 50, 0.1)
	ctx := stdctx.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = checker.Check(ctx)
	}
}
