package plugin

import (
	"context"
	"fmt"
	"slices"
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

// --- 控制器访问 ---

// Lifecycle 返回插件生命周期控制器。
func (pm *Manager) Lifecycle() *lifecycleController { return pm.lifecycle }

// Config 返回插件配置控制器。
func (pm *Manager) Config() *configController { return pm.config }

// --- 快捷委托方法（保持外部 API 不变）---

func (pm *Manager) Coordinator() engine.PluginCoordinator { return pm.coordinator }
func (pm *Manager) Disable(name string) error              { return pm.lifecycle.Disable(name) }
func (pm *Manager) Enable(name string) error               { return pm.lifecycle.Enable(name) }
func (pm *Manager) IsDisabled(name string) bool             { return pm.lifecycle.IsDisabled(name) }
func (pm *Manager) Reload(ctx context.Context, name string) error {
	return pm.lifecycle.Reload(ctx, name)
}
func (pm *Manager) Retry(name string, desc *Descriptor) error { return pm.lifecycle.Retry(name, desc) }
func (pm *Manager) StartAll(ctx context.Context) error         { return pm.lifecycle.StartAll(ctx) }
func (pm *Manager) StopAll(ctx context.Context) error          { return pm.lifecycle.StopAll(ctx) }
func (pm *Manager) Shutdown()                                  { pm.lifecycle.Shutdown() }
func (pm *Manager) AddListener(l LifecycleListener)             { pm.lifecycle.AddListener(l) }
func (pm *Manager) RemoveListener(l LifecycleListener)          { pm.lifecycle.RemoveListener(l) }
func (pm *Manager) SetStrictDeps(enabled bool)                  { pm.config.SetStrictDeps(enabled) }
func (pm *Manager) IsStrictDeps() bool                          { return pm.config.IsStrictDeps() }
func (pm *Manager) SetConfigProvider(cp ConfigProvider)         { pm.config.SetProvider(cp) }
func (pm *Manager) Stats() ManagerStats                         { return pm.stats.Snapshot() }
func (pm *Manager) ListDrainingInstances() map[string]*DrainingInfo {
	return pm.stats.ListDraining()
}
func (pm *Manager) GoroutineSummary() GoroutineSummary { return pm.stats.GoroutineSummary() }
func (pm *Manager) ListAllGoroutines() []GoroutineInfo { return pm.stats.ListGoroutines() }

// --- 核心方法（Manager 自有）---

// Unregister 注销插件，返回错误信息。
// ctx 用于控制超时：若 context 在 Teardown 完成前到期，返回 ctx.Err()。
func (pm *Manager) Unregister(ctx context.Context, name string) error {
	pm.mu.Lock()

	inst, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[PluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	if err := inst.unload(ctx, pm.coordinator); err != nil {
		inst.SetState(Error)
		pm.mu.Unlock()
		pm.notifyError(name, "unload", err)
		return err
	}

	delete(pm.plugins, name)
	pm.container.Remove(name)
	pm.mu.Unlock()

	logger.Infof("[PluginManager] Plugin %s unregistered", name)
	pm.notifyUnloaded(name)
	return nil
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

	if _, exists := pm.plugins[name]; !exists {
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
	pm.container.Remove(name)
	pm.mu.Unlock()

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
	dependents := make(map[string][]string)
	for pName, inst := range pm.plugins {
		for _, dep := range inst.desc.Deps {
			dependents[dep] = append(dependents[dep], pName)
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
		HasSaveState: inst.desc.effectiveAdvanced().SaveState != nil,
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
// 可使用 RegisterOption 控制行为：
//   - WithInferDeps(): 使用 DryRun 自动推断依赖（等价于旧 RegisterMultipleSmart）
//   - WithAtomic(): 失败时自动逆序回滚
//
// 当传入多个 Descriptor 时，进行批量注册：
//   - 默认内部拓扑排序后逐一注册（等价于旧 RegisterMultiple）
//   - WithAtomic() → 失败回滚（等价于旧 RegisterMultipleAtomic）
//   - WithInferDeps() → DryRun 推断后注册（等价于旧 RegisterMultipleSmart）
func (pm *Manager) Register(first *Descriptor, rest ...*Descriptor) error {
	all := append([]*Descriptor{first}, rest...)
	return pm.registerWithOptions(all, registerOptions{})
}

// RegisterMultiple 批量注册多个插件，自动处理依赖顺序。
//
// 拓扑排序基于声明 Deps + OptionalDeps。Setup 中发现的未声明依赖（通过 COW 合并）
// 会在所有插件注册完成后由 rectifyLoadOrder 修正 loadOrder（不影响 Setup 顺序）。
//
// Deprecated: 使用 Register(desc1, desc2, ...) 替代。
func (pm *Manager) RegisterMultiple(descriptors []*Descriptor) error {
	return pm.registerWithOptions(descriptors, registerOptions{})
}

// RegisterMultipleAtomic 原子批量注册：任意插件失败时，自动逆序回滚已注册的插件。
//
// Deprecated: 使用 Register(desc1, desc2, ..., WithAtomic()) 替代。
func (pm *Manager) RegisterMultipleAtomic(descriptors []*Descriptor) error {
	return pm.registerWithOptions(descriptors, registerOptions{atomic: true})
}

// RegisterMultipleSmart 智能批量注册：自动推断依赖关系（无需手动声明 Deps）。
//
// Deprecated: 使用 Register(desc1, desc2, ..., WithInferDeps()) 替代。
func (pm *Manager) RegisterMultipleSmart(descriptors []*Descriptor) error {
	return pm.registerWithOptions(descriptors, registerOptions{inferDeps: true})
}

// ValidateDependencies 验证一组插件的依赖关系（不注册）
//
// Deprecated: 将直接使用 topologicalSort 验证。
func (pm *Manager) ValidateDependencies(descriptors []*Descriptor) error {
	_, err := pm.topologicalSort(descriptors)
	return err
}

// registerOptions 控制 Register 的行为。
type registerOptions struct {
	atomic   bool // 失败时自动回滚
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
func (pm *Manager) registerWithOptions(descriptors []*Descriptor, opts registerOptions) error {
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
		return pm.registerSingle(desc)
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
	sorted, err := pm.topologicalSort(descriptors)
	if err != nil {
		if opts.inferDeps {
			return err // 错误信息已包含循环依赖提示
		}
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// 逐一注册
	registered := make([]string, 0, len(sorted))
	registeredInsts := make([]*Instance, 0, len(sorted))
	for _, desc := range sorted {
		if err := pm.registerSingle(desc); err != nil {
			if opts.atomic {
				for i := len(registered) - 1; i >= 0; i-- {
					if rollbackErr := pm.Unregister(context.Background(), registered[i]); rollbackErr != nil {
						logger.WithError(rollbackErr).Warnf("[PluginManager] Rollback failed for plugin %s", registered[i])
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
					Cause:             err,
					RegisteredPlugins: existingNames,
					Hint:              "all previously registered plugins in this batch have been rolled back",
				}
			}
			return fmt.Errorf("failed to register plugin %s: %w", desc.Name, err)
		}
		registered = append(registered, desc.Name)
		if inst, ok := pm.Get(desc.Name); ok {
			registeredInsts = append(registeredInsts, inst)
		}
	}

	pm.rectifyLoadOrder(registeredInsts)
	logger.Infof("[PluginManager] Successfully registered %d plugins in dependency order", len(sorted))
	return nil
}

// registerSingle 注册单个插件（三段锁策略）。
//
// 锁策略（三段式，最小化锁持有时间）：
//
//	Lock #1: 重复检查 + 依赖校验 + 版本约束
//	(解锁)  ←  I/O：NewPluginConfigFromProvider / validateConfigSchema
//	Lock #2: 再次重复检查 + pm.plugins[name] = instance
//	(解锁)
//	instance.load()  ← 已在锁外
//	Lock #3: 错误清理 / 依赖合并 / loadOrder / container.Register
func (pm *Manager) registerSingle(desc *Descriptor) error {
	if err := validateDescriptor(desc); err != nil {
		return err
	}

	name := desc.Name

	// ========== Lock #1: 快速校验 ==========
	pm.mu.Lock()

	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logger.Warnf("[PluginManager] Plugin %s already registered", name)
		return errutil.ErrPluginAlreadyExists
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
		return err
	}

	if err := validateVersionConstraints(pm, desc); err != nil {
		pm.mu.Unlock()
		return err
	}

	pm.ensureContainerInitialized()

	// 在锁内读取 configProvider，移出锁外调用（I/O 可能阻塞）
	cp := pm.config.configProvider

	// Reload/Strategy 检测（仅读 desc，不需要锁，但已在锁内顺手完成）
	if desc.Advanced != nil && desc.Advanced.Reload != nil && desc.Advanced.Strategy != ReloadInPlace {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q: Advanced.Reload is set but Strategy is %v (not ReloadInPlace). "+
			"The Reload func will NOT be called with this strategy. Did you mean Strategy: plugin.ReloadInPlace", name, desc.Advanced.Strategy)
	}

	pm.mu.Unlock()
	// ========== Lock #1 结束 ==========

	// ========== 无锁区：Config 构造（可能 I/O）+ Schema 校验 ==========
	var config Config
	if cp != nil {
		config = NewPluginConfigFromProvider(name, cp)
	}

	if err := validateConfigSchema(name, desc, config); err != nil {
		return err
	}

	// ========== 无锁区：实例 + SetupContext 构建（纯内存操作）==========
	instance := &Instance{
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
	// 二次重复检查：Lock#1~Lock#2 窗口期可能已被其他 Register 抢占注册同名插件
	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		return errutil.ErrPluginAlreadyExists
	}
	pm.plugins[name] = instance
	pm.mu.Unlock()
	// ========== Lock #2 结束 ==========

	// ========== 无锁区：执行 Setup ==========
	loadErr := instance.load(context.Background())

	// ========== Lock #3: 最终化 ==========
	pm.mu.Lock()

	if loadErr != nil {
		if pm.coordinator != nil {
			pm.coordinator.RemoveGroup(name)
		}
		delete(pm.plugins, name)
		pm.container.Remove(name)
		pm.mu.Unlock()
		logger.WithError(loadErr).Errorf("[PluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", loadErr)
		return loadErr
	}

	trackedDeps := setupCtx.getTrackedDependencies()
	trackedOptional := setupCtx.getTrackedOptionalDependencies()

	allTracked := make(map[string]bool, len(trackedDeps)+len(trackedOptional))
	for _, d := range trackedDeps {
		allTracked[d] = true
	}
	for _, d := range trackedOptional {
		allTracked[d] = true
	}

	if len(allTracked) > 0 {
		declaredDeps := make(map[string]bool)
		for _, dep := range desc.Deps {
			declaredDeps[dep] = true
		}
		for _, dep := range desc.OptionalDeps {
			declaredDeps[dep] = true
		}

		undeclaredAll := make([]string, 0)
		for dep := range allTracked {
			if !declaredDeps[dep] {
				undeclaredAll = append(undeclaredAll, dep)
			}
		}

		if len(undeclaredAll) > 0 {
			if pm.config.strictDeps {
				pm.mu.Unlock()
				if teardownErr := instance.unload(context.Background(), pm.coordinator); teardownErr != nil {
					logger.WithError(teardownErr).Warnf("[PluginManager] Failed to teardown plugin %s during strict-mode rollback", name)
				}
				pm.mu.Lock()
				delete(pm.plugins, name)
				pm.container.Remove(name)
				pm.mu.Unlock()
				return fmt.Errorf(
					"plugin %q uses undeclared dependencies %v (declared: %v); "+
						"add them to Deps or disable strict mode via manager.SetStrictDeps(false)",
					name, undeclaredAll, desc.Deps,
				)
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
			instance.desc = &newDesc
			instance.depsModified = true
		}
	}

	pm.loadOrder = append(pm.loadOrder, name)

	if !pm.container.Has(name) {
		pm.container.Register(name, instance)
	}

	pm.mu.Unlock()
	// ========== Lock #3 结束 ==========

	logger.Infof("[PluginManager] Plugin %s registered", name)
	pm.notifyLoaded(name)
	return nil
}

// --- 生命周期通知委托 ---

func (pm *Manager) notifyLoaded(name string)       { pm.lifecycle.notifyLoaded(name) }
func (pm *Manager) notifyUnloaded(name string)     { pm.lifecycle.notifyUnloaded(name) }
func (pm *Manager) notifyReloaded(name string)     { pm.lifecycle.notifyReloaded(name) }
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
		if !slices.Contains(deps, reloadedPlugin) {
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

// ensureContainerInitialized 确保依赖注入容器已初始化（须在持有 Manager 锁时调用）
func (pm *Manager) ensureContainerInitialized() {
	if pm.container == nil {
		pm.container = NewContainer()
	}
	for pluginName, plugin := range pm.plugins {
		if !pm.container.Has(pluginName) {
			pm.container.Register(pluginName, plugin)
		}
	}
	if !pm.container.Has("manager") {
		pm.container.Register("manager", pm)
	}
	if !pm.container.Has("engine") {
		pm.container.Register("engine", pm.coordinator)
	}
	if !pm.container.Has("coordinator") {
		pm.container.Register("coordinator", pm.coordinator)
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

// startTime is set at package init for uptime tracking.
var startTime = time.Now()
