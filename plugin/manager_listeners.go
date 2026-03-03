package plugin

// manager_listeners.go — 插件生命周期监听器通知逻辑

import "github.com/KomeiDiSanXian/remilia/infra/logger"

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
