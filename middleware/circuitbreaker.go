package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/sirupsen/logrus"
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState string

const (
	// StateClosed 闭合状态：正常工作
	StateClosed CircuitBreakerState = "closed"
	// StateOpen 开启状态：拒绝请求
	StateOpen CircuitBreakerState = "open"
	// StateHalfOpen 半开状态：尝试恢复
	StateHalfOpen CircuitBreakerState = "half-open"
)

// CircuitBreaker 熔断器
//
// 熔断器用于防止故障级联，当连续失败次数达到阈值时自动开启熔断，
// 在熔断期间拒绝所有请求，一段时间后进入半开状态尝试恢复。
//
// 状态转换：
//   - Closed -> Open: 连续失败达到阈值
//   - Open -> HalfOpen: 超过重置超时时间
//   - HalfOpen -> Closed: 半开状态下成功
//   - HalfOpen -> Open: 半开状态下失败
//
// 使用示例：
//
//	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
//	    MaxFailures: 5,
//	    ResetTimeout: 30 * time.Second,
//	    HalfOpenMaxRequests: 3,
//	})
//	engine.Use(middleware.CircuitBreakerMiddleware(cb))
type CircuitBreaker struct {
	config CircuitBreakerConfig

	// 原子操作字段
	failures     atomic.Int32 // 当前失败次数
	state        atomic.Value // CircuitBreakerState
	lastFailure  atomic.Value // time.Time
	halfOpenReqs atomic.Int32 // 半开状态下的请求数

	// 保护状态转换的互斥锁
	mu sync.Mutex
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	// MaxFailures 触发熔断的最大失败次数
	MaxFailures int

	// ResetTimeout 熔断器重置超时时间（从 Open 到 HalfOpen）
	ResetTimeout time.Duration

	// HalfOpenMaxRequests 半开状态下允许的最大请求数
	// 用于测试服务是否恢复，默认为 1
	HalfOpenMaxRequests int

	// OnStateChange 状态变化回调（可选）
	OnStateChange func(from, to CircuitBreakerState)
}

// NewCircuitBreaker 创建一个新的熔断器
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.ResetTimeout <= 0 {
		config.ResetTimeout = 30 * time.Second
	}
	if config.HalfOpenMaxRequests <= 0 {
		config.HalfOpenMaxRequests = 1
	}

	cb := &CircuitBreaker{
		config: config,
	}
	cb.state.Store(StateClosed)
	cb.lastFailure.Store(time.Time{})

	return cb
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	return cb.state.Load().(CircuitBreakerState)
}

// GetFailures 获取当前失败次数
func (cb *CircuitBreaker) GetFailures() int32 {
	return cb.failures.Load()
}

// setState 设置状态（内部方法）
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
	oldState := cb.GetState()
	if oldState == newState {
		return
	}

	cb.state.Store(newState)

	logrus.WithFields(logrus.Fields{
		"from": oldState,
		"to":   newState,
	}).Info("[CircuitBreaker] State changed")

	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(oldState, newState)
	}
}

// canExecute 检查是否可以执行请求
func (cb *CircuitBreaker) canExecute() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.GetState()

	switch state {
	case StateClosed:
		return nil

	case StateOpen:
		// 检查是否可以进入半开状态
		lastFail := cb.lastFailure.Load().(time.Time)
		if !lastFail.IsZero() && time.Since(lastFail) >= cb.config.ResetTimeout {
			// 原子地转换状态并重置计数器
			cb.state.Store(StateHalfOpen)
			cb.halfOpenReqs.Store(1) // Count this request

			logrus.Info("[CircuitBreaker] Transitioning from Open to HalfOpen")
			if cb.config.OnStateChange != nil {
				cb.config.OnStateChange(StateOpen, StateHalfOpen)
			}
			return nil
		}
		return fmt.Errorf("circuit breaker is open")

	case StateHalfOpen:
		// 半开状态下限制请求数量
		// 使用原子递增并检查，避免竞态
		reqs := cb.halfOpenReqs.Add(1)
		if reqs > int32(cb.config.HalfOpenMaxRequests) {
			// 超过限制，回退计数
			cb.halfOpenReqs.Add(-1)
			return fmt.Errorf("circuit breaker is half-open, max requests exceeded")
		}
		return nil

	default:
		return fmt.Errorf("unknown circuit breaker state: %s", state)
	}
}

// onSuccess 记录成功
func (cb *CircuitBreaker) onSuccess() {
	state := cb.GetState()

	switch state {
	case StateClosed:
		// 闭合状态下成功，重置失败计数
		cb.failures.Store(0)

	case StateHalfOpen:
		// 半开状态下成功，转为闭合
		cb.failures.Store(0)
		cb.setState(StateClosed)
		logrus.Info("[CircuitBreaker] Service recovered, transitioning to closed state")
	}
}

// onFailure 记录失败
func (cb *CircuitBreaker) onFailure() {
	state := cb.GetState()
	cb.lastFailure.Store(time.Now())

	switch state {
	case StateClosed:
		// 闭合状态下失败，增加失败计数
		failures := cb.failures.Add(1)
		if failures >= int32(cb.config.MaxFailures) {
			cb.setState(StateOpen)
			logrus.WithField("failures", failures).Warn("[CircuitBreaker] Max failures reached, opening circuit")
		}

	case StateHalfOpen:
		// 半开状态下失败，直接转为开启
		cb.failures.Store(int32(cb.config.MaxFailures)) // 设置为最大值
		cb.setState(StateOpen)
		logrus.Warn("[CircuitBreaker] Failed in half-open state, reopening circuit")
	}
}

// CircuitBreakerMiddleware 熔断器中间件
//
// 使用示例：
//
//	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
//	    MaxFailures: 5,
//	    ResetTimeout: 30 * time.Second,
//	})
//	engine.Use(middleware.CircuitBreakerMiddleware(cb))
func CircuitBreakerMiddleware(cb *CircuitBreaker) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			// 检查是否可以执行
			if err := cb.canExecute(); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"event_type": ctx.GetEventType(),
					"state":      cb.GetState(),
				}).Warn("[CircuitBreaker] Request rejected")
				return err
			}

			// 执行请求
			err := next(ctx)

			// 记录结果
			if err != nil {
				cb.onFailure()
			} else {
				cb.onSuccess()
			}

			return err
		}
	}
}

// Reset 手动重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.failures.Store(0)
	cb.halfOpenReqs.Store(0)
	cb.setState(StateClosed)
	logrus.Info("[CircuitBreaker] Manually reset to closed state")
}

// Stats 获取熔断器统计信息
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	lastFail := cb.lastFailure.Load().(time.Time)
	return CircuitBreakerStats{
		State:        cb.GetState(),
		Failures:     cb.GetFailures(),
		LastFailure:  lastFail,
		HalfOpenReqs: cb.halfOpenReqs.Load(),
	}
}

// CircuitBreakerStats 熔断器统计信息
type CircuitBreakerStats struct {
	State        CircuitBreakerState
	Failures     int32
	LastFailure  time.Time
	HalfOpenReqs int32
}
