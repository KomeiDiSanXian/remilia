package plugin

import (
	"context"
	"fmt"
	"slices"
	"sync"

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

// Manager 插件管理器
type Manager struct {
	plugins        map[string]*Instance
	coordinator    engine.PluginCoordinator
	listeners      []LifecycleListener
	configProvider ConfigProvider
	loadOrder      []string
	container      *Container
	eventBus       EventBus
	strictDeps     bool
	metaGM         *goroutineManager
	mu             sync.RWMutex
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
		listeners:   make([]LifecycleListener, 0),
		loadOrder:   make([]string, 0),
		container:   NewContainer(),
		eventBus:    NewEventBus(),
		metaGM:      newGoroutineManagerForPlugin("manager"),
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.configProvider != nil {
		m.configProvider.OnConfigChange(m.propagateConfigChange)
	}
	return m
}

// NewManagerWithEventBusOptions 使用自定义 EventBus 选项创建插件管理器。
func NewManagerWithEventBusOptions(coordinator engine.PluginCoordinator, ebOpts EventBusOptions, opts ...ManagerOption) *Manager {
	pm := NewManager(coordinator, opts...)
	pm.eventBus = NewEventBusWithOptions(ebOpts)
	return pm
}

// Coordinator 返回底层协调器（供需要直接访问 engine 的插件使用，如 debug）
func (pm *Manager) Coordinator() engine.PluginCoordinator {
	return pm.coordinator
}

// Disable 禁用插件（暂停事件响应，但保持注册状态）。
//
// 与 Unregister 的区别：
//   - Unregister: 完全移除插件，需要重新注册才能恢复
//   - Disable: 将状态置为 Disabled，通过 Enable 即可恢复，不触发 Teardown，不影响 Container 中的服务
//
// 禁用后：
//   - engine.Matcher 被挂起（engine.DisableGroup 暂停分发）
//   - GetState() 返回 Disabled
//   - 可通过 Enable(name) 恢复
func (pm *Manager) Disable(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.coordinator == nil {
		return fmt.Errorf("cannot disable plugin %q: manager has no engine coordinator; Register Engine before use", name)
	}

	inst, exists := pm.plugins[name]
	if !exists {
		return errutil.ErrPluginNotFound
	}

	state := inst.GetState()
	if state == Disabled {
		logger.Warnf("[PluginManager] Plugin %s is already disabled", name)
		return nil
	}
	if state != Loaded {
		return fmt.Errorf("plugin %s is not in Loaded state (state: %s)", name, state)
	}

	pm.coordinator.DisableGroup(name)
	inst.SetState(Disabled)
	logger.Infof("[PluginManager] Plugin %s disabled (matchers paused, container intact)", name)
	return nil
}

// Enable 启用已禁用的插件（恢复事件响应）。
func (pm *Manager) Enable(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.coordinator == nil {
		return fmt.Errorf("cannot enable plugin %q: manager has no engine coordinator; Register Engine before use", name)
	}

	inst, exists := pm.plugins[name]
	if !exists {
		return errutil.ErrPluginNotFound
	}
	if inst.GetState() != Disabled {
		logger.Warnf("[PluginManager] Plugin %s is not disabled (state: %s)", name, inst.GetState())
		return nil
	}

	pm.coordinator.EnableGroup(name)
	inst.SetState(Loaded)
	logger.Infof("[PluginManager] Plugin %s enabled (matchers resumed)", name)
	return nil
}

// IsDisabled 检查插件是否被禁用
func (pm *Manager) IsDisabled(name string) bool {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()
	if !exists {
		return false
	}
	return inst.GetState() == Disabled
}

// SetStrictDeps 设置严格依赖模式。
//
// 启用后（strictDeps=true），若插件在 Setup 中通过 [Service] / [TryService] 访问了
// 未在 Deps 字段声明的插件，注册时将返回错误而不是警告，
// 防止隐式依赖导致拓扑排序失效或生命周期管理混乱。
func (pm *Manager) SetStrictDeps(enabled bool) {
	pm.mu.Lock()
	pm.strictDeps = enabled
	pm.mu.Unlock()
}

// IsStrictDeps 返回当前严格依赖模式状态
func (pm *Manager) IsStrictDeps() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.strictDeps
}

// SetConfigProvider 设置全局配置提供者并订阅变更事件。
//
// 当底层配置源变更时自动触发所有已加载插件的 Config.Reload() 和 OnChange 回调。
// 若已通过 WithConfigProvider 在构造时注入，则无需调用此方法。
//
// 并发安全说明：
//   - 旧 provider 的 Stop() 在锁外执行（防止 Stop 中 I/O 阻塞其他操作）
//   - Config 构造（NewPluginConfigFromProvider → Sub()）在锁外执行（可能 I/O）
//   - 锁内仅执行指针赋值：configProvider 替换、inst.SetConfig（字段赋值）
//   - 与 Register 的 Lock#1 可能重叠：Register 会读到旧的 configProvider，
//     但 Register 在 Lock#2 时 Store 实例，Lock#3 中 SetConfigProvider 已更新配置
//   - 若 SetConfigProvider 中发现新注册的插件未包含在事先收集的插件列表中，
//     也不影响正确性——该插件使用最新的 configProvider 读取配置
func (pm *Manager) SetConfigProvider(cp ConfigProvider) {
	// Phase 1: 断开旧 provider 的监听（锁外执行 Stop）
	var oldStopFn func()
	pm.mu.Lock()
	if oldProvider := pm.configProvider; oldProvider != nil {
		if s, ok := oldProvider.(interface{ Stop() }); ok {
			oldStopFn = s.Stop
		}
	}
	pm.mu.Unlock()
	if oldStopFn != nil {
		oldStopFn()
	}

	// Phase 2: 替换 provider + 收集插件名（锁内，短操作）
	pm.mu.Lock()
	pm.configProvider = cp
	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	pm.mu.Unlock()

	// Phase 3: 构造 Config（锁外，可能 I/O）
	type nameConfig struct {
		name   string
		config Config
	}
	newCfgs := make([]nameConfig, 0, len(names))
	if cp != nil {
		for _, name := range names {
			newCfgs = append(newCfgs, nameConfig{name, NewPluginConfigFromProvider(name, cp)})
		}
	}

	// Phase 4: 应用 Config + 注册回调（锁内）
	pm.mu.Lock()
	if cp != nil {
		for _, nc := range newCfgs {
			if inst, ok := pm.plugins[nc.name]; ok {
				inst.SetConfig(nc.config)
			}
		}
		cp.OnConfigChange(pm.propagateConfigChange)
	}
	pm.mu.Unlock()
}

// propagateConfigChange 向所有已加载插件的 Config 广播配置变更
//
// 使用 TryRLock 避免 SetConfigProvider 持有写锁时同步触发导致死锁。
// 若锁被写操作持有，直接返回（Phase 3 的全量替换已保证数据最新）。
func (pm *Manager) propagateConfigChange() {
	if !pm.mu.TryRLock() {
		logger.Warn("[Manager] Config change notification skipped: write lock held (SetConfigProvider in progress, Phase 3 full replacement already up to date)")
		return
	}
	defer pm.mu.RUnlock()

	instances := make([]*Instance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		instances = append(instances, inst)
	}

	for _, inst := range instances {
		cfg := inst.GetConfig()
		if cfg != nil {
			if err := cfg.Reload(); err != nil {
				logger.WithError(err).Warnf("[Manager] Failed to reload config for plugin %s", inst.Name())
			}
		}
	}
}

// AddListener 添加生命周期监听器
func (pm *Manager) AddListener(listener LifecycleListener) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.listeners = append(pm.listeners, listener)
}

// RemoveListener 移除生命周期监听器
func (pm *Manager) RemoveListener(listener LifecycleListener) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	newListeners := make([]LifecycleListener, 0, len(pm.listeners))
	for _, l := range pm.listeners {
		if l != listener {
			newListeners = append(newListeners, l)
		}
	}
	pm.listeners = newListeners
}

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

// Retry 重新尝试加载处于 Error 状态的插件。
// 相当于 ForceUnregister + Register，但保留插件的 Descriptor。
//
// 仅在插件状态为 Error 时可用；其他状态返回错误。
func (pm *Manager) Retry(name string, desc *Descriptor) error {
	pm.mu.Lock()
	inst, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		return errutil.ErrPluginNotFound
	}
	if inst.GetState() != Error {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %s is not in Error state (state: %s)", name, inst.GetState())
	}
	pm.mu.Unlock()

	// 强制卸载
	if err := pm.ForceUnregister(name); err != nil {
		return fmt.Errorf("retry %s: force unregister failed: %w", name, err)
	}

	// 重新注册
	return pm.Register(desc)
}

// Reload 重新加载插件（热重载）
func (pm *Manager) Reload(ctx context.Context, name string) error {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		logger.Warnf("[PluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	state := inst.GetState()
	if state == Disabled {
		return fmt.Errorf("plugin %s is disabled, use Enable before Reload", name)
	}
	if state == Error {
		return fmt.Errorf("plugin %s is in Error state, use Retry instead of Reload", name)
	}

	logger.Infof("[PluginManager] Reloading plugin %s", name)

	if err := inst.reload(ctx, pm.coordinator); err != nil {
		logger.WithError(err).Errorf("[PluginManager] Failed to reload plugin %s", name)
		pm.notifyError(name, "reload", err)
		return err
	}

	logger.Infof("[PluginManager] Plugin %s reloaded successfully", name)
	pm.notifyReloaded(name)

	// 通知所有依赖了 name 插件的其他插件
	pm.notifyDependents(name)
	return nil
}

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
		pm.metaGM.goNamed_(fmt.Sprintf("notify-%s->%s", reloadedPlugin, depName), func(ctx context.Context) {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("panic", r).Errorf("[PluginManager] Panic in OnDependencyReloaded for plugin dependency %s", reloadedPlugin)
				}
			}()
			cb(reloadedPlugin)
		})
	}
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
// 在所有插件通过 Register/RegisterMultiple 加载完成后调用此方法，
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
