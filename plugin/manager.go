package plugin

import (
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/sirupsen/logrus"
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
	mu          sync.RWMutex
}

// NewManager 创建插件管理器
func NewManager(coordinator *engine.Engine) *Manager {
	return &Manager{
		plugins:     make(map[string]Plugin),
		coordinator: coordinator,
		listeners:   make([]LifecycleListener, 0),
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
	for i, l := range pm.listeners {
		// 使用指针比较
		if l == listener {
			pm.listeners = append(pm.listeners[:i], pm.listeners[i+1:]...)
			return
		}
	}
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

// Register 注册插件，返回错误信息
func (pm *Manager) Register(plugin Plugin) error {
	name := plugin.Name()
	pm.mu.Lock()

	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logrus.Warnf("[PluginManager] Plugin %s already registered", name)
		return ErrPluginAlreadyExists
	}

	// 加载插件
	if err := plugin.Load(pm.coordinator); err != nil {
		pm.mu.Unlock()
		logrus.WithError(err).Errorf("[PluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", err) // 通知监听器错误
		return err
	}

	pm.plugins[name] = plugin
	pm.mu.Unlock()

	logrus.Infof("[PluginManager] Plugin %s registered", name)
	pm.notifyLoaded(name) // 通知监听器（在锁外）
	return nil
}

// checkDependents 返回当前已注册插件中依赖指定插件的插件名称列表
func (pm *Manager) checkDependents(name string) []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	dependents := make([]string, 0)
	for pluginName, plugin := range pm.plugins {
		if pluginName == name {
			continue
		}
		for _, dep := range plugin.Dependencies() {
			if dep == name {
				dependents = append(dependents, pluginName)
				break
			}
		}
	}
	return dependents
}

// Unregister 注销插件，返回错误信息
// 如果当前仍有其他插件依赖该插件，则返回错误并拒绝卸载。
func (pm *Manager) Unregister(name string) error {
	// 先检查是否有其他插件依赖该插件
	if dependents := pm.checkDependents(name); len(dependents) > 0 {
		return fmt.Errorf("cannot unregister plugin %s: required by %v", name, dependents)
	}

	pm.mu.Lock()

	plugin, exists := pm.plugins[name]
	if !exists {
		pm.mu.Unlock()
		logrus.Warnf("[PluginManager] Plugin %s not found", name)
		return ErrPluginNotFound
	}

	// 卸载插件
	if err := plugin.Unload(pm.coordinator); err != nil {
		pm.mu.Unlock()
		logrus.WithError(err).Errorf("[PluginManager] Failed to unload plugin %s", name)
		pm.notifyError(name, "unload", err) // 通知监听器错误
		return err
	}

	delete(pm.plugins, name)
	pm.mu.Unlock()

	logrus.Infof("[PluginManager] Plugin %s unregistered", name)
	pm.notifyUnloaded(name) // 通知监听器（在锁外）
	return nil
}

// UnregisterCascade 级联卸载指定插件及所有依赖它的插件
// 注意：调用方需自行确认这是期望的行为。
func (pm *Manager) UnregisterCascade(name string) error {
	// 先递归卸载所有直接依赖 name 的插件
	dependents := pm.checkDependents(name)
	for _, dep := range dependents {
		if err := pm.UnregisterCascade(dep); err != nil {
			return err
		}
	}

	// 再卸载自身（此时不应再有依赖者）
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
		logrus.Warnf("[PluginManager] Plugin %s not found", name)
		return ErrPluginNotFound
	}

	logrus.Infof("[PluginManager] Reloading plugin %s", name)

	// 调用插件的 Reload 方法，传递 coordinator
	if err := plugin.Reload(pm.coordinator); err != nil {
		logrus.WithError(err).Errorf("[PluginManager] Failed to reload plugin %s", name)
		pm.notifyError(name, "reload", err) // 通知监听器错误
		// Reload 失败时不删除插件，因为无法判断插件是否实现了原子性重载
		// 调用方可以根据需要调用 Unregister 来删除插件
		return err
	}

	logrus.Infof("[PluginManager] Plugin %s reloaded successfully", name)
	pm.notifyReloaded(name) // 通知监听器
	return nil
}

// Get 获取插件
func (pm *Manager) Get(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	return plugin, exists
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

// RegisterWithDependencies 注册插件并处理依赖关系（v0.7.1 新增）
// 自动解析依赖顺序并按正确顺序加载插件
// 如果检测到循环依赖或缺少依赖会返回错误
func (pm *Manager) RegisterWithDependencies(plugins []Plugin) error {
	// 构建插件映射
	pluginMap := make(map[string]Plugin)
	for _, p := range plugins {
		pluginMap[p.Name()] = p
	}

	// 检查所有依赖是否存在
	for _, p := range plugins {
		for _, dep := range p.Dependencies() {
			if _, exists := pluginMap[dep]; !exists {
				// 检查是否已经注册
				pm.mu.RLock()
				_, registered := pm.plugins[dep]
				pm.mu.RUnlock()

				if !registered {
					return &DependencyError{
						Plugin:     p.Name(),
						Dependency: dep,
						Err:        ErrDependencyNotFound,
					}
				}
			}
		}
	}

	// 拓扑排序解析依赖顺序
	sorted, err := pm.topologicalSort(plugins)
	if err != nil {
		return err
	}

	// 按依赖顺序加载插件
	for _, p := range sorted {
		if err := pm.Register(p); err != nil {
			// 如果是已存在错误，跳过
			if errors.Is(err, ErrPluginAlreadyExists) {
				continue
			}
			return err
		}
	}

	return nil
}

// topologicalSort 对插件进行拓扑排序
// 使用 DFS 算法实现，检测循环依赖
func (pm *Manager) topologicalSort(plugins []Plugin) ([]Plugin, error) {
	// 构建插件映射
	pluginMap := make(map[string]Plugin)
	for _, p := range plugins {
		pluginMap[p.Name()] = p
	}

	// 访问状态：0=未访问，1=访问中，2=已完成
	visited := make(map[string]int)
	result := make([]Plugin, 0, len(plugins))

	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		state := visited[name]

		if state == 2 {
			// 已经完成，跳过
			return nil
		}

		if state == 1 {
			// 访问中，发现循环依赖
			cycle := append(path, name)
			return &CircularDependencyError{
				Cycle: cycle,
			}
		}

		// 标记为访问中
		visited[name] = 1
		path = append(path, name)

		// 获取插件
		plugin, exists := pluginMap[name]
		if !exists {
			// 检查是否已注册
			pm.mu.RLock()
			_, registered := pm.plugins[name]
			pm.mu.RUnlock()

			if !registered {
				return &DependencyError{
					Plugin:     name,
					Dependency: name,
					Err:        ErrPluginNotFound,
				}
			}
			// 已注册的插件不需要再次加载
			visited[name] = 2
			return nil
		}

		// 递归访问依赖
		for _, dep := range plugin.Dependencies() {
			if err := visit(dep, path); err != nil {
				return err
			}
		}

		// 标记为已完成
		visited[name] = 2
		result = append(result, plugin)

		return nil
	}

	// 访问所有插件
	for _, p := range plugins {
		if err := visit(p.Name(), []string{}); err != nil {
			return nil, err
		}
	}

	return result, nil
}
