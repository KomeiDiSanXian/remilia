package resilience

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// CircuitBreakerSnapshot 熔断器状态的持久化快照。
type CircuitBreakerSnapshot struct {
	State             CircuitBreakerState
	Failures          int32
	Successes         int32
	LastFailure       time.Time
	HalfOpenReqs      int32
	HalfOpenStartTime time.Time
}

// StatePersister 熔断器状态的持久化接口。
// 设置后状态会在创建时从 Load 恢复，在每次状态转换时 Save。
type StatePersister interface {
	Load() (CircuitBreakerSnapshot, error)
	Save(snapshot CircuitBreakerSnapshot) error
}

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

// circuitBreakerMetrics 持有 CircuitBreaker 的 Prometheus 指标
type circuitBreakerMetrics struct {
	stateGauge  prometheus.Gauge
	tripsTotal  prometheus.Counter
	rejectTotal prometheus.Counter
}

func newCircuitBreakerMetrics(namespace string) *circuitBreakerMetrics {
	return &circuitBreakerMetrics{
		stateGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "circuitbreaker", Name: "state",
			Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
		}),
		tripsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "circuitbreaker", Name: "trips_total",
			Help: "Total number of circuit breaker trips (closed→open transitions)",
		}),
		rejectTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "circuitbreaker", Name: "rejected_total",
			Help: "Total number of requests rejected by open circuit breaker",
		}),
	}
}

func registerCircuitBreakerMetrics(reg prometheus.Registerer, m *circuitBreakerMetrics) {
	m.stateGauge = inframetrics.MustRegisterOrGet(reg, m.stateGauge).(prometheus.Gauge)
	m.tripsTotal = inframetrics.MustRegisterOrGet(reg, m.tripsTotal).(prometheus.Counter)
	m.rejectTotal = inframetrics.MustRegisterOrGet(reg, m.rejectTotal).(prometheus.Counter)
}

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
	state           infraatomic.Value[CircuitBreakerState]
	lastFailure     infraatomic.Value[time.Time]
	halfOpenReqs    atomic.Int32                 // 半开状态下的请求数
	halfOpenStarted infraatomic.Value[time.Time] // 半开状态开始时间

	// 保护状态转换的互斥锁
	mu sync.Mutex

	// Prometheus 指标
	metrics *circuitBreakerMetrics
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

	// Registerer 自定义 Prometheus 注册器（可选，nil 时使用 DefaultRegisterer）
	Registerer prometheus.Registerer

	// Persister 可选持久化接口。
	// 设置后在启动时调用 Load() 恢复熔断器状态，
	// 每次状态转换后调用 Save() 持久化快照。
	// nil 表示不启用持久化。
	Persister StatePersister
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
	// SuccessThreshold 不能大于 HalfOpenMaxRequests：每个半开窗口最多只放行
	// HalfOpenMaxRequests 个探测请求，且成功计数在进入半开时被清零，
	// 因此累计成功数永远达不到更大的阈值，熔断器将在 开→半开 之间无限震荡，
	// 即使下游已完全恢复也永远无法闭合。
	if config.SuccessThreshold > config.HalfOpenMaxRequests {
		logger.WithFields(logger.Fields{
			"success_threshold":      config.SuccessThreshold,
			"half_open_max_requests": config.HalfOpenMaxRequests,
		}).Warn("[CircuitBreaker] SuccessThreshold > HalfOpenMaxRequests, clamping to HalfOpenMaxRequests")
		config.SuccessThreshold = config.HalfOpenMaxRequests
	}
	if config.HalfOpenTimeout <= 0 {
		config.HalfOpenTimeout = 10 * time.Second // 默认 10 秒
	}

	metrics := newCircuitBreakerMetrics("remilia")
	registerCircuitBreakerMetrics(config.Registerer, metrics)

	cb := &CircuitBreaker{
		config:  config,
		metrics: metrics,
	}
	cb.state.Store(StateClosed)
	cb.lastFailure.Store(time.Time{})
	cb.halfOpenStarted.Store(time.Time{})

	// 初始化 Prometheus 指标
	cb.metrics.stateGauge.Set(0) // 0 = closed

	// 从持久化恢复
	if config.Persister != nil {
		cb.loadPersisted()
	}

	return cb
}

// loadPersisted 从持久化存储恢复状态
func (cb *CircuitBreaker) loadPersisted() {
	snapshot, err := cb.config.Persister.Load()
	if err != nil {
		logger.WithError(err).Warn("[CircuitBreaker] Failed to load persisted state")
		return
	}
	// 恢复失败计数
	cb.failures.Store(snapshot.Failures)
	cb.successes.Store(snapshot.Successes)
	cb.halfOpenReqs.Store(snapshot.HalfOpenReqs)
	if !snapshot.LastFailure.IsZero() {
		cb.lastFailure.Store(snapshot.LastFailure)
	}
	if !snapshot.HalfOpenStartTime.IsZero() {
		cb.halfOpenStarted.Store(snapshot.HalfOpenStartTime)
	}
	// 验证并恢复状态
	state := snapshot.State
	switch state {
	case StateOpen, StateHalfOpen:
		cb.state.Store(state)
		cb.metrics.stateGauge.Set(float64(stateToInt(state)))
		logger.WithField("state", state).Info("[CircuitBreaker] Restored persisted state")
	default:
		cb.state.Store(StateClosed)
		cb.metrics.stateGauge.Set(0)
	}
}

// savePersisted 持久化当前状态快照
func (cb *CircuitBreaker) savePersisted() {
	p := cb.config.Persister
	if p == nil {
		return
	}
	snapshot := CircuitBreakerSnapshot{
		State:             cb.GetState(),
		Failures:          cb.failures.Load(),
		Successes:         cb.successes.Load(),
		LastFailure:       cb.lastFailure.Load(),
		HalfOpenReqs:      cb.halfOpenReqs.Load(),
		HalfOpenStartTime: cb.halfOpenStarted.Load(),
	}
	if err := p.Save(snapshot); err != nil {
		logger.WithError(err).Warn("[CircuitBreaker] Failed to persist state")
	}
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	return cb.state.Load()
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
	cb.savePersisted()
}

// setStateLocked 在已持有 mu 的情况下设置状态。
// 供 canExecute 等已持锁路径调用，避免重复加锁。
func (cb *CircuitBreaker) setStateLocked(newState CircuitBreakerState) {
	oldState := cb.state.Load()
	if oldState == newState {
		return
	}

	cb.state.Store(newState)

	// 更新 Prometheus 指标
	cb.metrics.stateGauge.Set(float64(stateToInt(newState)))

	// 记录熔断触发事件
	if newState == StateOpen {
		cb.metrics.tripsTotal.Inc()
	}

	logger.WithFields(logger.Fields{
		"from": oldState,
		"to":   newState,
	}).Info("[CircuitBreaker] State changed")

	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(oldState, newState)
	}
}

func stateToInt(s CircuitBreakerState) int {
	switch s {
	case StateClosed:
		return 0
	case StateHalfOpen:
		return 1
	case StateOpen:
		return 2
	default:
		return -1
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
			if ok := cb.tryTransitionToHalfOpen(); ok {
				return nil
			}
			// 检查后发现状态已被其他 goroutine 改变，重新循环检查
			if cb.GetState() != StateOpen {
				continue
			}
			cb.metrics.rejectTotal.Inc()
			return errutil.ErrCircuitBreakerOpen

		case StateHalfOpen:
			if err := cb.checkHalfOpenExpired(); err != nil {
				return err
			}
			if err := cb.acquireHalfOpenSlot(); err != nil {
				return err
			}
			return nil

		default:
			return fmt.Errorf("unknown circuit breaker state: %s", state)
		}
	}
}

// tryTransitionToHalfOpen 检查熔断器是否应过渡到半开状态。
// 若成功过渡返回 true；若因并发争用需要重试返回 false。
func (cb *CircuitBreaker) tryTransitionToHalfOpen() bool {
	lastFail := cb.lastFailure.Load()
	if lastFail.IsZero() {
		return false
	}
	cb.mu.Lock()
	resetTimeout := cb.config.ResetTimeout
	cb.mu.Unlock()
	if time.Since(lastFail) < resetTimeout {
		return false
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.GetState() != StateOpen {
		return false // 状态已被其他请求改变
	}
	cb.halfOpenReqs.Store(1)
	cb.successes.Store(0)
	cb.halfOpenStarted.Store(time.Now())
	cb.setStateLocked(StateHalfOpen)
	logger.Debug("[CircuitBreaker] Transitioned from open to half-open")
	return true
}

// checkHalfOpenExpired 检查半开状态是否已超时，超时则切回开启并返回错误。
func (cb *CircuitBreaker) checkHalfOpenExpired() error {
	halfOpenStart := cb.halfOpenStarted.Load()
	if halfOpenStart.IsZero() {
		return nil
	}
	cb.mu.Lock()
	halfOpenTimeout := cb.config.HalfOpenTimeout
	cb.mu.Unlock()
	if halfOpenTimeout <= 0 {
		return nil
	}
	if time.Since(halfOpenStart) < halfOpenTimeout {
		return nil
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.GetState() != StateHalfOpen {
		return nil // 状态已被其他请求改变
	}
	cb.lastFailure.Store(time.Now())
	cb.setStateLocked(StateOpen)
	logger.Debug("[CircuitBreaker] Half-open timeout expired, reopening")
	return errutil.ErrCircuitBreakerOpen
}

// acquireHalfOpenSlot 在半开状态下原子获取一个请求槽。
func (cb *CircuitBreaker) acquireHalfOpenSlot() error {
	cb.mu.Lock()
	halfOpenMaxReqs := cb.config.HalfOpenMaxRequests
	cb.mu.Unlock()
	for range 100 {
		current := cb.halfOpenReqs.Load()
		if current >= int32(halfOpenMaxReqs) {
			return fmt.Errorf("half-open slot exhausted (current=%d, max=%d): %w", current, halfOpenMaxReqs, errutil.ErrCircuitBreakerHalfOpen)
		}
		if cb.halfOpenReqs.CompareAndSwap(current, current+1) {
			return nil
		}
	}
	return errutil.ErrCircuitBreakerContention
}

// onSuccess 记录成功
func (cb *CircuitBreaker) onSuccess() {
	cb.mu.Lock()

	state := cb.GetState()

	switch state {
	case StateClosed:
		cb.failures.Store(0)
		cb.mu.Unlock()
		cb.savePersisted()
		return

	case StateHalfOpen:
		successes := cb.successes.Add(1)

		if successes >= int32(cb.config.SuccessThreshold) {
			cb.failures.Store(0)
			cb.successes.Store(0)
			cb.halfOpenReqs.Store(0)
			cb.setStateLocked(StateClosed)
			cb.mu.Unlock()
			cb.savePersisted()
			logger.WithField("successes", successes).Info("[CircuitBreaker] Service recovered, transitioning to closed state")
			return
		}
		cb.mu.Unlock()
		logger.WithFields(logger.Fields{
			"successes": successes,
			"threshold": cb.config.SuccessThreshold,
		}).Debug("[CircuitBreaker] Success in half-open state, waiting for threshold")
		return
	}

	cb.mu.Unlock()
}

// onFailure 记录失败
func (cb *CircuitBreaker) onFailure() {
	cb.mu.Lock()

	state := cb.GetState()
	cb.lastFailure.Store(time.Now())

	switch state {
	case StateClosed:
		failures := cb.failures.Add(1)
		if failures >= int32(cb.config.MaxFailures) {
			cb.setStateLocked(StateOpen)
			cb.mu.Unlock()
			cb.savePersisted()
			logger.WithField("failures", failures).Warn("[CircuitBreaker] Max failures reached, opening circuit")
			return
		}
		cb.mu.Unlock()
		cb.savePersisted()
		return

	case StateHalfOpen:
		cb.failures.Store(int32(cb.config.MaxFailures))
		cb.setStateLocked(StateOpen)
		cb.mu.Unlock()
		cb.savePersisted()
		logger.Warn("[CircuitBreaker] Failed in half-open state, reopening circuit")
		return
	}

	cb.mu.Unlock()
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
	// 热更新同样要维持该不变式，否则运行时改配置也能让熔断器永远无法闭合。
	if cb.config.SuccessThreshold > cb.config.HalfOpenMaxRequests {
		logger.WithFields(logger.Fields{
			"success_threshold":      cb.config.SuccessThreshold,
			"half_open_max_requests": cb.config.HalfOpenMaxRequests,
		}).Warn("[CircuitBreaker] SuccessThreshold > HalfOpenMaxRequests, clamping to HalfOpenMaxRequests")
		cb.config.SuccessThreshold = cb.config.HalfOpenMaxRequests
	}
	logger.Info("[CircuitBreaker] Config updated via hot-reload")
}

// Stats 获取熔断器统计信息
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	lastFail := cb.lastFailure.Load()
	halfOpenStart := cb.halfOpenStarted.Load()
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

// SimpleCircuitBreaker 创建带默认配置的熔断器中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleCircuitBreaker())
func SimpleCircuitBreaker() context.Middleware {
	config := CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
		HalfOpenTimeout:     10 * time.Second,
	}
	cb := NewCircuitBreaker(config)
	return CircuitBreakerMiddleware(cb)
}
