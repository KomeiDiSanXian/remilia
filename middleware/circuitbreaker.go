package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
	failures        atomic.Int32 // 当前失败次数
	successes       atomic.Int32 // 半开状态下的连续成功次数
	state           atomic.Value // CircuitBreakerState
	lastFailure     atomic.Value // time.Time
	halfOpenReqs    atomic.Int32 // 半开状态下的请求数
	halfOpenStarted atomic.Value // time.Time - 半开状态开始时间

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

	// SuccessThreshold 半开状态下连续成功多少次后转为关闭状态
	// 默认为 1，表示一次成功就关闭
	SuccessThreshold int

	// HalfOpenTimeout 半开状态的超时时间
	// 如果在此时间内没有足够的成功请求，重新打开熔断器
	// 默认为 0，表示不超时
	HalfOpenTimeout time.Duration

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
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 1
	}
	if config.HalfOpenTimeout <= 0 {
		config.HalfOpenTimeout = 10 * time.Second // 默认 10 秒
	}

	cb := &CircuitBreaker{
		config: config,
	}
	cb.state.Store(StateClosed)
	cb.lastFailure.Store(time.Time{})
	cb.halfOpenStarted.Store(time.Time{})

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
// 使用 mu 锁保护整个 Load-Compare-Store-Callback 序列，防止并发调用时
// OnStateChange 回调被重复触发（例如多个 goroutine 同时达到失败阈值时）。
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setStateLocked(newState)
}

// setStateLocked 在已持有 mu 的情况下设置状态。
// 供 canExecute 等已持锁路径调用，避免重复加锁。
func (cb *CircuitBreaker) setStateLocked(newState CircuitBreakerState) {
	oldState := cb.state.Load().(CircuitBreakerState)
	if oldState == newState {
		return
	}

	cb.state.Store(newState)

	logger.WithFields(logger.Fields{
		"from": oldState,
		"to":   newState,
	}).Info("[CircuitBreaker] State changed")

	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(oldState, newState)
	}
}

// canExecute 检查是否可以执行请求
func (cb *CircuitBreaker) canExecute() error {
	// 使用循环代替递归，避免栈溢出
	for {
		state := cb.GetState()

		switch state {
		case StateClosed:
			return nil

		case StateOpen:
			// 检查是否可以进入半开状态
			lastFail := cb.lastFailure.Load().(time.Time)
			if !lastFail.IsZero() && time.Since(lastFail) >= cb.config.ResetTimeout {
				// 尝试转换到半开状态 - 需要使用锁保护这个复杂操作
				cb.mu.Lock()
				// 再次检查状态（double-check）
				if cb.GetState() == StateOpen {
					// 转换状态
					cb.halfOpenReqs.Store(1) // Count this request
					cb.successes.Store(0)    // 重置成功计数
					cb.halfOpenStarted.Store(time.Now())
					cb.setStateLocked(StateHalfOpen)
					cb.mu.Unlock()
					return nil
				}
				// 状态已被其他请求改变，释放锁后重新检查
				cb.mu.Unlock()
				// 继续循环重新检查当前状态
				continue
			}
			return fmt.Errorf("circuit breaker is open")

		case StateHalfOpen:
			// 检查半开状态是否超时
			halfOpenStart := cb.halfOpenStarted.Load().(time.Time)
			if !halfOpenStart.IsZero() && cb.config.HalfOpenTimeout > 0 {
				if time.Since(halfOpenStart) >= cb.config.HalfOpenTimeout {
					// 半开状态超时，使用锁保护状态转换
					cb.mu.Lock()
					// Double-check 状态
					if cb.GetState() == StateHalfOpen {
						cb.lastFailure.Store(time.Now())
						cb.setStateLocked(StateOpen)
						cb.mu.Unlock()
						return fmt.Errorf("circuit breaker is open")
					}
					cb.mu.Unlock()
					// 状态已改变，继续循环重新检查
					continue
				}
			}

			// 半开状态下限制请求数量 - 使用 CAS 确保原子性，限制重试次数
			for range 100 {
				current := cb.halfOpenReqs.Load()
				if current >= int32(cb.config.HalfOpenMaxRequests) {
					return fmt.Errorf("circuit breaker is half-open, max requests exceeded")
				}
				// 原子地增加计数
				if cb.halfOpenReqs.CompareAndSwap(current, current+1) {
					return nil
				}
				// CAS 失败，重试
			}
			// CAS 重试次数过多，返回错误
			return fmt.Errorf("circuit breaker: too many concurrent state transitions")

		default:
			return fmt.Errorf("unknown circuit breaker state: %s", state)
		}
	}
}

// onSuccess 记录成功
func (cb *CircuitBreaker) onSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.GetState()

	switch state {
	case StateClosed:
		// 闭合状态下成功，重置失败计数
		cb.failures.Store(0)

	case StateHalfOpen:
		// 半开状态下成功，增加成功计数
		successes := cb.successes.Add(1)

		// 检查是否达到成功阈值
		if successes >= int32(cb.config.SuccessThreshold) {
			// 达到阈值，转为闭合状态
			cb.failures.Store(0)
			cb.successes.Store(0)
			cb.halfOpenReqs.Store(0) // 重置半开请求计数，避免下次进入半开状态时计数残留
			cb.setStateLocked(StateClosed)
			logger.WithField("successes", successes).Info("[CircuitBreaker] Service recovered, transitioning to closed state")
		} else {
			logger.WithFields(logger.Fields{
				"successes": successes,
				"threshold": cb.config.SuccessThreshold,
			}).Debug("[CircuitBreaker] Success in half-open state, waiting for threshold")
		}
	}
}

// onFailure 记录失败
func (cb *CircuitBreaker) onFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.GetState()
	cb.lastFailure.Store(time.Now())

	switch state {
	case StateClosed:
		// 闭合状态下失败，增加失败计数
		failures := cb.failures.Add(1)
		if failures >= int32(cb.config.MaxFailures) {
			cb.setStateLocked(StateOpen)
			logger.WithField("failures", failures).Warn("[CircuitBreaker] Max failures reached, opening circuit")
		}

	case StateHalfOpen:
		// 半开状态下失败，直接转为开启
		cb.failures.Store(int32(cb.config.MaxFailures)) // 设置为最大值
		cb.setStateLocked(StateOpen)
		logger.Warn("[CircuitBreaker] Failed in half-open state, reopening circuit")
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
				logger.WithError(err).WithFields(logger.Fields{
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
	cb.successes.Store(0)
	cb.halfOpenReqs.Store(0)
	cb.setState(StateClosed)
	logger.Info("[CircuitBreaker] Manually reset to closed state")
}

// UpdateConfig 热更新熔断器配置（线程安全，下一次状态判断时生效）
func (cb *CircuitBreaker) UpdateConfig(cfg CircuitBreakerConfig) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cfg.MaxFailures > 0 {
		cb.config.MaxFailures = cfg.MaxFailures
	}
	if cfg.ResetTimeout > 0 {
		cb.config.ResetTimeout = cfg.ResetTimeout
	}
	if cfg.HalfOpenMaxRequests > 0 {
		cb.config.HalfOpenMaxRequests = cfg.HalfOpenMaxRequests
	}
	if cfg.SuccessThreshold > 0 {
		cb.config.SuccessThreshold = cfg.SuccessThreshold
	}
	if cfg.HalfOpenTimeout > 0 {
		cb.config.HalfOpenTimeout = cfg.HalfOpenTimeout
	}
	logger.Info("[CircuitBreaker] Config updated via hot-reload")
}

// Stats 获取熔断器统计信息
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	lastFail := cb.lastFailure.Load().(time.Time)
	halfOpenStart := cb.halfOpenStarted.Load().(time.Time)
	return CircuitBreakerStats{
		State:             cb.GetState(),
		Failures:          cb.GetFailures(),
		Successes:         cb.successes.Load(),
		LastFailure:       lastFail,
		HalfOpenReqs:      cb.halfOpenReqs.Load(),
		HalfOpenStartTime: halfOpenStart,
	}
}

// CircuitBreakerStats 熔断器统计信息
type CircuitBreakerStats struct {
	State             CircuitBreakerState
	Failures          int32
	Successes         int32
	LastFailure       time.Time
	HalfOpenReqs      int32
	HalfOpenStartTime time.Time
}
