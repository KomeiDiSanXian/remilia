package plugin

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// manager_lifecycle.go — 插件生命周期控制器

// lifecycleController 管理插件的生命周期操作：启动、停止、启用、禁用、热重载、
// 生命周期事件通知等。
type lifecycleController struct {
	pm        *Manager
	listeners []LifecycleListener
	metaGM    *goroutineManager // 元数据 goroutine（notifyDependents 等）
}

func newLifecycleController(pm *Manager) *lifecycleController {
	return &lifecycleController{
		pm:     pm,
		metaGM: newGoroutineManagerForPlugin("manager"),
	}
}

// StartAll 启动所有已通过 Register 预注册（Unloaded 状态）的插件。
//
// 通常由 Bot.Start() 自动调用，无需手动调用。
// 若某个插件 Setup 失败，继续尝试其余插件并收集错误，最终返回合并错误。
// 已处于 Loaded 状态的插件会跳过（幂等）。
// ctx 用于控制超时：若 context 在插件 Setup 完成前到期，返回 ctx.Err()。
func (lc *lifecycleController) StartAll(ctx context.Context) error {
	// StopAll 会停掉 metaGM；支持 Stop → Start 循环（StartAll 文档宣称幂等）
	lc.ensureMetaGM()

	lc.pm.mu.RLock()
	names := make([]string, len(lc.pm.loadOrder))
	copy(names, lc.pm.loadOrder)
	lc.pm.mu.RUnlock()

	var errs []error
	for _, name := range names {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		lc.pm.mu.RLock()
		inst, exists := lc.pm.plugins[name]
		lc.pm.mu.RUnlock()
		if !exists {
			continue
		}
		// 仅启动 Unloaded 状态的插件（与文档一致）：
		//   - Loaded  已在运行，跳过（幂等）
		//   - Disabled 由 Enable 恢复，重复 load 会二次 Setup、matcher 翻倍
		//   - Error    由 Retry 处理
		//   - Loading/Unloading 正在变更中，跳过
		// tryTransition 原子完成"检查 + 置 Loading"：并发 StartAll 或
		// 与 UnloadLoad 重载的 Unloaded 窗口交叠时，只有一个调用方胜出，
		// 杜绝二次 Setup 导致的 matcher 翻倍。
		if !inst.tryTransition(Unloaded, Loading) {
			if st := inst.GetState(); st != Loaded && st != Unloaded {
				logger.Debugf("[PluginManager] StartAll: skip plugin %s in state %s", name, st)
			}
			continue
		}
		if err := loadWithRegisterBatch(lc.pm.coordinator, func() error { return inst.load(ctx) }); err != nil {
			logger.WithError(err).Errorf("[PluginManager] StartAll: plugin %s failed to start", name)
			lc.notifyError(name, "start", err)
			errs = append(errs, fmt.Errorf("plugin %q: %w", name, err))
		} else {
			inst.SetState(Loaded)
			lc.notifyLoaded(name)
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
// ctx 用于控制超时：若 context 在插件 Teardown 完成前到期，返回 ctx.Err()。
func (lc *lifecycleController) StopAll(ctx context.Context) error {
	lc.pm.mu.RLock()
	// 逆序：最后加载的最先卸载
	order := make([]string, len(lc.pm.loadOrder))
	copy(order, lc.pm.loadOrder)
	lc.pm.mu.RUnlock()

	var errs []error
	// 从后往前遍历
	for i := len(order) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		name := order[i]
		lc.pm.mu.RLock()
		inst, exists := lc.pm.plugins[name]
		lc.pm.mu.RUnlock()
		if !exists {
			continue
		}
		// 原子转换：与 StartAll 的 tryTransition 对称，防止并发 StopAll 或
		// 与 Reload/Unregister 交叠时对同一实例重复执行 unload。
		if !inst.tryTransition(Loaded, Unloading) && !inst.tryTransition(Disabled, Unloading) {
			continue
		}
		if err := inst.unload(ctx, lc.pm.coordinator); err != nil {
			logger.WithError(err).Errorf("[PluginManager] StopAll: plugin %s failed to stop", name)
			lc.notifyError(name, "stop", err)
			errs = append(errs, fmt.Errorf("plugin %q: %w", name, err))
		} else {
			lc.notifyUnloaded(name)
		}
	}

	// 等待蓝绿重载中异步清理的旧实例完成（受 ctx 超时约束）
	lc.pm.stats.drainDrainingInstances(ctx)

	// 停止 Manager 自身的后台 goroutine
	lc.Shutdown()

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("StopAll: %d plugin(s) failed: %v", len(errs), msgs)
	}
	return nil
}

// Shutdown 停止 Manager 管理的所有内部后台 goroutine（如 notifyDependents 等元数据 goroutine）。
func (lc *lifecycleController) Shutdown() {
	if lc.metaGM != nil {
		lc.metaGM.stopAndWait()
	}
}

// ensureMetaGM 确保元数据 goroutine 管理器可用。
//
// StopAll 结尾调用 Shutdown 停掉 metaGM 后，Manager 仍可继续使用
// （StartAll 注释宣称幂等可重启）。若 metaGM 已被停止，此处重建一个，
// 使 Stop → Start 循环中的元数据任务（notifyDependents 等）恢复正常。
func (lc *lifecycleController) ensureMetaGM() {
	if lc.metaGM == nil || lc.metaGM.isStopped() {
		lc.metaGM = newGoroutineManagerForPlugin("manager")
	}
}

// Disable 禁用插件（暂停事件响应，但保持注册状态）。
func (lc *lifecycleController) Disable(name string) error {
	lc.pm.mu.Lock()
	defer lc.pm.mu.Unlock()

	if lc.pm.coordinator == nil {
		return fmt.Errorf("cannot disable plugin %q: manager has no engine coordinator; Register Engine before use", name)
	}

	inst, exists := lc.pm.plugins[name]
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

	lc.pm.coordinator.DisableGroup(name)
	inst.SetState(Disabled)
	logger.Infof("[PluginManager] Plugin %s disabled (matchers paused, container intact)", name)
	return nil
}

// Enable 启用已禁用的插件（恢复事件响应）。
func (lc *lifecycleController) Enable(name string) error {
	lc.pm.mu.Lock()
	defer lc.pm.mu.Unlock()

	if lc.pm.coordinator == nil {
		return fmt.Errorf("cannot enable plugin %q: manager has no engine coordinator; Register Engine before use", name)
	}

	inst, exists := lc.pm.plugins[name]
	if !exists {
		return errutil.ErrPluginNotFound
	}
	if inst.GetState() != Disabled {
		logger.Warnf("[PluginManager] Plugin %s is not disabled (state: %s)", name, inst.GetState())
		return nil
	}

	lc.pm.coordinator.EnableGroup(name)
	inst.SetState(Loaded)
	logger.Infof("[PluginManager] Plugin %s enabled (matchers resumed)", name)
	return nil
}

// IsDisabled 检查插件是否被禁用
func (lc *lifecycleController) IsDisabled(name string) bool {
	lc.pm.mu.RLock()
	inst, exists := lc.pm.plugins[name]
	lc.pm.mu.RUnlock()
	if !exists {
		return false
	}
	return inst.GetState() == Disabled
}

// Reload 重新加载插件（热重载）
func (lc *lifecycleController) Reload(ctx context.Context, name string) error {
	lc.pm.mu.RLock()
	inst, exists := lc.pm.plugins[name]
	lc.pm.mu.RUnlock()

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

	if err := inst.reload(ctx, lc.pm.coordinator); err != nil {
		logger.WithError(err).Errorf("[PluginManager] Failed to reload plugin %s", name)
		lc.notifyError(name, "reload", err)
		return err
	}

	logger.Infof("[PluginManager] Plugin %s reloaded successfully", name)
	lc.notifyReloaded(name)

	// 通知所有依赖了 name 插件的其他插件
	lc.pm.notifyDependents(name)
	return nil
}

// Retry 重新尝试加载处于 Error 状态的插件。
func (lc *lifecycleController) Retry(name string, desc *Descriptor) error {
	lc.pm.mu.Lock()
	inst, exists := lc.pm.plugins[name]
	if !exists {
		lc.pm.mu.Unlock()
		return errutil.ErrPluginNotFound
	}
	if inst.GetState() != Error {
		lc.pm.mu.Unlock()
		return fmt.Errorf("plugin %s is not in Error state (state: %s)", name, inst.GetState())
	}
	lc.pm.mu.Unlock()

	// 强制卸载
	if err := lc.pm.ForceUnregister(name); err != nil {
		return fmt.Errorf("retry %s: force unregister failed: %w", name, err)
	}

	// 重新注册
	return lc.pm.Register(desc)
}

// AddListener 添加生命周期监听器
func (lc *lifecycleController) AddListener(listener LifecycleListener) {
	lc.pm.mu.Lock()
	defer lc.pm.mu.Unlock()
	lc.listeners = append(lc.listeners, listener)
}

// RemoveListener 移除生命周期监听器
func (lc *lifecycleController) RemoveListener(listener LifecycleListener) {
	lc.pm.mu.Lock()
	defer lc.pm.mu.Unlock()
	newListeners := make([]LifecycleListener, 0, len(lc.listeners))
	for _, l := range lc.listeners {
		if l != listener {
			newListeners = append(newListeners, l)
		}
	}
	lc.listeners = newListeners
}

// --- 生命周期事件通知 ---

func (lc *lifecycleController) notifyLoaded(name string) {
	lc.pm.mu.RLock()
	listeners := make([]LifecycleListener, len(lc.listeners))
	copy(listeners, lc.listeners)
	bus := lc.pm.eventBus
	lc.pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginLoaded", func() { listener.OnPluginLoaded(name) })
	}
	if bus != nil {
		_ = bus.Publish("plugin.loaded", name)
	}
}

func (lc *lifecycleController) notifyUnloaded(name string) {
	lc.pm.mu.RLock()
	listeners := make([]LifecycleListener, len(lc.listeners))
	copy(listeners, lc.listeners)
	bus := lc.pm.eventBus
	lc.pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginUnloaded", func() { listener.OnPluginUnloaded(name) })
	}
	if bus != nil {
		_ = bus.Publish("plugin.unloaded", name)
	}
}

func (lc *lifecycleController) notifyReloaded(name string) {
	lc.pm.mu.RLock()
	listeners := make([]LifecycleListener, len(lc.listeners))
	copy(listeners, lc.listeners)
	bus := lc.pm.eventBus
	lc.pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginReloaded", func() { listener.OnPluginReloaded(name) })
	}
	if bus != nil {
		_ = bus.Publish("plugin.reloaded", name)
	}
}

func (lc *lifecycleController) notifyError(name string, operation string, err error) {
	lc.pm.mu.RLock()
	listeners := make([]LifecycleListener, len(lc.listeners))
	copy(listeners, lc.listeners)
	lc.pm.mu.RUnlock()

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
			}).Error("[PluginManager] LifecycleListener panic recovered")
		}
	}()
	fn()
}
