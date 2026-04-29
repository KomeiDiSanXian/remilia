package plugin

// manager_lifecycle.go — 插件管理器生命周期（StartAll / StopAll / Shutdown）

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// StartAll 启动所有已通过 Register 预注册（Unloaded 状态）的插件。
//
// 通常由 Bot.Start() 自动调用，无需手动调用。
// 若某个插件 Setup 失败，继续尝试其余插件并收集错误，最终返回合并错误。
// 已处于 Loaded 状态的插件会跳过（幂等）。
func (pm *Manager) StartAll() error {
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
		if err := inst.load(); err != nil {
			logger.WithError(err).Errorf("[PluginManager] StartAll: plugin %s failed to start", name)
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
func (pm *Manager) StopAll() error {
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
			logger.WithError(err).Errorf("[PluginManager] StopAll: plugin %s failed to stop", name)
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
// StopAll() 内部会自动调用此方法。如果调用方未调用 StopAll()（例如仅做了 Unregister 操作），
// 应在不再使用 Manager 时显式调用 Shutdown() 或 Close() 以防止 goroutine 泄漏。
//
// 注意：此方法不卸载插件（如需卸载，请先调用 StopAll 或 UnregisterAll）。
func (pm *Manager) Shutdown() {
	if pm.metaGM != nil {
		pm.metaGM.stopAndWait()
	}
}
