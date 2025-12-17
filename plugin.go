package remilia

import (
	"errors"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

// Plugin 相关错误
var (
	ErrPluginAlreadyExists = errors.New("plugin already exists")
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrCircularDependency  = errors.New("circular dependency detected")
	ErrDependencyNotFound  = errors.New("dependency not found")
)

// Plugin 插件接口
type Plugin interface {
	// Name 返回插件名称
	Name() string
	// Load 加载插件到引擎，返回错误信息
	Load(engine *Engine) error
	// Unload 卸载插件，返回错误信息
	Unload(engine *Engine) error
	// Reload 原子性重载插件（策略 B）：
	//  - 成功时，用新的内部状态替换旧状态；
	//  - 失败时，不改变原有状态（调用方可根据错误自行处理）。
	// engine 参数用于重新注册 handler 等操作
	Reload(engine *Engine) error
	// Dependencies 返回插件依赖列表（v0.7.1 新增）
	// 返回的插件名称列表表示此插件依赖的其他插件
	// 插件管理器会确保依赖的插件先于当前插件加载
	Dependencies() []string
}

// BasePlugin 基础插件结构
type BasePlugin struct {
	name     string
	matchers []*Matcher
	mu       sync.RWMutex
}

// NewBasePlugin 创建基础插件
func NewBasePlugin(name string) *BasePlugin {
	return &BasePlugin{
		name:     name,
		matchers: make([]*Matcher, 0),
	}
}

// Name 返回插件名称
func (p *BasePlugin) Name() string {
	return p.name
}

// AddMatcher 添加匹配器到插件（线程安全）
func (p *BasePlugin) AddMatcher(matcher *Matcher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 标记来源为该插件
	matcher.Source = "plugin:" + p.name
	p.matchers = append(p.matchers, matcher)
}

// GetMatchers 获取所有匹配器（线程安全）
func (p *BasePlugin) GetMatchers() []*Matcher {
	p.mu.RLock()
	defer p.mu.RUnlock()
	matchers := make([]*Matcher, len(p.matchers))
	copy(matchers, p.matchers)
	return matchers
}

// Load 加载插件（子类需要重写实现具体逻辑）
func (p *BasePlugin) Load(_ *Engine) error {
	// 默认实现为空，子类重写
	return nil
}

// Unload 卸载插件，清理所有匹配器（在锁外删除匹配器，避免锁反转）
func (p *BasePlugin) Unload(_ *Engine) error {
	p.mu.Lock()
	matchersToDelete := make([]*Matcher, len(p.matchers))
	copy(matchersToDelete, p.matchers)
	p.matchers = make([]*Matcher, 0)
	p.mu.Unlock()

	for _, matcher := range matchersToDelete {
		if matcher != nil {
			matcher.Delete()
		}
	}
	return nil
}

// Reload 的默认实现：原子性重载插件（适配 COW Engine）
//
// COW Engine 下的实现策略：
// 1. 保存插件的 matchers 快照和 Engine 状态快照
// 2. 尝试 Unload（清空 matchers 并删除）
// 3. 尝试 Load（创建新的 matchers）
// 4. 如果 Load 失败，通过 Engine 的 COW 机制回滚
//
// 优势：
//   - 利用 Engine 的 COW 特性，简化回滚逻辑
//   - 回滚更安全，不会出现状态不一致
func (p *BasePlugin) Reload(engine *Engine) error {
	if engine == nil {
		return fmt.Errorf("engine is nil")
	}

	// 1. 保存插件的 matchers 快照
	p.mu.Lock()
	oldMatchers := make([]*Matcher, len(p.matchers))
	copy(oldMatchers, p.matchers)
	p.mu.Unlock()

	// 2. 保存 Engine 状态快照（COW 模式下直接保存引用）
	oldEngineState := engine.state.Load().(*engineState)

	// 3. 尝试卸载（这会清空 p.matchers 并删除 matchers）
	if err := p.Unload(engine); err != nil {
		// Unload 失败，状态未改变
		return fmt.Errorf("unload failed during reload: %w", err)
	}

	// 4. 尝试加载新状态
	if err := p.Load(engine); err != nil {
		// Load 失败，需要回滚
		logrus.WithError(err).Warn("[Plugin] Load failed during reload, rolling back")

		// 恢复插件的 matchers 列表
		p.mu.Lock()
		p.matchers = oldMatchers
		p.mu.Unlock()

		// 回滚 Engine 状态（直接恢复旧的不可变状态）
		engine.writeMu.Lock()
		engine.state.Store(oldEngineState)
		engine.writeMu.Unlock()

		// 恢复所有 matcher 的 deleted 状态并重建中间件链
		for _, matcher := range oldMatchers {
			if matcher != nil {
				matcher.mu.Lock()
				matcher.deleted = false
				matcher.mu.Unlock()

				// 重建中间件链
				engine.rebuildMatcherChainCOW(matcher)
			}
		}

		return fmt.Errorf("load failed during reload, rolled back to previous state: %w", err)
	}

	// 5. 成功，旧的 matchers 已经被 Unload 删除，不需要额外清理
	logrus.WithField("plugin", p.name).Info("[Plugin] Reload successful")
	return nil
}

// Dependencies 返回插件依赖列表（默认无依赖）
// 子类可以重写此方法来声明依赖
func (p *BasePlugin) Dependencies() []string {
	return []string{}
}

// Use 为当前插件注册中间件（作用于该插件的所有匹配器）
func (p *BasePlugin) Use(engine *Engine, mw ...HandlerMiddleware) {
	if engine == nil {
		return
	}
	engine.UseForPlugin(p.name, mw...)
}

// PluginLifecycleListener 插件生命周期监听器接口
type PluginLifecycleListener interface {
	// OnPluginLoaded 插件加载完成时调用
	OnPluginLoaded(name string)
	// OnPluginUnloaded 插件卸载完成时调用
	OnPluginUnloaded(name string)
	// OnPluginReloaded 插件重载完成时调用
	OnPluginReloaded(name string)
	// OnPluginError 插件操作发生错误时调用
	OnPluginError(name string, operation string, err error)
}

// PluginManager 插件管理器
type PluginManager struct {
	plugins   map[string]Plugin
	engine    *Engine
	listeners []PluginLifecycleListener // 生命周期监听器列表
	mu        sync.RWMutex
}

// NewPluginManager 创建插件管理器
func NewPluginManager(engine *Engine) *PluginManager {
	return &PluginManager{
		plugins:   make(map[string]Plugin),
		engine:    engine,
		listeners: make([]PluginLifecycleListener, 0),
	}
}

// AddListener 添加生命周期监听器
func (pm *PluginManager) AddListener(listener PluginLifecycleListener) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.listeners = append(pm.listeners, listener)
}

// RemoveListener 移除生命周期监听器
func (pm *PluginManager) RemoveListener(listener PluginLifecycleListener) {
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
func (pm *PluginManager) notifyLoaded(name string) {
	pm.mu.RLock()
	listeners := make([]PluginLifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginLoaded(name)
	}
}

// notifyUnloaded 通知监听器插件已卸载
func (pm *PluginManager) notifyUnloaded(name string) {
	pm.mu.RLock()
	listeners := make([]PluginLifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginUnloaded(name)
	}
}

// notifyReloaded 通知监听器插件已重载
func (pm *PluginManager) notifyReloaded(name string) {
	pm.mu.RLock()
	listeners := make([]PluginLifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginReloaded(name)
	}
}

// notifyError 通知监听器插件操作发生错误
func (pm *PluginManager) notifyError(name string, operation string, err error) {
	pm.mu.RLock()
	listeners := make([]PluginLifecycleListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mu.RUnlock()

	for _, listener := range listeners {
		listener.OnPluginError(name, operation, err)
	}
}

// Register 注册插件，返回错误信息
func (pm *PluginManager) Register(plugin Plugin) error {
	name := plugin.Name()
	pm.mu.Lock()

	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logrus.Warnf("[PluginManager] Plugin %s already registered", name)
		return ErrPluginAlreadyExists
	}

	// 加载插件
	if err := plugin.Load(pm.engine); err != nil {
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
func (pm *PluginManager) checkDependents(name string) []string {
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
func (pm *PluginManager) Unregister(name string) error {
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
	if err := plugin.Unload(pm.engine); err != nil {
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
func (pm *PluginManager) UnregisterCascade(name string) error {
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
func (pm *PluginManager) Reload(name string) error {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()

	if !exists {
		logrus.Warnf("[PluginManager] Plugin %s not found", name)
		return ErrPluginNotFound
	}

	logrus.Infof("[PluginManager] Reloading plugin %s", name)

	// 调用插件的 Reload 方法，传递 engine
	if err := plugin.Reload(pm.engine); err != nil {
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
func (pm *PluginManager) Get(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	return plugin, exists
}

// List 列出所有插件
func (pm *PluginManager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

// Stats 返回插件与全局匹配器统计（读锁）
func (pm *PluginManager) Stats() MatcherStats {
	return pm.engine.GetMatcherStats()
}

// RegisterWithDependencies 注册插件并处理依赖关系（v0.7.1 新增）
// 自动解析依赖顺序并按正确顺序加载插件
// 如果检测到循环依赖或缺少依赖会返回错误
func (pm *PluginManager) RegisterWithDependencies(plugins []Plugin) error {
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
func (pm *PluginManager) topologicalSort(plugins []Plugin) ([]Plugin, error) {
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

// DependencyError 依赖错误
type DependencyError struct {
	Plugin     string
	Dependency string
	Err        error
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("plugin %s: dependency %s not found", e.Plugin, e.Dependency)
}

func (e *DependencyError) Unwrap() error {
	return e.Err
}

// CircularDependencyError 循环依赖错误
type CircularDependencyError struct {
	Cycle []string
}

func (e *CircularDependencyError) Error() string {
	return fmt.Sprintf("circular dependency detected: %v", e.Cycle)
}

func (e *CircularDependencyError) Unwrap() error {
	return ErrCircularDependency
}
