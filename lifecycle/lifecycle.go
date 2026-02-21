// Package lifecycle 提供改进的组件生命周期管理功能
//
// # 概述
//
// v2 包是 lifecycle 的重新设计版本，结合了：
//   - 方案1：显式的运行时 Context
//   - 方案3：生命周期 Hooks
//
// 这个设计解决了 v1 中 Context 语义不清晰的问题，使得组件开发更加直观和安全。
//
// # 核心概念
//
// ## Component 接口
//
// 组件需要实现三个生命周期方法：
//
//	type Component interface {
//	    Name() string
//	    OnStart(ctx context.Context) error  // 启动前准备
//	    OnRun(ctx context.Context) error    // 运行时逻辑（ctx 是运行时 context）
//	    OnStop(ctx context.Context) error   // 停止清理
//	}
//
// ## 生命周期阶段
//
//  1. **OnStart**: 初始化资源（非阻塞）
//     - 分配内存、打开文件、建立连接等
//     - 必须快速返回（不要启动长期运行的 goroutine）
//     - ctx 用于控制 OnStart 操作本身的超时
//
//  2. **OnRun**: 运行时逻辑（阻塞）
//     - 在独立 goroutine 中执行
//     - ctx 是运行时 context（✅ 这是关键！）
//     - 通过 <-ctx.Done() 监听停止信号
//     - 返回时表示组件运行结束
//
//  3. **OnStop**: 清理资源（非阻塞）
//     - 关闭连接、释放资源等
//     - ctx 用于控制 OnStop 操作本身的超时
//     - 必须是幂等的
//
// # 使用示例
//
// ## 基本组件实现
//
//	type MyAdapter struct {
//	    events chan Event
//	}
//
//	func (a *MyAdapter) Name() string {
//	    return "my-adapter"
//	}
//
//	func (a *MyAdapter) OnStart(ctx context.Context) error {
//	    // 初始化资源
//	    a.events = make(chan Event, 100)
//	    logger.Info("Adapter initialized")
//	    return nil
//	}
//
//	func (a *MyAdapter) OnRun(ctx context.Context) error {
//	    // 运行时逻辑（此方法在独立 goroutine 中执行）
//	    logger.Info("Adapter running")
//	    for {
//	        select {
//	        case <-ctx.Done():
//	            // ✅ ctx 是运行时 context，在 Stop 时被取消
//	            logger.Info("Adapter stopping")
//	            return nil
//	        case event := <-a.events:
//	            // 处理事件
//	            a.handleEvent(event)
//	        }
//	    }
//	}
//
//	func (a *MyAdapter) OnStop(ctx context.Context) error {
//	    // 清理资源
//	    close(a.events)
//	    logger.Info("Adapter stopped")
//	    return nil
//	}
//
// ## 使用 Manager
//
//	// 创建管理器
//	manager := v2.NewManager()
//
//	// 注册组件
//	manager.Register(&MyAdapter{})
//	manager.Register(&OtherComponent{})
//
//	// 启动所有组件
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	if err := manager.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	// ... 应用运行 ...
//
//	// 停止所有组件
//	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer stopCancel()
//	if err := manager.Stop(stopCtx); err != nil {
//	    log.Error(err)
//	}
//
// # 高级功能
//
// ## SimpleComponent - 简化创建
//
//	comp := v2.NewSimpleComponent(
//	    "my-component",
//	    func(ctx context.Context) error { return nil },  // onStart
//	    func(ctx context.Context) error { <-ctx.Done(); return nil },  // onRun
//	    func(ctx context.Context) error { return nil },  // onStop
//	)
//
// ## ResourceComponent - 资源管理
//
//	comp := v2.NewResourceComponent(
//	    "database",
//	    func(ctx context.Context) (interface{}, error) {
//	        return sql.Open("postgres", dsn)  // 打开资源
//	    },
//	    func(ctx context.Context, res interface{}) error {
//	        return res.(*sql.DB).Close()  // 关闭资源
//	    },
//	)
//
// ## 获取运行时 Context
//
//	// 在其他地方获取全局运行时 context
//	runCtx, ok := manager.RunContext()
//	if ok {
//	    go func() {
//	        <-runCtx.Done()
//	        // 全局停止信号
//	    }()
//	}
package lifecycle

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Component 定义组件生命周期接口
//
// 组件通过实现此接口来参与生命周期管理。Manager 会按照注册顺序
// 依次调用所有组件的 OnStart，然后在独立 goroutine 中调用 OnRun，
// 停止时逆序调用 OnStop。
//
// # 关键设计
//
// OnRun 方法的 ctx 参数是运行时 context（这是 v2 的核心改进）：
//   - Manager 在调用 Start() 后创建运行时 context
//   - 该 context 在调用 Stop() 时被取消
//   - 组件应该在 OnRun 中监听 ctx.Done() 来响应停止信号
//
// # 实现要求
//
//  1. Name() 必须返回唯一的组件名称
//  2. OnStart() 必须快速返回（不要启动 goroutine）
//  3. OnRun() 可以阻塞（在独立 goroutine 中运行）
//  4. OnStop() 必须是幂等的（可以多次调用）
type Component interface {
	// Name 返回组件名称（必须唯一）
	Name() string

	// OnStart 在组件启动前调用，用于初始化资源
	//
	// 参数：
	//   ctx: 用于控制 OnStart 操作本身的超时（不是运行时 context）
	//
	// 返回：
	//   error: 初始化失败时返回错误，Manager 会自动回滚
	//
	// 要求：
	//   - 必须快速返回（不要启动长期运行的 goroutine）
	//   - 分配内存、打开文件、建立连接等
	//   - 不要使用 ctx 来管理长期运行的逻辑（使用 OnRun 的 ctx）
	OnStart(ctx context.Context) error

	// OnRun 在组件启动后调用，用于运行时逻辑
	//
	// 参数：
	//   ctx: 运行时 context（✅ 这是 v2 的关键改进）
	//
	// 说明：
	//   - 此方法在独立 goroutine 中执行
	//   - ctx 在 Manager.Stop() 时被取消
	//   - 应该通过 <-ctx.Done() 监听停止信号
	//   - 返回时表示组件运行结束
	//
	// 示例：
	//   func (c *MyComponent) OnRun(ctx context.Context) error {
	//       for {
	//           select {
	//           case <-ctx.Done():
	//               return nil  // 收到停止信号
	//           case event := <-c.events:
	//               c.handle(event)
	//           }
	//       }
	//   }
	OnRun(ctx context.Context) error

	// OnStop 在组件停止时调用，用于清理资源
	//
	// 参数：
	//   ctx: 用于控制 OnStop 操作的超时
	//
	// 返回：
	//   error: 清理失败时返回错误
	//
	// 要求：
	//   - 必须是幂等的（可以多次调用）
	//   - 关闭连接、释放资源等
	//   - 返回前必须确保所有 goroutine 已退出
	OnStop(ctx context.Context) error
}

// State 表示生命周期状态
type State int

const (
	// StateCreated 组件已创建但未启动
	StateCreated State = iota
	// StateStarting 组件正在启动
	StateStarting
	// StateRunning 组件正在运行
	StateRunning
	// StateStopping 组件正在停止
	StateStopping
	// StateStopped 组件已停止
	StateStopped
)

// String 返回状态的字符串表示
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
	default:
		return "unknown"
	}
}

// Manager 管理多个组件的生命周期
//
// Manager 提供统一的组件生命周期管理，包括：
//   - 按顺序启动所有组件
//   - 统一管理运行时 context
//   - 逆序停止所有组件
//   - 启动失败时自动回滚
//
// # 使用示例
//
//	manager := v2.NewManager()
//	manager.Register(comp1)
//	manager.Register(comp2)
//
//	// 启动
//	if err := manager.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	// 停止
//	if err := manager.Stop(ctx); err != nil {
//	    log.Error(err)
//	}
type Manager struct {
	mu         sync.RWMutex
	components []Component
	state      State
	startTime  time.Time
	stopTime   time.Time

	// 运行时 context 管理
	runCtx    context.Context
	runCancel context.CancelFunc
	runWg     sync.WaitGroup // 等待所有 OnRun 完成
}

// NewManager 创建新的生命周期管理器
func NewManager() *Manager {
	return &Manager{
		state: StateCreated,
	}
}

// Register 注册组件
//
// 组件将按照注册顺序启动，按照逆序停止。
//
// 注意：此方法只能在 Manager 启动之前调用（StateCreated 或 StateStopped 状态）。
// 在 StateRunning 状态下调用时，组件会被加入列表，但 OnRun 不会被执行，
// 仅会记录警告日志。如需动态添加组件，应在下次 Start 前注册。
func (m *Manager) Register(comp Component) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateRunning {
		logger.WithFields(logger.Fields{
			"component": comp.Name(),
		}).Warn("[Lifecycle] Register called while running: component OnRun will NOT be started until next Start()")
	}

	m.components = append(m.components, comp)
}

// Start 启动所有组件
//
// 启动过程：
//  1. 调用所有组件的 OnStart（按注册顺序）
//  2. 创建运行时 context
//  3. 在独立 goroutine 中调用所有组件的 OnRun
//
// 如果任何组件的 OnStart 失败，会自动回滚已启动的组件。
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()

	// 检查状态
	if m.state != StateCreated && m.state != StateStopped {
		m.mu.Unlock()
		return &ErrInvalidState{Current: m.state, Expected: StateCreated}
	}

	m.state = StateStarting
	m.startTime = time.Now()
	components := m.components
	m.mu.Unlock()

	logger.WithFields(logger.Fields{
		"component_count": len(components),
	}).Info("[Lifecycle] Starting components")

	// Phase 1: 调用所有组件的 OnStart
	var startedComponents []Component
	for i, comp := range components {
		select {
		case <-ctx.Done():
			// 超时，回滚
			m.rollback(startedComponents)
			m.mu.Lock()
			m.state = StateCreated
			m.mu.Unlock()
			return fmt.Errorf("start timeout: %w", ctx.Err())
		default:
		}

		if err := comp.OnStart(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"index":     i,
				"error":     err,
			}).Error("[Lifecycle] Component OnStart failed")

			// 回滚已启动的组件
			m.rollback(startedComponents)
			m.mu.Lock()
			m.state = StateCreated
			m.mu.Unlock()
			return &StartError{
				Component: comp.Name(),
				Phase:     "OnStart",
				Err:       err,
			}
		}

		startedComponents = append(startedComponents, comp)
	}

	// Phase 2: 创建运行时 context 并启动所有组件的 OnRun
	m.mu.Lock()
	m.runCtx, m.runCancel = context.WithCancel(context.Background())
	runCtx := m.runCtx
	m.state = StateRunning
	m.mu.Unlock()

	// 在独立 goroutine 中运行每个组件的 OnRun
	for _, comp := range components {
		m.runWg.Add(1)
		go func(c Component) {
			defer m.runWg.Done()

			if err := c.OnRun(runCtx); err != nil {
				logger.WithFields(logger.Fields{
					"component": c.Name(),
					"error":     err,
				}).Error("[Lifecycle] Component OnRun failed")
			}
		}(comp)
	}

	logger.Info("[Lifecycle] All components started successfully")
	return nil
}

// rollback 回滚已启动的组件
func (m *Manager) rollback(startedComponents []Component) {
	if len(startedComponents) == 0 {
		return
	}

	logger.WithFields(logger.Fields{
		"count": len(startedComponents),
	}).Warn("[Lifecycle] Rolling back started components")

	// 逆序停止
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := len(startedComponents) - 1; i >= 0; i-- {
		comp := startedComponents[i]
		if err := comp.OnStop(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"error":     err,
			}).Error("[Lifecycle] Component rollback failed")
		}
	}

	logger.Info("[Lifecycle] Rollback completed successfully")
}

// Stop 停止所有组件
//
// 停止过程：
//  1. 取消运行时 context（触发所有 OnRun 退出）
//  2. 等待所有 OnRun 完成
//  3. 逆序调用所有组件的 OnStop
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()

	if m.state != StateRunning {
		m.mu.Unlock()
		return nil
	}

	m.state = StateStopping
	m.stopTime = time.Now()

	// 取消运行时 context
	if m.runCancel != nil {
		m.runCancel()
	}

	components := m.components
	m.mu.Unlock()

	logger.WithFields(logger.Fields{
		"component_count": len(components),
	}).Info("[Lifecycle] Stopping components")

	// 等待所有 OnRun 完成
	done := make(chan struct{})
	go func() {
		m.runWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有 OnRun 已完成
	case <-ctx.Done():
		// 等待 OnRun 超时，为 OnStop 创建新的独立 context
		// 原 ctx 已过期，继续使用会导致 OnStop 立即返回错误
		logger.Warn("[Lifecycle] Stop timeout waiting for OnRun, proceeding with OnStop using fresh context")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		ctx = stopCtx
	}

	// 逆序调用 OnStop，收集所有错误
	var stopErrors []error
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]
		if err := comp.OnStop(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"error":     err,
			}).Error("[Lifecycle] Component OnStop failed")
			stopErrors = append(stopErrors, fmt.Errorf("component %s: %w", comp.Name(), err))
		}
	}

	m.mu.Lock()
	m.state = StateStopped
	m.mu.Unlock()

	// 如果有多个错误，返回组合错误
	if len(stopErrors) > 0 {
		if len(stopErrors) == 1 {
			return &StopError{Err: stopErrors[0]}
		}
		// 多个错误，返回组合错误
		return &StopError{
			Err: fmt.Errorf("multiple components failed to stop: %v", stopErrors),
		}
	}

	logger.Info("[Lifecycle] All components stopped successfully")
	return nil
}

// State 返回当前状态
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Uptime 返回运行时间
func (m *Manager) Uptime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.state != StateRunning {
		if !m.stopTime.IsZero() && !m.startTime.IsZero() {
			return m.stopTime.Sub(m.startTime)
		}
		return 0
	}

	return time.Since(m.startTime)
}

// ComponentCount 返回注册的组件数量
func (m *Manager) ComponentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.components)
}

// RunContext 返回运行时 context
//
// 返回值：
//   - context.Context: 运行时 context
//   - bool: 如果组件正在运行返回 true，否则返回 false
//
// 使用场景：
//   - 在组件外部监听全局停止信号
//   - 创建派生 context
func (m *Manager) RunContext() (context.Context, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.state == StateRunning && m.runCtx != nil {
		return m.runCtx, true
	}

	return nil, false
}

// StartError 表示启动错误
type StartError struct {
	Component string
	Phase     string
	Err       error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("Lifecycle: component '%s' %s failed: %v", e.Component, e.Phase, e.Err)
}

func (e *StartError) Unwrap() error {
	return e.Err
}

// StopError 表示停止错误
type StopError struct {
	Err error
}

func (e *StopError) Error() string {
	return fmt.Sprintf("Lifecycle: stop failed: %v", e.Err)
}

func (e *StopError) Unwrap() error {
	return e.Err
}

// ErrInvalidState 表示无效的状态转换
type ErrInvalidState struct {
	Current  State
	Expected State
}

func (e ErrInvalidState) Error() string {
	return fmt.Sprintf("Lifecycle: invalid state: current=%s, expected=%s", e.Current, e.Expected)
}
