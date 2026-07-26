package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// LifecycleListener 插件生命周期监听器接口
type LifecycleListener interface {
	OnPluginLoaded(name string)
	OnPluginUnloaded(name string)
	OnPluginReloaded(name string)
	OnPluginError(name string, operation string, err error)
}

// DrainingInfo 蓝绿重载中旧实例的异步清理状态。
type DrainingInfo struct {
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	Done      bool      `json:"done"`
	Err       string    `json:"error,omitempty"`
}

// drainingEntry 内部 draining 实例跟踪记录。
type drainingEntry struct {
	inst      *Instance
	startedAt time.Time
	done      bool
	err       error
}

// Manager 插件管理器
//
// 核心职责：插件注册/注销、查询、容器/EventBus 访问。
// 生命周期操作通过 Lifecycle() 获取，配置通过 Config() 获取，统计通过 Stats() 获取。
type Manager struct {
	plugins     map[string]*Instance
	coordinator engine.PluginCoordinator
	loadOrder   []string
	container   *Container
	eventBus    EventBus
	mu          sync.RWMutex
	createdAt   time.Time // Manager 创建时间（Stats().Uptime 使用）

	lifecycle *lifecycleController
	config    *configController
	stats     *statsController
}

// NewManager 创建插件管理器
//
// opts 为可选配置，例如通过 WithConfigProvider 注入配置源：
//
//	pm := plugin.NewManager(eng,
//	    plugin.WithConfigProvider(plugin.NewViperConfigProvider(v)),
//	)
func NewManager(coordinator engine.PluginCoordinator, opts ...ManagerOption) *Manager {
	m := &Manager{
		plugins:     make(map[string]*Instance),
		coordinator: coordinator,
		loadOrder:   make([]string, 0),
		container:   NewContainer(),
		eventBus:    NewEventBus(),
		createdAt:   time.Now(),
	}
	m.lifecycle = newLifecycleController(m)
	m.config = newConfigController(m)
	m.stats = newStatsController(m)
	for _, opt := range opts {
		opt(m)
	}
	if m.config.configProvider != nil {
		m.config.configProvider.OnConfigChange(m.config.propagateConfigChange)
	}
	return m
}

// NewManagerWithEventBusOptions 使用自定义 EventBus 选项创建插件管理器。
func NewManagerWithEventBusOptions(coordinator engine.PluginCoordinator, ebOpts EventBusOptions, opts ...ManagerOption) *Manager {
	pm := NewManager(coordinator, opts...)
	pm.eventBus = NewEventBusWithOptions(ebOpts)
	return pm
}

// --- 快捷委托方法（保持外部 API 不变）---

func (pm *Manager) Coordinator() engine.PluginCoordinator { return pm.coordinator }
func (pm *Manager) Disable(name string) error             { return pm.lifecycle.Disable(name) }
func (pm *Manager) Enable(name string) error              { return pm.lifecycle.Enable(name) }
func (pm *Manager) IsDisabled(name string) bool           { return pm.lifecycle.IsDisabled(name) }
func (pm *Manager) Reload(ctx context.Context, name string) error {
	return pm.lifecycle.Reload(ctx, name)
}
func (pm *Manager) Retry(name string, desc *Descriptor) error { return pm.lifecycle.Retry(name, desc) }
func (pm *Manager) StartAll(ctx context.Context) error        { return pm.lifecycle.StartAll(ctx) }
func (pm *Manager) StopAll(ctx context.Context) error         { return pm.lifecycle.StopAll(ctx) }
func (pm *Manager) Shutdown()                                 { pm.lifecycle.Shutdown() }
func (pm *Manager) AddListener(l LifecycleListener)           { pm.lifecycle.AddListener(l) }
func (pm *Manager) RemoveListener(l LifecycleListener)        { pm.lifecycle.RemoveListener(l) }
func (pm *Manager) SetStrictDeps(enabled bool)                { pm.config.SetStrictDeps(enabled) }
func (pm *Manager) IsStrictDeps() bool                        { return pm.config.IsStrictDeps() }
func (pm *Manager) SetConfigProvider(cp ConfigProvider)       { pm.config.SetProvider(cp) }
func (pm *Manager) Stats() ManagerStats                       { return pm.stats.Snapshot() }
func (pm *Manager) ListDrainingInstances() map[string]*DrainingInfo {
	return pm.stats.ListDraining()
}
func (pm *Manager) GoroutineSummary() GoroutineSummary { return pm.stats.GoroutineSummary() }
func (pm *Manager) ListAllGoroutines() []GoroutineInfo { return pm.stats.ListGoroutines() }

// --- 核心方法（Manager 自有）---

// Unregister 注销插件，返回错误信息。
// ctx 用于控制超时：若 context 在 Teardown 完成前到期，返回 ctx.Err()。
//
// 并发安全说明：unload（含 Scope 清理、goroutine 等待、Teardown 用户代码）在
// Manager 锁外执行。旧实现全程持有写锁，一旦 Teardown 或插件后台 goroutine
// 调用任何 Manager API（如 ctx.Info.IsLoaded），整个 Manager 会永久死锁。
// 通过先将状态置为 Unloading 防止并发的重复注销。
func (pm *Manager) Unregister(ctx context.Context, name string) error {
	pm.mu.Lock()
	inst, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[PluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}
	if inst.GetState() == Unloading {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %s is already being unregistered", name)
	}
	inst.SetState(Unloading)
	pm.mu.Unlock()

	// 锁外执行 unload：Teardown/Scope 钩子/后台 goroutine 可安全访问 Manager API
	if err := inst.unload(ctx, pm.coordinator); err != nil {
		inst.SetState(Error)
		pm.notifyError(name, "unload", err)
		return err
	}

	pm.mu.Lock()
	delete(pm.plugins, name)
	pm.mu.Unlock()

	// 容器清理同样在锁外：Container 自身线程安全，且 Remove 会同步触发
	// Watch 回调（插件代码），不能在持有 Manager 锁时调用。
	pm.removeExportedKeys(inst, name)

	logger.Infof("[PluginManager] Plugin %s unregistered", name)
	pm.notifyUnloaded(name)
	return nil
}

// removeExportedKeys 从容器移除插件的主 key 及其通过 ExportAs/ExportIface
// 额外导出的所有 key，避免注销后容器残留悬挂引用。
// 必须在不持有 pm.mu 时调用（Remove 会同步触发 Watch 回调）。
func (pm *Manager) removeExportedKeys(inst *Instance, name string) {
	pm.container.Remove(name)
	if inst == nil {
		return
	}
	for _, k := range inst.exportedKeys() {
		if k != name {
			pm.container.Remove(k)
		}
	}
}

// registerInstanceInContainer 将实例注册进容器（若插件 API 未占用该 key）。
// 必须在不持有 pm.mu 时调用（Register 会同步触发 Watch 回调）。
func (pm *Manager) registerInstanceInContainer(name string) {
	pm.mu.RLock()
	inst, ok := pm.plugins[name]
	c := pm.container
	pm.mu.RUnlock()
	if !ok || c == nil {
		return
	}
	if !c.Has(name) {
		c.Register(name, inst)
	}
}

// ForceUnregister 强制注销插件，忽略 Unload 错误直接从管理器中移除。
//
// 适用场景：
//   - 插件处于 Error 状态，Unload 已无法正常执行
//   - 需要强制清理损坏的插件
//
// 注意：强制注销不会调用插件的资源清理逻辑，可能造成资源泄漏。
func (pm *Manager) ForceUnregister(name string) error {
	pm.mu.Lock()
	inst, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[PluginManager] ForceUnregister: plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	// 先释放锁再清理 engine 端资源，避免在持有 Manager 锁的情况下操作 engine
	pm.mu.Unlock()

	if pm.coordinator != nil {
		pm.coordinator.RemoveGroup(name)
	}

	pm.mu.Lock()
	delete(pm.plugins, name)
	pm.mu.Unlock()

	// 容器清理在锁外（Remove 会同步触发 Watch 回调）
	pm.removeExportedKeys(inst, name)

	logger.Warnf("[PluginManager] Plugin %s force unregistered (Unload skipped, engine group/cleanup done)", name)
	pm.notifyUnloaded(name)
	return nil
}

// UnregisterCascade 级联卸载指定插件及所有直接/间接依赖它的插件。
//
// 算法：
//  1. 构建反向依赖图（谁依赖了 name）
//  2. DFS 拓扑排序，得到从"最外层依赖方"到"目标插件"的卸载顺序
//  3. 按顺序逐一 Unregister
//
// 返回值：若目标插件不存在，返回 ErrPluginNotFound；
// 若任意插件卸载失败，停止卸载并返回错误（已卸载的插件不会回滚）。
func (pm *Manager) UnregisterCascade(ctx context.Context, name string) error {
	pm.mu.RLock()
	if _, exists := pm.plugins[name]; !exists {
		pm.mu.RUnlock()
		return errutil.ErrPluginNotFound
	}

	// 构建反向依赖图（dependents[A] = 所有声明依赖了 A 的插件名称集合）
	// 依赖声明可能带版本约束（"storage@>=1.0"），统一解析出插件名。
	dependents := make(map[string][]string)
	for pName, inst := range pm.plugins {
		for _, dep := range inst.desc.Deps {
			dependents[parseDepSpec(dep).name] = append(dependents[parseDepSpec(dep).name], pName)
		}
	}
	pm.mu.RUnlock()

	// DFS 收集所有需要卸载的插件（包括 name 本身）
	visited := make(map[string]bool)
	var order []string
	var dfs func(n string)
	dfs = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		for _, dep := range dependents[n] {
			dfs(dep)
		}
		order = append(order, n)
	}
	dfs(name)

	logger.Infof("[PluginManager] UnregisterCascade: will unregister %d plugin(s) in order: %v", len(order), order)

	for _, n := range order {
		if err := pm.Unregister(ctx, n); err != nil {
			logger.WithError(err).Errorf("[PluginManager] UnregisterCascade: failed to unregister plugin %s", n)
			return fmt.Errorf("cascade unregister %s: %w", n, err)
		}
	}
	return nil
}

// Get 获取插件实例。
// 若插件处于 Loading 状态（正在初始化），返回 nil, false，调用方需等待。
func (pm *Manager) Get(name string) (*Instance, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	inst, exists := pm.plugins[name]
	if !exists {
		return nil, false
	}
	if inst.GetState() == Loading {
		logger.Warnf("[PluginManager] Plugin %s is currently loading, please wait", name)
		return nil, false
	}
	return inst, true
}

// List 列出所有插件名称
func (pm *Manager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

// GetMetadata 获取插件的元数据
func (pm *Manager) GetMetadata(name string) (*Metadata, bool) {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		return nil, false
	}
	return inst.Metadata(), true
}

// ListWithMetadata 列出所有插件及其元数据
func (pm *Manager) ListWithMetadata() map[string]*Metadata {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*Metadata, len(pm.plugins))
	for name, inst := range pm.plugins {
		result[name] = inst.Metadata()
	}
	return result
}

// GetStatus 获取插件状态
func (pm *Manager) GetStatus(name string) (*Status, error) {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		return nil, errutil.ErrPluginNotFound
	}

	status := &Status{
		Name:         name,
		State:        inst.GetState(),
		LoadTime:     inst.GetLoadTime(),
		LastError:    inst.GetLastError(),
		Uptime:       inst.GetUptime(),
		MatcherCount: len(inst.GetMatchers()),
		Metadata:     inst.Metadata(),
		HasSaveState: inst.descriptor().effectiveAdvanced().SaveState != nil,
	}

	pm.mu.RLock()
	bus := pm.eventBus
	pm.mu.RUnlock()
	if bus != nil {
		stats := bus.GetStats()
		status.EventBusSubscriptions = stats.SubscriptionCount
	}

	inst.mu.RLock()
	gm := inst.goroutineMgr
	inst.mu.RUnlock()
	if gm != nil {
		status.GoroutineCount = len(gm.listGoroutines())
	}

	return status, nil
}

// ListStatus 列出所有插件的状态
func (pm *Manager) ListStatus() map[string]*Status {
	pm.mu.RLock()
	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	pm.mu.RUnlock()

	result := make(map[string]*Status, len(names))
	for _, name := range names {
		if status, err := pm.GetStatus(name); err == nil {
			result[name] = status
		}
	}
	return result
}

// IsLoaded 检查插件是否已加载（Loaded 状态）
func (pm *Manager) IsLoaded(name string) bool {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()
	if !exists {
		return false
	}
	return inst.GetState() == Loaded
}

// GetLoadOrder 获取插件加载顺序
func (pm *Manager) GetLoadOrder() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	order := make([]string, len(pm.loadOrder))
	copy(order, pm.loadOrder)
	return order
}

// Count 返回已注册插件的数量
func (pm *Manager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.plugins)
}

// GetContainer 获取依赖注入容器
func (pm *Manager) GetContainer() *Container {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.container
}

// FreezeContainer 冻结依赖注入容器，切换为无锁只读模式。
//
// 在所有插件通过 Register 加载完成后调用此方法，
// 后续 Get/Has 操作将使用原子指针快照，读性能提升 2-3x。
func (pm *Manager) FreezeContainer() {
	pm.mu.RLock()
	c := pm.container
	pm.mu.RUnlock()
	if c != nil {
		c.Freeze()
	}
	logger.Info("[PluginManager] Container frozen, Get/Has now use lock-free read")
}

// GetEventBus 获取插件间事件总线
func (pm *Manager) GetEventBus() EventBus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.eventBus
}

// --- 注册方法 ---

// Register 注册单个插件。
//
//	pm.Register(&Descriptor{Name: "help", Setup: fn})
//
// 可使用 RegisterOption 控制行为（仅批量注册生效）：
//
//	pm.RegisterBatch(ctx, descs, WithAtomic())
//	pm.RegisterBatch(ctx, descs, WithInferDeps())
func (pm *Manager) Register(desc *Descriptor) error {
	return pm.registerWithOptions(context.Background(), []*Descriptor{desc}, registerOptions{})
}

// RegisterBatch 批量注册多个插件，自动处理依赖顺序。
//
// ctx 用于控制 Setup 的超时。
// opts 可选，支持 WithAtomic（失败回滚）和 WithInferDeps（DryRun 推断依赖）。
//
//	pm.RegisterBatch(ctx, []*plugin.Descriptor{help.New(), storage.New()})
//	pm.RegisterBatch(ctx, descs, plugin.WithAtomic(), plugin.WithInferDeps())
func (pm *Manager) RegisterBatch(ctx context.Context, descs []*Descriptor, opts ...RegisterOption) error {
	o := registerOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return pm.registerWithOptions(ctx, descs, o)
}

// ValidateDependencies 验证一组插件的依赖关系（不注册）。
func (pm *Manager) ValidateDependencies(descriptors []*Descriptor) error {
	_, err := pm.TopologicalSort(descriptors)
	return err
}

// registerOptions 控制 Register 的行为。
type registerOptions struct {
	atomic    bool // 失败时自动回滚
	inferDeps bool // 使用 DryRun 推断依赖
}

// RegisterOption 注册选项函数。
type RegisterOption func(*registerOptions)

// WithAtomic 注册选项：失败时自动逆序回滚已注册的所有插件。
func WithAtomic() RegisterOption {
	return func(o *registerOptions) { o.atomic = true }
}

// WithInferDeps 注册选项：使用 DryRun 自动推断依赖关系。
func WithInferDeps() RegisterOption {
	return func(o *registerOptions) { o.inferDeps = true }
}

// registerWithOptions 内部统一注册入口。
// ctx 传递到 registerSingle 用于 Setup 超时控制。
func (pm *Manager) registerWithOptions(ctx context.Context, descriptors []*Descriptor, opts registerOptions) error {
	if len(descriptors) == 0 {
		return nil
	}

	// 单个插件不走拓扑排序（checkDependencies 使用宽松模式验证，不强制 Deps 必须全部就绪）
	if len(descriptors) == 1 && !opts.inferDeps {
		desc := descriptors[0]
		if desc == nil {
			return fmt.Errorf("descriptor at index 0 is nil")
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index 0 has empty name")
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
		descMap := map[string]*Descriptor{desc.Name: desc}
		if err := pm.checkCrossBatchCyclicDependency(descriptors, descMap); err != nil {
			return err
		}
		return pm.registerSingle(ctx, desc)
	}

	for i, desc := range descriptors {
		if desc == nil {
			return fmt.Errorf("descriptor at index %d is nil", i)
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index %d has empty name", i)
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
	}

	// 推断依赖（DryRun）
	if opts.inferDeps {
		logger.Info("[PluginManager] Smart registration: inferring dependencies...")
		inferred, err := pm.dryRunInferDeps(descriptors)
		if err != nil {
			return err
		}
		descriptors = mergeInferredDeps(descriptors, inferred)
	}

	// 拓扑排序
	sorted, err := pm.TopologicalSort(descriptors)
	if err != nil {
		if opts.inferDeps {
			return err // 错误信息已包含循环依赖提示
		}
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Phase 1: 预注册（Lock #1 + Config I/O + Lock #2 + Setup）
	// Lock #3 延期到 Phase 2 批量处理，减少锁争用
	type regResult struct {
		desc            *Descriptor
		instance        *Instance
		trackedDeps     []string
		trackedOptional []string
	}
	results := make([]regResult, 0, len(sorted))
	var batchErr error
	for i, desc := range sorted {
		instance, tdeps, topts, loadErr := pm.registerPreSetup(ctx, desc)
		if loadErr != nil {
			// 清理失败插件的 Lock #2 残留（已存入 plugins 但无法继续）。
			// 仅当 instance != nil（确实完成过插入）才清理——
			// 例如 ErrPluginAlreadyExists 时 instance 为 nil，此时若无条件
			// delete/RemoveGroup 会误删已注册的同名插件（历史 bug）。
			// 引擎与容器清理在锁外进行（Watch 回调不能在持 Manager 锁时触发）。
			if instance != nil {
				pm.mu.Lock()
				delete(pm.plugins, desc.Name)
				pm.mu.Unlock()
				if pm.coordinator != nil {
					pm.coordinator.RemoveGroup(desc.Name)
				}
				pm.removeExportedKeys(instance, desc.Name)
			}

			if opts.atomic {
				// 回滚之前已注册的
				for j := range i {
					if rollbackErr := pm.Unregister(context.Background(), sorted[j].Name); rollbackErr != nil {
						logger.WithError(rollbackErr).Warnf("[PluginManager] Rollback failed for plugin %s", sorted[j].Name)
					}
				}
				pm.mu.RLock()
				existingNames := make([]string, 0, len(pm.plugins))
				for n := range pm.plugins {
					existingNames = append(existingNames, n)
				}
				pm.mu.RUnlock()
				return &PluginError{
					PluginName:        desc.Name,
					Operation:         "register",
					Cause:             loadErr,
					RegisteredPlugins: existingNames,
					Hint:              "all previously registered plugins in this batch have been rolled back",
				}
			}
			// 非原子模式：停止注册后续插件，但仍要完成已成功插件的 finalize
			//（loadOrder/容器/通知），避免它们停留在"半注册"状态
			//（在 plugins 表里、matcher 已挂 engine，却不受 StopAll 管理）。
			batchErr = fmt.Errorf("failed to register plugin %s: %w", desc.Name, loadErr)
			break
		}
		results = append(results, regResult{desc: desc, instance: instance, trackedDeps: tdeps, trackedOptional: topts})
	}

	// Phase 2: 批量 Lock #3 — 依赖合并 + loadOrder
	pm.mu.Lock()
	var succeeded []string
	var strictFailed []regResult
	for _, r := range results {
		if pm.finalizeRegistration(r.desc, r.trackedDeps, r.trackedOptional) {
			// 严格模式违规：不进 loadOrder，锁外统一回滚（与单插件路径行为一致）
			strictFailed = append(strictFailed, r)
			continue
		}
		succeeded = append(succeeded, r.desc.Name)
	}
	pm.mu.Unlock()

	// 容器注册与通知在锁外进行（Register 触发 Watch 回调，notifyLoaded 获取 RLock）
	for _, name := range succeeded {
		pm.registerInstanceInContainer(name)
	}
	for _, name := range succeeded {
		pm.notifyLoaded(name)
	}

	// 严格模式违规插件：锁外 unload（内部会 RemoveGroup）并移除
	var strictErr error
	for _, r := range strictFailed {
		name := r.desc.Name
		logger.Warnf("[PluginManager] Plugin %s uses undeclared dependencies, rolling back (strict mode)", name)
		if r.instance != nil {
			if err := r.instance.unload(context.Background(), pm.coordinator); err != nil {
				logger.WithError(err).Warnf("[PluginManager] Failed to teardown plugin %s during strict-mode rollback", name)
			}
		}
		pm.mu.Lock()
		delete(pm.plugins, name)
		pm.mu.Unlock()
		pm.removeExportedKeys(r.instance, name)
		strictErr = fmt.Errorf("plugin %q uses undeclared dependencies; "+
			"add them to Deps or disable strict mode via manager.SetStrictDeps(false)", name)
	}

	// 修正 loadOrder
	registeredInsts := make([]*Instance, 0, len(succeeded))
	for _, name := range succeeded {
		if inst, ok := pm.Get(name); ok {
			registeredInsts = append(registeredInsts, inst)
		}
	}
	pm.rectifyLoadOrder(registeredInsts)

	if batchErr != nil {
		return batchErr
	}
	if strictErr != nil {
		return strictErr
	}
	logger.Infof("[PluginManager] Successfully registered %d plugins in dependency order", len(succeeded))
	return nil
}

// registerPreSetup 执行三段锁的前两段：Lock #1（验证）+ Lock #2（存储）+ Setup。
// 返回 instance、loadErr 和追踪的依赖列表，供最终化阶段处理。
// 调用方负责在 Setup 后调用 finalizeRegistration 或 registerSingle 的 Lock #3。
func (pm *Manager) registerPreSetup(ctx context.Context, desc *Descriptor) (instance *Instance, trackedDeps, trackedOptional []string, loadErr error) {
	if err := validateDescriptor(desc); err != nil {
		return nil, nil, nil, err
	}

	name := desc.Name

	// ========== Lock #1: 快速校验 ==========
	pm.mu.Lock()

	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		return nil, nil, nil, errutil.ErrPluginAlreadyExists
	}

	registeredList := func() []string {
		names := make([]string, 0, len(pm.plugins))
		for n := range pm.plugins {
			names = append(names, n)
		}
		return names
	}

	if err := checkDependencies(pm, desc, registeredList); err != nil {
		pm.mu.Unlock()
		return nil, nil, nil, err
	}

	if err := validateVersionConstraints(pm, desc); err != nil {
		pm.mu.Unlock()
		return nil, nil, nil, err
	}

	backfill := pm.collectContainerBackfillLocked()

	cp := pm.config.configProvider

	if desc.Advanced != nil && desc.Advanced.Reload != nil && desc.Advanced.Strategy != ReloadInPlace {
		pm.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("plugin %q: Advanced.Reload is set but Strategy is %v (not ReloadInPlace). "+
			"The Reload func will NOT be called with this strategy. Did you mean Strategy: plugin.ReloadInPlace", name, desc.Advanced.Strategy)
	}

	pm.mu.Unlock()
	// ========== Lock #1 结束 ==========

	// 容器回填在锁外执行：Container.Register 会同步触发 Watch 回调（插件代码），
	// 不能在持有 Manager 锁时调用。
	pm.applyContainerBackfill(backfill)

	// ========== 无锁区：Config 构造（可能 I/O）+ Schema 校验 ==========
	var config Config
	if cp != nil {
		config = NewPluginConfigFromProvider(name, cp)
	}

	if err := validateConfigSchema(name, desc, config); err != nil {
		return nil, nil, nil, err
	}

	// ========== 无锁区：实例 + SetupContext 构建 ==========
	instance = &Instance{
		desc:     desc,
		state:    Unloaded,
		matchers: make([]*engine.Matcher, 0),
		manager:  pm,
	}

	var adminView ManagerWriter
	if desc.Privileged {
		adminView = newManagerWriter(pm)
	}

	setupCtx := &SetupContext{
		Reg:      newLiveRegistryWriter(pm.coordinator, name, instance),
		Log:      newPluginLogger(name),
		Info:     newPluginInfo(pm),
		Admin:    adminView,
		Config:   config,
		EventBus: pm.eventBus,
		setupContextInternal: setupContextInternal{
			container:        pm.container,
			pluginName:       name,
			instance:         instance,
			autoTrackEnabled: true,
			eng:              pm.coordinator,
		},
	}

	instance.setupContext = setupCtx
	instance.state = Loading

	// ========== Lock #2: 存入 plugins 表（短）==========
	pm.mu.Lock()
	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		return nil, nil, nil, errutil.ErrPluginAlreadyExists
	}
	pm.plugins[name] = instance
	pm.mu.Unlock()
	// ========== Lock #2 结束 ==========

	// ========== 无锁区：执行 Setup ==========
	loadErr = instance.load(ctx)

	return instance, setupCtx.getTrackedDependencies(), setupCtx.getTrackedOptionalDependencies(), loadErr
}

// registerPostSetupSingle Lock #3：完成依赖合并、loadOrder 更新，
// 容器注册和通知在锁外执行（Register 会同步触发 Watch 回调）。
// 适用于单插件注册（非批量路径直接调用的 finalize）。
func (pm *Manager) registerPostSetupSingle(inst *Instance, desc *Descriptor, loadErr error, trackedDeps, trackedOptional []string) error {
	name := desc.Name

	if loadErr != nil {
		pm.mu.Lock()
		delete(pm.plugins, name)
		pm.mu.Unlock()
		if pm.coordinator != nil {
			pm.coordinator.RemoveGroup(name)
		}
		pm.removeExportedKeys(inst, name)
		logger.WithError(loadErr).Errorf("[PluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", loadErr)
		return loadErr
	}

	pm.mu.Lock()
	strictViolation := pm.finalizeRegistration(desc, trackedDeps, trackedOptional)
	pm.mu.Unlock()

	if strictViolation {
		// 严格模式：撤销注册（unload 在锁外执行，内部会 RemoveGroup）
		if inst != nil {
			if err := inst.unload(context.Background(), pm.coordinator); err != nil {
				logger.WithError(err).Warnf("[PluginManager] Failed to teardown plugin %s during strict-mode rollback", name)
			}
		}
		pm.mu.Lock()
		delete(pm.plugins, name)
		pm.mu.Unlock()
		pm.removeExportedKeys(inst, name)
		return fmt.Errorf("plugin %q uses undeclared dependencies; "+
			"add them to Deps or disable strict mode via manager.SetStrictDeps(false)", name)
	}

	pm.registerInstanceInContainer(name)
	pm.notifyLoaded(name)
	return nil
}

// registerSingle 注册单个插件（三段锁策略）。
func (pm *Manager) registerSingle(ctx context.Context, desc *Descriptor) error {
	instance, trackedDeps, trackedOptional, loadErr := pm.registerPreSetup(ctx, desc)
	if instance == nil && loadErr != nil {
		return loadErr
	}
	return pm.registerPostSetupSingle(instance, desc, loadErr, trackedDeps, trackedOptional)
}

// finalizeRegistration 在锁内执行 Lock #3 逻辑：依赖合并、loadOrder、容器注册。
// 返回 true 表示存在未声明的必需依赖（strictDeps 模式下应回滚）。
// 调用方须持有 pm.mu。
func (pm *Manager) finalizeRegistration(desc *Descriptor, trackedDeps, trackedOptional []string) (strictViolation bool) {
	name := desc.Name

	allTracked := make(map[string]bool, len(trackedDeps)+len(trackedOptional))
	for _, d := range trackedDeps {
		allTracked[d] = true
	}
	for _, d := range trackedOptional {
		allTracked[d] = true
	}

	if len(allTracked) > 0 {
		// 声明可能带版本约束（"storage@>=1.0"），追踪到的是纯插件名，
		// 比较前解析出名字，避免把已声明的依赖误判为未声明。
		declaredDeps := make(map[string]bool)
		for _, dep := range desc.Deps {
			declaredDeps[parseDepSpec(dep).name] = true
		}
		for _, dep := range desc.OptionalDeps {
			declaredDeps[parseDepSpec(dep).name] = true
		}

		undeclaredAll := make([]string, 0)
		for dep := range allTracked {
			if !declaredDeps[dep] {
				undeclaredAll = append(undeclaredAll, dep)
			}
		}

		if len(undeclaredAll) > 0 {
			if pm.config.strictDeps {
				return true // strictDeps 违反，由调用方决定回滚
			}
			logger.WithFields(logger.Fields{
				"plugin":          name,
				"undeclared_deps": undeclaredAll,
				"declared_deps":   desc.Deps,
			}).Warn("[PluginManager] Plugin uses dependencies not declared in Deps field")
		}

		var undeclaredRequired []string
		for _, d := range trackedDeps {
			if !declaredDeps[d] {
				undeclaredRequired = append(undeclaredRequired, d)
			}
		}
		if len(undeclaredRequired) > 0 {
			mergedDeps := make([]string, len(desc.Deps), len(desc.Deps)+len(undeclaredRequired))
			copy(mergedDeps, desc.Deps)
			mergedDeps = append(mergedDeps, undeclaredRequired...)
			newDesc := *desc
			newDesc.Deps = mergedDeps
			if inst, ok := pm.plugins[name]; ok {
				// COW 替换走 inst.mu，供无锁读取方（Metadata/GetStatus）安全读取
				inst.setDescriptor(&newDesc)
			}
		}
	}

	pm.loadOrder = append(pm.loadOrder, name)

	// 注意：容器注册不在此处进行——Container.Register 会同步触发 Watch 回调，
	// 而本方法在 pm.mu 写锁内执行。调用方在解锁后通过 registerInstanceInContainer 完成。

	logger.Infof("[PluginManager] Plugin %s registered", name)
	return false
}

// --- 生命周期通知委托 ---

func (pm *Manager) notifyLoaded(name string)               { pm.lifecycle.notifyLoaded(name) }
func (pm *Manager) notifyUnloaded(name string)             { pm.lifecycle.notifyUnloaded(name) }
func (pm *Manager) notifyError(name, op string, err error) { pm.lifecycle.notifyError(name, op, err) }

// --- 内部辅助方法 ---

// notifyDependents 通知依赖了 reloadedPlugin 的所有其他插件
func (pm *Manager) notifyDependents(reloadedPlugin string) {
	// 仅在锁下收集插件名，避免复制整个 map（maps.Copy 的 O(n) 开销）
	pm.mu.RLock()
	allNames := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		allNames = append(allNames, name)
	}
	pm.mu.RUnlock()

	for _, depName := range allNames {
		if depName == reloadedPlugin {
			continue
		}
		pm.mu.RLock()
		inst, exists := pm.plugins[depName]
		if !exists {
			pm.mu.RUnlock()
			continue
		}
		deps := inst.desc.Deps
		cb := inst.desc.getOnDependencyReloaded()
		pm.mu.RUnlock()
		// 依赖声明可能带版本约束（"storage@>=1.0"），按解析后的名字匹配
		matched := false
		for _, d := range deps {
			if parseDepSpec(d).name == reloadedPlugin {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if cb == nil {
			continue
		}
		logger.Infof("[PluginManager] Notifying plugin %s that dependency %s was reloaded", depName, reloadedPlugin)
		// 使用 metaGM 管理此类元数据 goroutine，Shutdown 时可感知并等待
		pm.lifecycle.metaGM.goNamed_(fmt.Sprintf("notify-%s->%s", reloadedPlugin, depName), func(ctx context.Context) {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("panic", r).Errorf("[PluginManager] Panic in OnDependencyReloaded for plugin dependency %s", reloadedPlugin)
				}
			}()
			cb(reloadedPlugin)
		})
	}
}

// ensureContainerInitialized 确保依赖注入容器已初始化并回填缺失条目。
// 不要求调用方持有 Manager 锁；容器注册在锁外执行。
// 内部注册路径使用 collectContainerBackfillLocked/applyContainerBackfill 两段式。
func (pm *Manager) ensureContainerInitialized() {
	pm.mu.Lock()
	backfill := pm.collectContainerBackfillLocked()
	pm.mu.Unlock()
	pm.applyContainerBackfill(backfill)
}

// containerBackfill 待回填进容器的条目（名称 → 值）。
type containerBackfill struct {
	name  string
	value any
}

// collectContainerBackfillLocked 收集需要回填进容器的条目（须在持有 Manager 锁时调用）。
// 实际注册由 applyContainerBackfill 在锁外执行——Container.Register 会同步触发
// Watch 回调（插件代码），不能在持有 Manager 锁时调用。
func (pm *Manager) collectContainerBackfillLocked() []containerBackfill {
	if pm.container == nil {
		pm.container = NewContainer()
	}
	var out []containerBackfill
	for pluginName, inst := range pm.plugins {
		if !pm.container.Has(pluginName) {
			out = append(out, containerBackfill{pluginName, inst})
		}
	}
	if !pm.container.Has("manager") {
		out = append(out, containerBackfill{"manager", pm})
	}
	if !pm.container.Has("engine") {
		out = append(out, containerBackfill{"engine", pm.coordinator})
	}
	if !pm.container.Has("coordinator") {
		out = append(out, containerBackfill{"coordinator", pm.coordinator})
	}
	return out
}

// applyContainerBackfill 在锁外将收集到的条目注册进容器。
func (pm *Manager) applyContainerBackfill(items []containerBackfill) {
	if len(items) == 0 {
		return
	}
	pm.mu.RLock()
	c := pm.container
	pm.mu.RUnlock()
	if c == nil {
		return
	}
	for _, it := range items {
		if !c.Has(it.name) {
			c.Register(it.name, it.value)
		}
	}
}

// ManagerStats 插件管理器的运行时统计快照。
type ManagerStats struct {
	PluginsTotal      int              `json:"plugins_total"`
	PluginsByState    map[string]int   `json:"plugins_by_state"`
	LoadOrder         []string         `json:"load_order"`
	GoroutineSummary  GoroutineSummary `json:"goroutine_summary"`
	EventBusStats     EventBusStats    `json:"event_bus_stats"`
	DrainingCount     int              `json:"draining_count"`
	ContainerFrozen   bool             `json:"container_frozen"`
	ContainerServices int              `json:"container_services"`
	StrictDeps        bool             `json:"strict_deps"`
	Uptime            string           `json:"uptime,omitempty"`
}
