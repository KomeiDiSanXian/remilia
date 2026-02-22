package plugin

import (
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
type Manager struct {
	plugins     map[string]Plugin
	coordinator *engine.Engine
	listeners   []LifecycleListener // 生命周期监听器列表
	viper       *viper.Viper        // 全局配置
	loadOrder   []string            // 插件加载顺序
	container   *Container          // 依赖注入容器（v2）
	eventBus    EventBus            // 插件间事件总线
	mu          sync.RWMutex
}

// NewManager 创建插件管理器
func NewManager(coordinator *engine.Engine) *Manager {
	return &Manager{
		plugins:     make(map[string]Plugin),
		coordinator: coordinator,
		listeners:   make([]LifecycleListener, 0),
		loadOrder:   make([]string, 0),
		container:   NewContainer(),
		eventBus:    NewEventBus(),
	}
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
	plugins := make([]Plugin, 0, len(pm.plugins))
	for _, p := range pm.plugins {
		plugins = append(plugins, p)
	}
	pm.mu.RUnlock()

	for _, p := range plugins {
		if configurable, ok := p.(ConfigurablePlugin); ok {
			cfg := configurable.GetConfig()
			if cfg != nil {
				if err := cfg.Reload(); err != nil {
					logger.WithError(err).Warnf("[Manager] Failed to reload config for plugin %s", p.Name())
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

	// 更安全的删除方式：创建新切片，避免索引越界风险
	newListeners := make([]LifecycleListener, 0, len(pm.listeners))
	for _, l := range pm.listeners {
		if l != listener {
			newListeners = append(newListeners, l)
		}
	}
	pm.listeners = newListeners
}

// notifyLoaded 通知监听器插件已加载
func (pm *Manager) notifyLoaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginLoaded(name)
	}
}

// notifyUnloaded 通知监听器插件已卸载
func (pm *Manager) notifyUnloaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginUnloaded(name)
	}
}

// notifyReloaded 通知监听器插件已重载
func (pm *Manager) notifyReloaded(name string) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginReloaded(name)
	}
}

// notifyError 通知监听器插件操作发生错误
func (pm *Manager) notifyError(name string, operation string, err error) {
	pm.mu.RLock()
	listeners := make([]LifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginError(name, operation, err)
	}
}

// --- v1 API 已在 v2.0.0 中移除 ---
//
// 以下 v1 API 方法已被移除:
//   - func (pm *Manager) Register(plugin Plugin) error
//   - func (pm *Manager) RegisterWithDependencies(plugins []Plugin) error
//   - func (pm *Manager) topologicalSort(plugins []Plugin) ([]Plugin, error)
//   - func (pm *Manager) checkDependents(name string) []string
//
// 请使用 v2 API:
//   - manager.RegisterV2(descriptor)
//
// 迁移指南: docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md

// Unregister 注销插件，返回错误信息
func (pm *Manager) Unregister(name string) error {
	pm.mu.Lock()

	plugin, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[pluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	// 卸载插件
	if err := plugin.Unload(pm.coordinator); err != nil {
		// 修复 #13：Unload 失败时，标记插件为 Error 状态，
		// 防止插件在损坏状态下继续被 Reload/Get 等操作使用。
		if stateful, ok := plugin.(statefulPluginWriter); ok {
			stateful.SetState(Error)
		}
		pm.notifyError(name, "unload", err) // 通知监听器错误
		return err
	}

	delete(pm.plugins, name)
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

	_, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logger.Warnf("[pluginManager] ForceUnregister: plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	delete(pm.plugins, name)
	pm.mu.Unlock()

	logger.Warnf("[pluginManager] Plugin %s force unregistered (Unload skipped)", name)
	pm.notifyUnloaded(name)
	return nil
}

// UnregisterCascade 级联卸载指定插件及所有依赖它的插件
// 注意：v2 API 中依赖关系通过容器自动管理
func (pm *Manager) UnregisterCascade(name string) error {
	// 直接卸载插件（v2 依赖通过容器管理，不需要级联）
	return pm.Unregister(name)
}

// Reload 重新加载插件（热重载）
// 调用插件的 Reload() 方法进行原子性重载。
// 插件可以选择实现原子性的 Reload()（失败时保持原状态），
// 或者非原子性的 Reload()（如 BasePlugin 的默认实现：先 Unload 再 Load）。
func (pm *Manager) Reload(name string) error {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		logger.Warnf("[pluginManager] Plugin %s not found", name)
		return errutil.ErrPluginNotFound
	}

	logger.Infof("[pluginManager] Reloading plugin %s", name)

	// 调用插件的 Reload 方法，传递 coordinator
	if err := plugin.Reload(pm.coordinator); err != nil {
		logger.WithError(err).Errorf("[pluginManager] Failed to reload plugin %s", name)
		pm.notifyError(name, "reload", err) // 通知监听器错误
		// Reload 失败时不删除插件，因为无法判断插件是否实现了原子性重载
		// 调用方可以根据需要调用 Unregister 来删除插件
		return err
	}

	logger.Infof("[pluginManager] Plugin %s reloaded successfully", name)
	pm.notifyReloaded(name) // 通知监听器

	// 通知所有依赖了 name 插件的其他插件（依赖方通知）
	pm.notifyDependents(name)
	return nil
}

// notifyDependents 通知依赖了 reloadedPlugin 的所有其他 v2 插件
func (pm *Manager) notifyDependents(reloadedPlugin string) {
	pm.mu.RLock()
	plugins := make(map[string]Plugin, len(pm.plugins))
	maps.Copy(plugins, pm.plugins)
	pm.mu.RUnlock()

	for depName, p := range plugins {
		if depName == reloadedPlugin {
			continue
		}
		// 只处理 PluginInstance（v2 插件）
		inst, ok := p.(*PluginInstance)
		if !ok {
			continue
		}
		// 检查该插件是否依赖了刚重载的插件
		if slices.Contains(inst.desc.Deps, reloadedPlugin) {
			// 调用 OnDependencyReloaded 回调
			if inst.desc.OnDependencyReloaded != nil {
				logger.Infof("[pluginManager] Notifying plugin %s that dependency %s was reloaded", depName, reloadedPlugin)
				go func(cb func(string), dep string) {
					defer func() {
						if r := recover(); r != nil {
							logger.WithField("panic", r).Errorf("[pluginManager] Panic in OnDependencyReloaded for plugin dependency %s", dep)
						}
					}()
					cb(dep)
				}(inst.desc.OnDependencyReloaded, reloadedPlugin)
			}
		}
	}
}

// Get 获取插件
func (pm *Manager) Get(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, false
	}

	// 检查插件状态，如果正在加载则返回 false
	if stateful, ok := plugin.(StatefulPlugin); ok {
		if stateful.GetState() == Loading {
			logger.Warnf("[pluginManager] Plugin %s is currently loading, please wait", name)
			return nil, false
		}
	}

	return plugin, true
}

// List 列出所有插件
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
// 如果插件实现了 MetadataProvider 接口，返回详细元数据
// 否则返回只包含名称的基本元数据
func (pm *Manager) GetMetadata(name string) (*Metadata, bool) {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// 检查插件是否实现了 MetadataProvider 接口
	if provider, ok := plugin.(MetadataProvider); ok {
		return provider.Metadata(), true
	}

	// 返回基本元数据
	return &Metadata{
		Name: name,
	}, true
}

// ListWithMetadata 列出所有插件及其元数据
func (pm *Manager) ListWithMetadata() map[string]*Metadata {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*Metadata, len(pm.plugins))
	for name, plugin := range pm.plugins {
		if provider, ok := plugin.(MetadataProvider); ok {
			result[name] = provider.Metadata()
		} else {
			result[name] = &Metadata{
				Name: name,
			}
		}
	}
	return result
}

// GetStatus 获取插件状态
func (pm *Manager) GetStatus(name string) (*Status, error) {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		return nil, errutil.ErrPluginNotFound
	}

	status := &Status{
		Name: name,
	}

	// 如果插件支持状态管理，获取详细状态
	if stateful, ok := plugin.(StatefulPlugin); ok {
		status.State = stateful.GetState()
		status.LoadTime = stateful.GetLoadTime()
		status.LastError = stateful.GetLastError()
		status.Uptime = stateful.GetUptime()
	} else {
		// 不支持状态管理，默认为已加载
		status.State = Loaded
	}

	// 如果插件提供 Matcher 信息
	if matcherProvider, ok := plugin.(MatcherProvider); ok {
		status.MatcherCount = len(matcherProvider.GetMatchers())
	}

	// 如果插件提供元数据
	if provider, ok := plugin.(MetadataProvider); ok {
		status.Metadata = provider.Metadata()
	}

	// 填充 SaveState 是否已实现（v2 PluginInstance）
	if inst, ok := plugin.(*PluginInstance); ok {
		status.HasSaveState = inst.desc.SaveState != nil
	}

	// 填充 EventBus 全局订阅数快照
	pm.mu.RLock()
	bus := pm.eventBus
	pm.mu.RUnlock()
	if bus != nil {
		stats := bus.GetStats()
		status.EventBusSubscriptions = stats.SubscriptionCount
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

// IsLoaded 检查插件是否已加载
func (pm *Manager) IsLoaded(name string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return false
	}

	// 如果插件支持状态管理，检查状态
	if stateful, ok := plugin.(StatefulPlugin); ok {
		return stateful.GetState() == Loaded
	}

	// 不支持状态管理，只要存在就认为已加载
	return true
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

// AsLifecycleComponent 将插件转换为 lifecycle.Component
// 这样插件可以被集成到统一的生命周期管理系统中
//
// 使用示例:
//
//	lifecycleManager := lifecycle.NewManager()
//	plugin := NewMyPlugin()
//	component := pluginManager.AsLifecycleComponent(plugin)
//	lifecycleManager.Register(component)
//
// 注意: 这不会将插件注册到 pluginManager，只是创建适配器
func (pm *Manager) AsLifecycleComponent(plugin Plugin) lifecycle.Component {
	return NewPluginComponent(plugin, pm.coordinator, pm)
}

// RegisterToLifecycle 将所有已注册的插件注册到 lifecycle.Manager
// 这样可以利用 lifecycle 包的统一生命周期管理
//
// 使用示例:
//
//	pluginManager.Register(plugin1)
//	pluginManager.Register(plugin2)
//
//	lifecycleManager := lifecycle.NewManager()
//	pluginManager.RegisterToLifecycle(lifecycleManager)
//
//	lifecycleManager.Start(ctx)
func (pm *Manager) RegisterToLifecycle(lm *lifecycle.Manager) error {
	pm.mu.RLock()
	plugins := make([]Plugin, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		plugins = append(plugins, plugin)
	}
	pm.mu.RUnlock()

	// 按加载顺序注册
	for _, plugin := range plugins {
		component := pm.AsLifecycleComponent(plugin)
		lm.Register(component)
	}

	logger.Infof("[pluginManager] Registered %d plugins to lifecycle manager", len(plugins))
	return nil
}

// GetContainer 获取依赖注入容器（v2 API）
// 允许插件直接访问容器进行高级操作
func (pm *Manager) GetContainer() *Container {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.container
}

// FreezeContainer 冻结依赖注入容器，切换为无锁只读模式。
//
// 改进 3.5：在所有插件通过 RegisterV2/RegisterMultipleV2 加载完成后调用此方法，
// 后续 Get/Has 操作将使用无锁 map，读性能提升 2-3x。
//
// 注意：冻结后不能再调用 Register/Remove，否则会 panic。
//
// 示例：
//
//	manager.RegisterMultipleV2(plugins...)
//	manager.FreezeContainer() // 所有插件加载完成，冻结容器
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
//
// 允许在 Setup 阶段之外（如外部组件）订阅或发布插件事件。
// 使用示例:
//
//	bus := manager.GetEventBus()
//	bus.Subscribe("my-topic", func(data any) { ... })
func (pm *Manager) GetEventBus() EventBus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.eventBus
}
