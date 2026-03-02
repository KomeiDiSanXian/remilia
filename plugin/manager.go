package plugin

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// LifecycleListener 插件生命周期监听器接口
type LifecycleListener interface {
	// OnPluginLoaded 插件加载完成时调用
	OnPluginLoaded(name string)

	// OnPluginUnloaded 插件卸载完成时调用
	OnPluginUnloaded(name string)

	// OnPluginReloaded 插件重载完成时调用
	OnPluginReloaded(name string)

	// OnPluginError 插件操作发生错误时调用
	OnPluginError(name string, operation string, err error)
}

// Manager 插件管理器
//
// plugins 字段改为 map[string]*PluginInstance，消除对 Plugin 公开接口的依赖：
//   - 所有外部 API 直接返回 *PluginInstance，类型明确；
//   - 内部生命周期调用通过 pluginInternal 私有接口驱动；
//   - Disable/Enable 不再维护独立 disabled map，状态由 PluginInstance.state 字段管理。
type Manager struct {
	plugins     map[string]*PluginInstance
	coordinator *engine.Engine
	listeners   []LifecycleListener // 生命周期监听器列表
	viper       *viper.Viper        // 全局配置
	loadOrder   []string            // 插件加载顺序
	container   *Container          // 依赖注入容器（v2）
	eventBus    EventBus            // 插件间事件总线
	strictDeps  bool                // 严格依赖模式：未声明依赖拒绝注册
	metaGM      *goroutineManager   // 管理 notifyDependents 等元数据 goroutine
	mu          sync.RWMutex
}

// NewManager 创建插件管理器
func NewManager(coordinator *engine.Engine) *Manager {
	return &Manager{
		plugins:     make(map[string]*PluginInstance),
		coordinator: coordinator,
		listeners:   make([]LifecycleListener, 0),
		loadOrder:   make([]string, 0),
		container:   NewContainer(),
		eventBus:    NewEventBus(),
		metaGM:      newGoroutineManagerForPlugin("manager"),
	}
}

// NewManagerWithEventBusOptions 使用自定义 EventBus 选项创建插件管理器。
// 适用于需要调整事件处理并发度的场景（高流量或低资源环境）。
func NewManagerWithEventBusOptions(coordinator *engine.Engine, ebOpts EventBusOptions) *Manager {
	pm := NewManager(coordinator)
	pm.eventBus = NewEventBusWithOptions(ebOpts)
	return pm
}

// Coordinator 返回底层 engine（供需要直接访问 engine 的插件使用，如 debug）
func (pm *Manager) Coordinator() *engine.Engine {
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

	inst, exists := pm.plugins[name]
	if !exists {
		return errutil.ErrPluginNotFound
	}

	state := inst.GetState()
	if state == Disabled {
		logger.Warnf("[pluginManager] Plugin %s is already disabled", name)
		return nil
	}

	if state != Loaded {
		return fmt.Errorf("plugin %s is not in Loaded state (state: %s)", name, state)
	}

	// 暂停 engine 中该插件组的所有 Matcher 分发
	if pm.coordinator != nil {
		pm.coordinator.DisableGroup(name)
	}

	// 状态直接写入 PluginInstance，不再维护独立 disabled map
	inst.SetState(Disabled)
	logger.Infof("[pluginManager] Plugin %s disabled (matchers paused, container intact)", name)
	return nil
}

// Enable 启用已禁用的插件（恢复事件响应）。
func (pm *Manager) Enable(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	inst, exists := pm.plugins[name]
	if !exists {
		return errutil.ErrPluginNotFound
	}

	if inst.GetState() != Disabled {
		logger.Warnf("[pluginManager] Plugin %s is not disabled (state: %s)", name, inst.GetState())
		return nil
	}

	// 恢复 engine 中该插件组的 Matcher 分发
	if pm.coordinator != nil {
		pm.coordinator.EnableGroup(name)
	}

	inst.SetState(Loaded)
	logger.Infof("[pluginManager] Plugin %s enabled (matchers resumed)", name)
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
// 启用后（strictDeps=true），若插件在 Setup 中通过 Get/MustGet 访问了
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

// SetViper 设置全局配置（用于插件配置管理）并订阅变更事件，
// 当底层配置文件变更时自动触发所有已加载插件的 Config.Reload() 和 OnChange 回调。
func (pm *Manager) SetViper(v *viper.Viper) {
	pm.mu.Lock()
	pm.viper = v
	pm.mu.Unlock()

	// 订阅 viper 配置变更事件，自动传播到所有插件配置
	if v != nil {
		v.OnConfigChange(func(_ fsnotify.Event) {
			pm.propagateConfigChange()
		})
	}
}

// propagateConfigChange 向所有已加载插件的 Config 广播配置变更
func (pm *Manager) propagateConfigChange() {
	pm.mu.RLock()
	instances := make([]*PluginInstance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		instances = append(instances, inst)
	}
	pm.mu.RUnlock()

	for _, inst := range instances {
		if configurable, ok := any(inst).(ConfigurablePlugin); ok {
			cfg := configurable.GetConfig()
			if cfg != nil {
				if err := cfg.Reload(); err != nil {
					logger.WithError(err).Warnf("[Manager] Failed to reload config for plugin %s", inst.Name())
				}
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

// notifyLoaded 通知监听器插件已加载（P1-4: 每个回调加 panic recover）
func (pm *Manager) notifyLoaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	bus := pm.eventBus
	pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginLoaded", func() { listener.OnPluginLoaded(name) })
	}
	// 向 EventBus 发布生命周期事件（Bug 2.8：help 插件订阅此事件以清空缓存）
	if bus != nil {
		_ = bus.Publish("plugin.loaded", name)
	}
}

// notifyUnloaded 通知监听器插件已卸载（P1-4: 每个回调加 panic recover）
func (pm *Manager) notifyUnloaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	bus := pm.eventBus
	pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginUnloaded", func() { listener.OnPluginUnloaded(name) })
	}
	if bus != nil {
		_ = bus.Publish("plugin.unloaded", name)
	}
}

// notifyReloaded 通知监听器插件已重载（P1-4: 每个回调加 panic recover）
func (pm *Manager) notifyReloaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	bus := pm.eventBus
	pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginReloaded", func() { listener.OnPluginReloaded(name) })
	}
	if bus != nil {
		_ = bus.Publish("plugin.reloaded", name)
	}
}

// notifyError 通知监听器插件操作发生错误（P1-4: 每个回调加 panic recover）
func (pm *Manager) notifyError(name string, operation string, err error) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginError", func() { listener.OnPluginError(name, operation, err) })
	}
}

// safeNotify 安全调用通知回调，捕获 panic 防止单个监听器崩溃影响整个通知链
func safeNotify(pluginName, callback string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"plugin":   pluginName,
				"callback": callback,
				"panic":    r,
			}).Error("[pluginManager] LifecycleListener panic recovered")
		}
	}()
	fn()
}

// Unregister 注销插件，返回错误信息
func (pm *Manager) Unregister(name string) error {
	pm.mu.Lock()

	inst, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[pluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	// 卸载插件
	if err := inst.unload(pm.coordinator); err != nil {
		// Unload 失败时，标记插件为 Error 状态，
		// 防止插件在损坏状态下继续被 Reload/Get 等操作使用。
		inst.SetState(Error)
		pm.mu.Unlock()
		pm.notifyError(name, "unload", err)
		return err
	}

	delete(pm.plugins, name)
	pm.container.Remove(name)
	pm.mu.Unlock()

	logger.Infof("[pluginManager] Plugin %s unregistered", name)
	pm.notifyUnloaded(name) // 通知监听器（在锁外）
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
		logger.Warnf("[pluginManager] ForceUnregister: plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	delete(pm.plugins, name)
	pm.container.Remove(name)
	pm.mu.Unlock()

	logger.Warnf("[pluginManager] Plugin %s force unregistered (Unload skipped)", name)
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
func (pm *Manager) UnregisterCascade(name string) error {
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

	logger.Infof("[pluginManager] UnregisterCascade: will unregister %d plugin(s) in order: %v", len(order), order)

	for _, n := range order {
		if err := pm.Unregister(n); err != nil {
			logger.WithError(err).Errorf("[pluginManager] UnregisterCascade: failed to unregister plugin %s", n)
			return fmt.Errorf("cascade unregister %s: %w", n, err)
		}
	}
	return nil
}

// Reload 重新加载插件（热重载）
func (pm *Manager) Reload(name string) error {
	pm.mu.RLock()
	inst, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		logger.Warnf("[pluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	logger.Infof("[pluginManager] Reloading plugin %s", name)

	if err := inst.reload(pm.coordinator); err != nil {
		logger.WithError(err).Errorf("[pluginManager] Failed to reload plugin %s", name)
		pm.notifyError(name, "reload", err)
		return err
	}

	logger.Infof("[pluginManager] Plugin %s reloaded successfully", name)
	pm.notifyReloaded(name)

	// 通知所有依赖了 name 插件的其他插件
	pm.notifyDependents(name)
	return nil
}

// notifyDependents 通知依赖了 reloadedPlugin 的所有其他插件
func (pm *Manager) notifyDependents(reloadedPlugin string) {
	pm.mu.RLock()
	instances := make(map[string]*PluginInstance, len(pm.plugins))
	maps.Copy(instances, pm.plugins)
	pm.mu.RUnlock()

	for depName, inst := range instances {
		if depName == reloadedPlugin {
			continue
		}
		if !slices.Contains(inst.desc.Deps, reloadedPlugin) {
			continue
		}
		cb := inst.desc.getOnDependencyReloaded()
		if cb == nil {
			continue
		}
		logger.Infof("[pluginManager] Notifying plugin %s that dependency %s was reloaded", depName, reloadedPlugin)
		// 使用 metaGM 管理此类元数据 goroutine，Shutdown 时可感知并等待
		pm.metaGM.goNamed_(fmt.Sprintf("notify-%s->%s", reloadedPlugin, depName), func(ctx context.Context) {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("panic", r).Errorf("[pluginManager] Panic in OnDependencyReloaded for plugin dependency %s", reloadedPlugin)
				}
			}()
			cb(reloadedPlugin)
		})
	}
}

// Get 获取插件实例。
// 若插件处于 Loading 状态（正在初始化），返回 nil, false，调用方需等待。
func (pm *Manager) Get(name string) (*PluginInstance, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	inst, exists := pm.plugins[name]
	if !exists {
		return nil, false
	}

	// 正在加载时不对外暴露
	if inst.GetState() == Loading {
		logger.Warnf("[pluginManager] Plugin %s is currently loading, please wait", name)
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

	// 填充 EventBus 全局订阅数快照
	pm.mu.RLock()
	bus := pm.eventBus
	pm.mu.RUnlock()
	if bus != nil {
		stats := bus.GetStats()
		status.EventBusSubscriptions = stats.SubscriptionCount
	}

	// 填充活跃 goroutine 数量
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

// AsLifecycleComponent 将插件实例转换为 lifecycle.Component
func (pm *Manager) AsLifecycleComponent(inst *PluginInstance) lifecycle.Component {
	return NewPluginComponent(inst, pm.coordinator, pm)
}

// RegisterToLifecycle 将所有已注册的插件注册到 lifecycle.Manager
func (pm *Manager) RegisterToLifecycle(lm *lifecycle.Manager) error {
	pm.mu.RLock()
	instances := make([]*PluginInstance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		instances = append(instances, inst)
	}
	pm.mu.RUnlock()

	for _, inst := range instances {
		component := pm.AsLifecycleComponent(inst)
		lm.Register(component)
	}

	logger.Infof("[pluginManager] Registered %d plugins to lifecycle manager", len(instances))
	return nil
}

// GetContainer 获取依赖注入容器（v2 API）
func (pm *Manager) GetContainer() *Container {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.container
}

// FreezeContainer 冻结依赖注入容器，切换为无锁只读模式。
//
// 在所有插件通过 RegisterV2/RegisterMultipleV2 加载完成后调用此方法，
// 后续 Get/Has 操作将使用原子指针快照，读性能提升 2-3x。
func (pm *Manager) FreezeContainer() {
	pm.mu.RLock()
	c := pm.container
	pm.mu.RUnlock()
	if c != nil {
		c.Freeze()
	}
	logger.Info("[pluginManager] Container frozen, Get/Has now use lock-free read")
}

// GetEventBus 获取插件间事件总线
func (pm *Manager) GetEventBus() EventBus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.eventBus
}

// AsPluginInfo 返回 Manager 的只读视图（PluginInfo 接口）。
// 供向后兼容代码（如已废弃的 SetPluginManager）使用。
func (pm *Manager) AsPluginInfo() PluginInfo {
	return newPluginInfo(pm)
}

// ListPluginGoroutines 返回所有插件的受管后台 goroutine 信息快照。
//
// 可用于调试（如 dump goroutine 列表、监控后台任务）。
// 仅包含通过 ctx.Go / ctx.GoNamed 启动的 goroutine，不包含系统级 goroutine。
func (pm *Manager) ListPluginGoroutines() []GoroutineInfo {
	pm.mu.RLock()
	instances := make([]*PluginInstance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		instances = append(instances, inst)
	}
	pm.mu.RUnlock()

	var result []GoroutineInfo
	for _, inst := range instances {
		inst.mu.RLock()
		gm := inst.goroutineMgr
		inst.mu.RUnlock()
		if gm != nil {
			result = append(result, gm.listGoroutines()...)
		}
	}
	return result
}

// StartAll 启动所有已通过 RegisterV2 预注册（Unloaded 状态）的插件。
//
// 通常由 Bot.Start() 自动调用，无需手动调用。
// 若某个插件 Setup 失败，继续尝试其余插件并收集错误，最终返回合并错误。
// 已处于 Loaded 状态的插件会跳过（幂等）。
func (pm *Manager) StartAll(ctx context.Context) error {
	pm.mu.RLock()
	names := make([]string, len(pm.loadOrder))
	copy(names, pm.loadOrder)
	pm.mu.RUnlock()

	var errs []error
	for _, name := range names {
		pm.mu.RLock()
		inst, exists := pm.plugins[name]
		pm.mu.RUnlock()
		if !exists {
			continue
		}
		if inst.GetState() == Loaded {
			continue // 已加载，跳过
		}
		if err := inst.load(pm.coordinator); err != nil {
			logger.WithError(err).Errorf("[pluginManager] StartAll: plugin %s failed to start", name)
			pm.notifyError(name, "start", err)
			errs = append(errs, fmt.Errorf("plugin %q: %w", name, err))
		} else {
			inst.SetState(Loaded)
			pm.notifyLoaded(name)
		}
	}

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("StartAll: %d plugin(s) failed: %v", len(errs), msgs)
	}
	return nil
}

// StopAll 按逆加载顺序停止所有已加载插件，调用各插件的 Teardown。
//
// 通常由 Bot.Stop() 自动调用，无需手动调用。
// 若某个插件 Teardown 失败，继续处理其余插件并收集错误。
func (pm *Manager) StopAll(ctx context.Context) error {
	pm.mu.RLock()
	// 逆序：最后加载的最先卸载
	order := make([]string, len(pm.loadOrder))
	copy(order, pm.loadOrder)
	pm.mu.RUnlock()

	var errs []error
	// 从后往前遍历
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		pm.mu.RLock()
		inst, exists := pm.plugins[name]
		pm.mu.RUnlock()
		if !exists {
			continue
		}
		if inst.GetState() != Loaded && inst.GetState() != Disabled {
			continue
		}
		if err := inst.unload(pm.coordinator); err != nil {
			logger.WithError(err).Errorf("[pluginManager] StopAll: plugin %s failed to stop", name)
			pm.notifyError(name, "stop", err)
			errs = append(errs, fmt.Errorf("plugin %q: %w", name, err))
		} else {
			pm.notifyUnloaded(name)
		}
	}

	// 停止 Manager 自身的后台 goroutine
	pm.Shutdown()

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("StopAll: %d plugin(s) failed: %w", len(errs), errs[0])
	}
	return nil
}

// Shutdown 停止 Manager 管理的所有内部后台 goroutine（如 notifyDependents 等元数据 goroutine）。
//
// 在进程退出前调用，防止 goroutine 泄漏导致 data race。
// 注意：此方法不卸载插件（如需卸载，请先调用 StopAll 或 UnregisterAll）。
func (pm *Manager) Shutdown() {
	if pm.metaGM != nil {
		pm.metaGM.stopAndWait()
	}
}
