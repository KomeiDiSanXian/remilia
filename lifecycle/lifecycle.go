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
	"errors"
	"fmt"
	"slices"
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

// ComponentStatus 记录单个组件的运行时状态
type ComponentStatus struct {
	Name    string
	Running bool
	ExitErr error     // OnRun 返回的错误，nil 表示正常退出或仍在运行
	ExitAt  time.Time // OnRun 退出时间，零值表示仍在运行
}

// Manager 管理多个组件的生命周期
//
// Manager 提供统一的组件生命周期管理，包括：
//   - 按注册顺序启动所有组件
//   - 统一管理运行时 context
//   - 逆序停止所有组件
//   - 启动失败时自动回滚（回滚错误会聚合进返回值）
//
// # 组件启动顺序
//
// 组件按注册顺序串行启动，逆序停止。当前不支持声明式依赖（如"B 依赖 A"）
// 和自动拓扑排序，调用方应手动保证注册顺序满足依赖关系。
// 可使用 [Manager.RegisterOrdered] 在一处集中声明顺序，提高可读性。
//
// # 使用示例
//
//	manager := lifecycle.NewManager()
//	manager.RegisterOrdered(configComp, dbComp, serverComp)
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
	// startedComps 记录已成功经历 OnStart 的组件，Stop() 仅对这些组件调用 OnStop
	// 运行时调用 Register() 添加的组件不在此列表中（它们从未经历 OnStart）
	startedComps []Component
	state        State
	startTime    time.Time
	stopTime     time.Time

	// 父级 context（最外层，Bot 的 root context）
	parentCtx    context.Context
	parentCancel context.CancelFunc

	// 运行时 context 管理
	runCtx    context.Context
	runCancel context.CancelFunc
	runWg     sync.WaitGroup // 等待所有 OnRun 完成

	// 组件运行时状态追踪
	compStatusMu sync.RWMutex
	compStatuses map[string]*ComponentStatus

	// 修复 #8：Stop 等待 OnRun 超时后为 OnStop 阶段分配的额外超时。
	// 默认 10s，可通过 WithStopTimeout() 选项自定义。
	stopTimeout time.Duration
}

// ManagerOption Manager 配置选项
type ManagerOption func(*Manager)

// WithStopTimeout 设置 Stop 等待 OnRun 超时后 OnStop 阶段的额外超时时间（默认 10s）
func WithStopTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.stopTimeout = d
		}
	}
}

// NewManager 创建新的生命周期管理器
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		state:        StateCreated,
		compStatuses: make(map[string]*ComponentStatus),
		stopTimeout:  10 * time.Second, // 默认 10s
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Register 注册组件，返回 true 表示组件将完整参与当前（或下次）生命周期。
//
// 组件将按照注册顺序启动，按照逆序停止。
//
// # 返回值语义
//
//   - true：Manager 处于 StateCreated 或 StateStopped，组件已入队，
//     下次 Start() 时将完整走完 OnStart → OnRun → OnStop。
//   - false：Manager 处于 StateRunning，组件已入队但**本次运行周期内
//     OnStart / OnRun / OnStop 均不会被调用**；下次 Start() 时才会生效。
//     同时会打印 Warn 日志提示。
//
// # 运行时注册的限制（重要）
//
// 此方法只应在 Manager 启动之前调用（StateCreated 或 StateStopped 状态）。
// 在 StateRunning 状态下调用时，组件会被加入 components 列表，
// 但存在以下限制：
//
//   - OnStart **不会**被调用（组件未经过启动阶段初始化）
//   - OnRun  **不会**被调用（不参与当前运行时的 goroutine 管理）
//   - OnStop **不会**被调用（Stop() 仅清理 startedComps，即已成功完成 OnStart 的组件）
//
// 因此，运行时注册的组件处于"仅登记"状态，不参与当前生命周期的任何阶段。
// 它们将在下次 Start() 时完整走完 OnStart → OnRun → OnStop 生命周期。
//
// 若组件的 OnStop 依赖 OnStart 完成的初始化（如释放在 OnStart 中申请的资源），
// 请务必避免运行时注册，以防 OnStop 在从未调用 OnStart 的情况下被意外触发。
//
// 示例：
//
//	if ok := manager.Register(myComp); !ok {
//	    // Manager 正在运行中，myComp 将在下次 Start() 时才生效
//	    log.Warn("component registered but will not run until next Start()")
//	}
func (m *Manager) Register(comp Component) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateRunning {
		logger.WithFields(logger.Fields{
			"component": comp.Name(),
		}).Warn("[Lifecycle] Register called while running: component OnStart/OnRun/OnStop will NOT be called in the current cycle; will take effect on next Start()")
		m.components = append(m.components, comp)
		return false
	}

	m.components = append(m.components, comp)
	return true
}

// RegisterOrdered 按依赖顺序批量注册多个组件，返回每个组件的注册结果切片。
//
// 组件将按传入切片的顺序依次追加到注册列表，等价于多次调用 Register。
// 返回值与传入组件一一对应：true 表示组件将参与当前（或下次）生命周期，
// false 表示 Manager 处于运行状态，该组件在本次运行周期内不生效。
//
// 这是在当前"手动保证注册顺序"语义下的辅助方法，使调用方可以在一处
// 显式声明组件的启动顺序，提高可读性：
//
//	results := manager.RegisterOrdered(
//	    configComp,   // 第一个启动（无依赖）
//	    databaseComp, // 依赖 config，第二个启动
//	    serverComp,   // 依赖 database，最后启动
//	)
//	// 若 Manager 已在运行中，results 中对应项为 false
//
// # 关于完整依赖声明支持
//
// 当前 Manager 不支持声明式依赖（如 "B 依赖 A"）并自动拓扑排序，
// 调用方应确保传入的组件切片已按依赖先后排好序。
// Plugin Manager 已实现拓扑排序，需要时可参考其实现将相同机制引入 lifecycle.Manager。
func (m *Manager) RegisterOrdered(comps ...Component) []bool {
	results := make([]bool, len(comps))
	for i, c := range comps {
		results[i] = m.Register(c)
	}
	return results
}

// Start 启动所有组件
//
// 启动过程：
//  1. 调用所有组件的 OnStart（按注册顺序）
//  2. 以传入 ctx 的父 context 派生运行时 context（runCtx）
//  3. 在独立 goroutine 中调用所有组件的 OnRun
//
// ctx 仅用于控制 OnStart 阶段的超时，不作为 OnRun 的运行时 context。
// OnRun 使用从 ctx.Value 链继承的父 context（剥离超时/截止时间），
// 由 Stop() 时调用 runCancel 来终止所有 OnRun goroutine。
//
// 若希望 OnRun goroutine 与 Bot 根 context 联动，
// 请在 Bot.Start() 中传入从 rootCtx 派生的 ctx。
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
	m.startedComps = nil // 每次 Start() 重置（支持热重启）
	m.mu.Unlock()

	logger.WithFields(logger.Fields{
		"component_count": len(components),
	}).Info("[Lifecycle] Starting components")

	// 调用所有组件的 OnStart
	var startedComponents []Component
	for i, comp := range components {
		select {
		case <-ctx.Done():
			// 启动阶段超时，回滚已启动的组件。
			// rollbackErr 不为 nil 时说明回滚本身也有组件失败，包装进返回错误让上层感知。
			rollbackErr := m.rollback(ctx, startedComponents)
			m.mu.Lock()
			m.state = StateCreated
			m.mu.Unlock()
			startErr := fmt.Errorf("start timeout: %w", ctx.Err())
			if rollbackErr != nil {
				return fmt.Errorf("%w; additionally rollback failed: %w", startErr, rollbackErr)
			}
			return startErr
		default:
		}

		if err := comp.OnStart(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"index":     i,
				"error":     err,
			}).Error("[Lifecycle] Component OnStart failed")

			// 回滚已启动的组件。
			// rollbackErr 不为 nil 时说明回滚本身也有组件失败，包装进返回错误让上层感知。
			rollbackErr := m.rollback(ctx, startedComponents)
			m.mu.Lock()
			m.state = StateCreated
			m.mu.Unlock()
			startErr := &StartError{
				Component: comp.Name(),
				Phase:     "OnStart",
				Err:       err,
			}
			if rollbackErr != nil {
				return fmt.Errorf("%w; additionally rollback failed: %w", startErr, rollbackErr)
			}
			return startErr
		}

		startedComponents = append(startedComponents, comp)
	}

	// 创建 parent / run 双层 context 并启动所有组件的 OnRun
	//
	// parentCtx 是最外层 context（Bot 的 root context），剥离 OnStart 阶段的超时/截止时间，
	// 但保留 ctx 的 Value 链（tracing、metadata 等）。
	// parentCtx 在 Stop() 中 OnStop 全部完成后才被取消，确保 OnStop 期间外部资源仍可用。
	//
	// runCtx 从 parentCtx 派生，由 Stop() 首先取消以通知所有 OnRun goroutine 退出。
	base := context.WithoutCancel(ctx)
	m.mu.Lock()
	m.parentCtx, m.parentCancel = context.WithCancel(base)
	m.runCtx, m.runCancel = context.WithCancel(m.parentCtx)
	runCtx := m.runCtx
	m.startedComps = startedComponents // 保存已成功 OnStart 的组件
	m.state = StateRunning
	m.mu.Unlock()

	// 初始化组件状态
	m.compStatusMu.Lock()
	m.compStatuses = make(map[string]*ComponentStatus, len(components))
	for _, comp := range components {
		m.compStatuses[comp.Name()] = &ComponentStatus{
			Name:    comp.Name(),
			Running: true,
		}
	}
	m.compStatusMu.Unlock()

	// 在独立 goroutine 中运行每个组件的 OnRun
	for _, comp := range components {
		m.runWg.Add(1)
		go func(c Component) {
			defer m.runWg.Done()

			err := c.OnRun(runCtx)

			// 更新组件退出状态
			m.compStatusMu.Lock()
			if st, ok := m.compStatuses[c.Name()]; ok {
				st.Running = false
				st.ExitErr = err
				st.ExitAt = time.Now()
			}
			m.compStatusMu.Unlock()

			if err != nil {
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

// rollback 回滚已启动的组件，逆序调用每个组件的 OnStop。
//
// 超时时间复用 m.stopTimeout（由 WithStopTimeout 配置，默认 10s），
// 与 Stop() 阶段的 OnStop 超时保持一致，不再单独硬编码。
//
// 返回值：若回滚期间有一个或多个组件 OnStop 失败，返回聚合错误（类似 Stop() 的收集逻辑）。
// 调用方应将此错误包含在原始启动错误中一并返回，方便上层感知"回滚是否干净"。
func (m *Manager) rollback(startCtx context.Context, startedComponents []Component) error {
	if len(startedComponents) == 0 {
		return nil
	}

	logger.WithFields(logger.Fields{
		"count": len(startedComponents),
	}).Warn("[Lifecycle] Rolling back started components")

	// 从调用方 ctx 派生（继承 Value 链），但剥离已过期的 deadline/cancel，
	// 给 rollback 一个独立超时（复用 m.stopTimeout，与 Stop() 阶段一致）。
	// 避免启动超时后 rollback 也立即失败。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(startCtx), m.stopTimeout)
	defer cancel()

	var rollbackErrs []error
	for _, comp := range slices.Backward(startedComponents) {
		if err := comp.OnStop(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"error":     err,
			}).Error("[Lifecycle] Component rollback failed")
			rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback component %s: %w", comp.Name(), err))
		}
	}

	if len(rollbackErrs) > 0 {
		logger.WithFields(logger.Fields{
			"failed_count": len(rollbackErrs),
		}).Warn("[Lifecycle] Rollback completed with errors")
		return errors.Join(rollbackErrs...)
	}

	logger.Info("[Lifecycle] Rollback completed successfully")
	return nil
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

	components := m.startedComps // 仅对经历过 OnStart 的组件调用 OnStop
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
		// 使用 context.WithoutCancel(ctx) 保留原始 ctx 的 Value 链（trace/metadata），
		// 只剥离已过期的取消信号，避免 trace span 断链。
		logger.Warn("[Lifecycle] Stop timeout waiting for OnRun, proceeding with OnStop using fresh context")
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), m.stopTimeout)
		defer stopCancel()
		ctx = stopCtx
	}

	// 逆序调用 OnStop，仅对经历过 OnStart 的组件调用
	var stopErrors []error
	for _, comp := range slices.Backward(components) {
		if err := comp.OnStop(ctx); err != nil {
			logger.WithFields(logger.Fields{
				"component": comp.Name(),
				"error":     err,
			}).Error("[Lifecycle] Component OnStop failed")
			stopErrors = append(stopErrors, fmt.Errorf("component %s: %w", comp.Name(), err))
		}
	}

	// OnStop 全部完成后，取消 parentCtx（最外层 context，原 Bot 的 rootCtx）
	if m.parentCancel != nil {
		m.parentCancel()
	}

	m.mu.Lock()
	m.state = StateStopped
	m.mu.Unlock()

	// 如果有多个错误，返回组合错误
	if len(stopErrors) > 0 {
		return &StopError{Err: errors.Join(stopErrors...)}
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

// ComponentStatuses 返回所有组件的运行时状态快照
//
// 返回 map[组件名]*ComponentStatus，包括：
//   - Running: 是否仍在运行
//   - ExitErr: OnRun 退出时的错误（nil 表示正常退出或仍在运行）
//   - ExitAt:  OnRun 退出时间（零值表示仍在运行）
//
// 可以用于健康检查或排查哪个组件意外退出。
func (m *Manager) ComponentStatuses() map[string]ComponentStatus {
	m.compStatusMu.RLock()
	defer m.compStatusMu.RUnlock()

	result := make(map[string]ComponentStatus, len(m.compStatuses))
	for name, st := range m.compStatuses {
		result[name] = *st // 返回值拷贝，避免外部修改
	}
	return result
}

// HasUnhealthyComponents 检查是否有组件意外退出（OnRun 返回 error）。
// 返回的字符串切片包含所有不健康组件的名称，可通过 len() 判断是否存在不健康组件。
func (m *Manager) HasUnhealthyComponents() ([]string, bool) {
	m.compStatusMu.RLock()
	defer m.compStatusMu.RUnlock()

	var unhealthy []string
	for _, st := range m.compStatuses {
		if !st.Running && st.ExitErr != nil {
			unhealthy = append(unhealthy, st.Name)
		}
	}
	return unhealthy, len(unhealthy) > 0
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

// ParentContext 返回父级 context（Bot 的根 context）。
//
// 与 RunContext 的区别：
//   - ParentContext 是最外层 context，贯穿 Bot 整个生命周期，在 Stop() 的 OnStop 阶段后取消。
//     OnStop 执行期间 parentCtx 仍然有效，供插件安全访问平台 API。
//   - RunContext 是组件运行时 context，在 Stop() 开始时取消（早于 OnStop）。
//
// Bot 层应使用此方法而非 RunContext，确保外部组件（AdaptiveRateLimiter 等）
// 的生命周期与 Bot 的最外层 context 绑定。
//
// Start() 之前调用此方法返回 context.Background()。
func (m *Manager) ParentContext() context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.parentCtx != nil {
		return m.parentCtx
	}
	return context.Background()
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
