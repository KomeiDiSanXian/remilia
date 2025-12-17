package middleware

import (
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	})

	// 初始状态应该是 Closed
	assert.Equal(t, StateClosed, cb.GetState())

	// 模拟 3 次失败
	for i := 0; i < 3; i++ {
		cb.onFailure()
	}

	// 应该转为 Open
	assert.Equal(t, StateOpen, cb.GetState())
	assert.Equal(t, int32(3), cb.GetFailures())

	// 立即尝试执行应该被拒绝
	err := cb.canExecute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")

	// 等待重置超时
	time.Sleep(150 * time.Millisecond)

	// 应该可以进入半开状态
	err = cb.canExecute()
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 半开状态下成功，应该转为 Closed
	cb.onSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(0), cb.GetFailures())
}

func TestCircuitBreaker_HalfOpenMaxRequests(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	})

	// 触发熔断
	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// 等待进入半开状态
	time.Sleep(60 * time.Millisecond)

	// 前两个请求应该通过
	assert.NoError(t, cb.canExecute())
	assert.NoError(t, cb.canExecute())

	// 第三个请求应该被拒绝
	err := cb.canExecute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max requests exceeded")
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	})

	// 触发熔断
	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// 等待进入半开状态
	time.Sleep(60 * time.Millisecond)

	// 允许一个请求
	assert.NoError(t, cb.canExecute())
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 半开状态下失败，应该重新开启
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_Middleware(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	})

	middleware := CircuitBreakerMiddleware(cb)

	// 创建一个会失败的 handler
	failingHandler := func(ctx *remilia.Context) error {
		return errors.New("handler error")
	}

	wrappedHandler := middleware(failingHandler)

	// 模拟事件
	event := &dto.Payload{
		ID:   "test-event",
		Type: dto.C2CMessageCreate,
	}
	ctx := remilia.NewContext(event, nil)

	// 前 3 次失败
	for i := 0; i < 3; i++ {
		err := wrappedHandler(ctx)
		assert.Error(t, err)
		assert.Equal(t, "handler error", err.Error())
	}

	// 熔断器应该开启
	assert.Equal(t, StateOpen, cb.GetState())

	// 第 4 次应该被熔断器拒绝
	err := wrappedHandler(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	})

	// 2 次失败
	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, int32(2), cb.GetFailures())
	assert.Equal(t, StateClosed, cb.GetState())

	// 1 次成功应该重置失败计数
	cb.onSuccess()
	assert.Equal(t, int32(0), cb.GetFailures())
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         2,
		ResetTimeout:        1 * time.Hour, // 很长的超时
		HalfOpenMaxRequests: 1,
	})

	// 触发熔断
	cb.onFailure()
	cb.onFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// 手动重置
	cb.Reset()
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int32(0), cb.GetFailures())

	// 应该可以正常执行
	assert.NoError(t, cb.canExecute())
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	})

	// 初始统计
	stats := cb.Stats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, int32(0), stats.Failures)
	assert.True(t, stats.LastFailure.IsZero())

	// 模拟失败
	cb.onFailure()
	cb.onFailure()

	stats = cb.Stats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, int32(2), stats.Failures)
	assert.False(t, stats.LastFailure.IsZero())
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	stateChanges := []struct {
		from CircuitBreakerState
		to   CircuitBreakerState
	}{}

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  2,
		ResetTimeout: 50 * time.Millisecond,
		OnStateChange: func(from, to CircuitBreakerState) {
			stateChanges = append(stateChanges, struct {
				from CircuitBreakerState
				to   CircuitBreakerState
			}{from, to})
		},
	})

	// 触发熔断
	cb.onFailure()
	cb.onFailure()

	// 应该有一个状态变化: Closed -> Open
	assert.Len(t, stateChanges, 1)
	assert.Equal(t, StateClosed, stateChanges[0].from)
	assert.Equal(t, StateOpen, stateChanges[0].to)

	// 等待进入半开状态
	time.Sleep(60 * time.Millisecond)
	_ = cb.canExecute()

	// 应该有两个状态变化: Open -> HalfOpen
	assert.Len(t, stateChanges, 2)
	assert.Equal(t, StateOpen, stateChanges[1].from)
	assert.Equal(t, StateHalfOpen, stateChanges[1].to)

	// 成功，转为闭合
	cb.onSuccess()

	// 应该有三个状态变化: HalfOpen -> Closed
	assert.Len(t, stateChanges, 3)
	assert.Equal(t, StateHalfOpen, stateChanges[2].from)
	assert.Equal(t, StateClosed, stateChanges[2].to)
}

func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	// 测试默认配置
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	assert.Equal(t, 5, cb.config.MaxFailures)
	assert.Equal(t, 30*time.Second, cb.config.ResetTimeout)
	assert.Equal(t, 1, cb.config.HalfOpenMaxRequests)
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         10,
		ResetTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests: 3,
	})

	// 并发访问
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			cb.onFailure()
			cb.onSuccess()
			_ = cb.GetState()
			_ = cb.GetFailures()
			_ = cb.Stats()
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 100; i++ {
		<-done
	}

	// 应该没有 panic
	assert.NotNil(t, cb)
}
