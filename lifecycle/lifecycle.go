package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// State 生命周期状态
type State int

const (
	// StateCreated 已创建
	StateCreated State = iota
	// StateStarting 启动中
	StateStarting
	// StateRunning 运行中
	StateRunning
	// StateStopping 停止中
	StateStopping
	// StateStopped 已停止
	StateStopped
	// StateFailed 失败
	StateFailed
)

// String 返回状态字符串
func (s State) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Component 生命周期组件接口
type Component interface {
	// Name 返回组件名称
	Name() string

	// Start 启动组件
	Start(ctx context.Context) error

	// Stop 停止组件
	Stop(ctx context.Context) error
}

// Manager 生命周期管理器
type Manager struct {
	components []Component
	state      State
	mu         sync.RWMutex

	startTime time.Time
	stopTime  time.Time
}

// NewManager 创建新的生命周期管理器
func NewManager() *Manager {
	return &Manager{
		components: make([]Component, 0),
		state:      StateCreated,
	}
}

// Register 注册组件
func (m *Manager) Register(component Component) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.components = append(m.components, component)
	logger.WithField("component", component.Name()).Debug("[Lifecycle] Component registered")
}

// Start 启动所有组件
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.state != StateCreated && m.state != StateStopped {
		m.mu.Unlock()
		return ErrInvalidState{Current: m.state, Expected: StateCreated}
	}
	m.state = StateStarting
	m.startTime = time.Now()
	components := append([]Component(nil), m.components...)
	m.mu.Unlock()

	logger.WithField("component_count", len(components)).Info("[Lifecycle] Starting components")

	// 按顺序启动所有组件
	for i, comp := range components {
		if err := comp.Start(ctx); err != nil {
			logger.WithError(err).
				WithField("component", comp.Name()).
				WithField("index", i).
				Error("[Lifecycle] Component start failed")

			// 启动失败，回滚已启动的组件
			m.rollbackStart(components[:i])

			m.mu.Lock()
			m.state = StateFailed
			m.mu.Unlock()

			return &StartError{Component: comp.Name(), Err: err}
		}

		logger.WithField("component", comp.Name()).Debug("[Lifecycle] Component started")
	}

	m.mu.Lock()
	m.state = StateRunning
	m.mu.Unlock()

	logger.Info("[Lifecycle] All components started successfully")
	return nil
}

// Stop 停止所有组件
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.state != StateRunning {
		m.mu.Unlock()
		return ErrInvalidState{Current: m.state, Expected: StateRunning}
	}
	m.state = StateStopping
	m.stopTime = time.Now()
	components := append([]Component(nil), m.components...)
	m.mu.Unlock()

	logger.WithField("component_count", len(components)).Info("[Lifecycle] Stopping components")

	// 按逆序停止所有组件
	var lastErr error
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]

		// 检查 context 是否已超时
		select {
		case <-ctx.Done():
			logger.WithError(ctx.Err()).
				WithField("remaining_components", i+1).
				Warn("[Lifecycle] Stop timeout, aborting remaining components")
			m.mu.Lock()
			m.state = StateFailed
			m.mu.Unlock()
			return &StopError{Err: ctx.Err()}
		default:
		}

		// 为每个组件创建子 context，避免单个组件阻塞整个关闭流程
		compCtx, compCancel := context.WithTimeout(ctx, 10*time.Second)
		err := comp.Stop(compCtx)
		compCancel()

		if err != nil {
			logger.WithError(err).
				WithField("component", comp.Name()).
				Error("[Lifecycle] Component stop failed")
			lastErr = err
			// 继续停止其他组件
		} else {
			logger.WithField("component", comp.Name()).Debug("[Lifecycle] Component stopped")
		}
	}

	m.mu.Lock()
	if lastErr != nil {
		m.state = StateFailed
	} else {
		m.state = StateStopped
	}
	m.mu.Unlock()

	if lastErr != nil {
		return &StopError{Err: lastErr}
	}

	logger.Info("[Lifecycle] All components stopped successfully")
	return nil
}

// rollbackStart 回滚已启动的组件
// 使用独立的超时 context 避免被父 context 取消影响
func (m *Manager) rollbackStart(components []Component) {
	logger.WithField("count", len(components)).Warn("[Lifecycle] Rolling back started components")

	// 使用独立的超时 context，确保回滚有足够时间完成
	// 即使父 context 已取消，回滚仍应继续
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var rollbackErrors []error
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]

		// 为每个组件创建子 context，避免单个组件阻塞整个回滚
		compCtx, compCancel := context.WithTimeout(rollbackCtx, 10*time.Second)
		// 使用 defer 确保即使 comp.Stop() panic 也能释放 context
		func() {
			defer compCancel()
			err := comp.Stop(compCtx)

			if err != nil {
				logger.WithError(err).
					WithField("component", comp.Name()).
					Error("[Lifecycle] Component rollback failed")
				rollbackErrors = append(rollbackErrors, err)
			} else {
				logger.WithField("component", comp.Name()).Debug("[Lifecycle] Component rolled back successfully")
			}
		}()
	}

	if len(rollbackErrors) > 0 {
		logger.WithField("error_count", len(rollbackErrors)).
			Error("[Lifecycle] Rollback completed with errors, some resources may not be released")
	} else {
		logger.Info("[Lifecycle] Rollback completed successfully")
	}
}

// State 获取当前状态
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Uptime 获取运行时间
func (m *Manager) Uptime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.state == StateRunning {
		return time.Since(m.startTime)
	}
	if m.state == StateStopped && !m.stopTime.IsZero() {
		return m.stopTime.Sub(m.startTime)
	}
	return 0
}

// ComponentCount 获取组件数量
func (m *Manager) ComponentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.components)
}

// StartError 启动错误
type StartError struct {
	Component string
	Err       error
}

func (e *StartError) Error() string {
	return "lifecycle: component '" + e.Component + "' start failed: " + e.Err.Error()
}

func (e *StartError) Unwrap() error {
	return e.Err
}

// StopError 停止错误
type StopError struct {
	Err error
}

func (e *StopError) Error() string {
	return "lifecycle: stop failed: " + e.Err.Error()
}

func (e *StopError) Unwrap() error {
	return e.Err
}

// ErrInvalidState 无效状态错误
type ErrInvalidState struct {
	Current  State
	Expected State
}

func (e ErrInvalidState) Error() string {
	return "lifecycle: invalid state: current=" + e.Current.String() + ", expected=" + e.Expected.String()
}

// SimpleComponent 简单组件实现
type SimpleComponent struct {
	name      string
	startFunc func(context.Context) error
	stopFunc  func(context.Context) error
}

// NewSimpleComponent 创建简单组件
func NewSimpleComponent(name string, start, stop func(context.Context) error) Component {
	return &SimpleComponent{
		name:      name,
		startFunc: start,
		stopFunc:  stop,
	}
}

func (c *SimpleComponent) Name() string {
	return c.name
}

func (c *SimpleComponent) Start(ctx context.Context) error {
	if c.startFunc != nil {
		return c.startFunc(ctx)
	}
	return nil
}

func (c *SimpleComponent) Stop(ctx context.Context) error {
	if c.stopFunc != nil {
		return c.stopFunc(ctx)
	}
	return nil
}
