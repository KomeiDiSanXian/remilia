package plugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// manager_lifecycle.go — 插件生命周期控制器

// lifecycleController 管理插件的生命周期操作：启动、停止、启用、禁用、热重载、
// 生命周期事件通知等。
type lifecycleController struct {
	pm        *Manager
	listeners []LifecycleListener
	// metaGM 元数据 goroutine（notifyDependents 等）。
	//
	// Shutdown 停止它、StartAll 经 ensureMetaGM 重建它，两者可能与管理命令
	// （notifyDependents / 配置变更重试定时器）并发，因此用 atomic.Pointer
	// 承载：读方拿到的旧管理器即使随即被替换也不会读到撕裂的字段。
	metaGM atomic.Pointer[goroutineManager]
}

func newLifecycleController(pm *Manager) *lifecycleController {
	lc := &lifecycleController{pm: pm}
	lc.metaGM.Store(newGoroutineManagerForPlugin("manager"))
	return lc
}

// metaManager 返回当前的元数据 goroutine 管理器（可能为 nil）。
//
// 与 [lifecycleController.ensureMetaGM] 配对使用：返回的管理器可能已被
// Shutdown 停止，调用方按需自行判断 isStopped。
func (lc *lifecycleController) metaManager() *goroutineManager {
	if lc == nil {
		return nil
	}
	return lc.metaGM.Load()
}

// StartAll 启动所有已通过 Register 预注册（Unloaded 状态）的插件。
//
// 通常由 Bot.Start() 自动调用，无需手动调用。
// 若某个插件 Setup 失败，继续尝试其余插件并收集错误，最终返回合并错误。
// 已处于 Loaded 状态的插件会跳过（幂等）。
// ctx 用于控制超时：若 context 在插件 Setup 完成前到期，返回 ctx.Err()。
func (lc *lifecycleController) StartAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

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
			// 失败收尾：回收 Scope 订阅/OnDispose 钩子、goroutine、引擎分组。
			// 实例保留在表中（状态 Error）供 Retry 使用，因此不清容器 key 之外的
			// 引用——容器 key 由插件自身重试时覆盖或由 ForceUnregister 清理。
			inst.cleanupFailedLoad(lc.pm.coordinator, nil)
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
	if ctx == nil {
		ctx = context.Background()
	}

	lc.pm.mu.RLock()
	// 逆序：最后加载的最先卸载
	order := make([]string, len(lc.pm.loadOrder))
	copy(order, lc.pm.loadOrder)
	lc.pm.mu.RUnlock()

	var errs []error
	// skipped 记录本次未被停止的插件（状态不允许卸载），停机后一并告警：
	// 静默跳过会让"停机了但插件还在响应事件"难以定位。
	var skipped []string
	// 从后往前遍历
	for _, name := range slices.Backward(order) {
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
		// 原子转换：与 StartAll 的 tryTransition 对称，防止并发 StopAll 或
		// 与 Reload/Unregister 交叠时对同一实例重复执行 unload。
		if !inst.tryTransition(Loaded, Unloading) && !inst.tryTransition(Disabled, Unloading) {
			state := inst.GetState()
			// 在飞的加载/重载：直接跳过会让停机后旧实例仍持有订阅与 Matcher，
			// 且在飞重载的收尾还会向已停机的 Manager 提交通知。在停机预算内
			// 等它落定后再尝试一次转换。
			if state == Loading || state == Reloading {
				if err := inst.waitForSettledState(ctx); err != nil {
					skipped = append(skipped, name+":"+inst.GetState().String())
					logger.WithField("plugin", name).WithError(err).
						Warn("[PluginManager] StopAll: gave up waiting for in-flight load/reload")
					// 调用方需要知道停机不完整：与 Teardown 失败一致计入错误
					errs = append(errs, fmt.Errorf("plugin %q: wait for in-flight %s: %w",
						name, inst.GetState().String(), err))
					continue
				}
				if !inst.tryTransition(Loaded, Unloading) && !inst.tryTransition(Disabled, Unloading) {
					skipped = append(skipped, name+":"+inst.GetState().String())
					continue
				}
			} else {
				// Error/Unloaded/Unloading：不可在此路径卸载（Error 需 Retry 或
				// ForceUnregister），记录以免停机后"以为都停了"。
				skipped = append(skipped, name+":"+state.String())
				continue
			}
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

	if len(skipped) > 0 {
		logger.WithField("plugins", skipped).
			Warnf("[PluginManager] StopAll: %d plugin(s) were not stopped", len(skipped))
	}

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		// errors.Join 保留错误链：调用方可用 errors.Is 判断原因
		// （如 ctx 到期导致停机不完整）
		return fmt.Errorf("StopAll: %d plugin(s) failed: %v: %w", len(errs), msgs, errors.Join(errs...))
	}
	return nil
}

// Shutdown 停止 Manager 管理的所有内部后台 goroutine（如 notifyDependents 等元数据 goroutine）。
//
// 自带等待上限：元数据 goroutine（依赖通知回调）可能等待插件侧资源，
// 无上限的等待会让停机卡死且没有任何日志。
func (lc *lifecycleController) Shutdown() {
	gm := lc.metaManager()
	if gm == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pluginGoroutineStopTimeout)
	defer cancel()
	if err := gm.stopAndWaitCtx(ctx); err != nil {
		logger.WithError(err).Warn("[PluginManager] Timed out stopping metadata goroutines")
	}
}

// ensureMetaGM 确保元数据 goroutine 管理器可用。
//
// StopAll 结尾调用 Shutdown 停掉 metaGM 后，Manager 仍可继续使用
// （StartAll 注释宣称幂等可重启）。若 metaGM 已被停止，此处重建一个，
// 使 Stop → Start 循环中的元数据任务（notifyDependents 等）恢复正常。
//
// 并发调用（如两次 StartAll）用 CAS 保证只发布一个管理器：落败者创建出的
// 管理器未被任何人持有，直接丢弃。
func (lc *lifecycleController) ensureMetaGM() {
	for {
		cur := lc.metaGM.Load()
		if cur != nil && !cur.isStopped() {
			return
		}
		next := newGoroutineManagerForPlugin("manager")
		if lc.metaGM.CompareAndSwap(cur, next) {
			return
		}
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

// RemoveListener 移除生命周期监听器。
//
// 比较通过 sameListener 进行：接口值直接 `==` 在动态类型不可比较时 panic，
// 这里按动态类型分派（指针比地址、可比较类型用 ==、函数/映射/切片等
// 不可比较类型无法判定同一性，跳过并告警）。
func (lc *lifecycleController) RemoveListener(listener LifecycleListener) {
	lc.pm.mu.Lock()
	defer lc.pm.mu.Unlock()
	newListeners := make([]LifecycleListener, 0, len(lc.listeners))
	for _, l := range lc.listeners {
		if !sameListener(l, listener) {
			newListeners = append(newListeners, l)
		}
	}
	lc.listeners = newListeners
}

// sameListener 判定两个监听器是否为同一对象（不 panic）。
func sameListener(a, b LifecycleListener) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if va.Type() != vb.Type() {
		return false
	}
	switch va.Kind() {
	case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		return va.Pointer() == vb.Pointer()
	case reflect.Func, reflect.Map, reflect.Slice:
		// 不可比较的动态类型：无法判定同一性（不允许用 panic 换取移除），
		// 视为不同对象，监听器保留。
		logger.Warn("[PluginManager] RemoveListener: listener has a non-comparable dynamic type, kept in place")
		return false
	default:
		if va.Comparable() {
			return a == b
		}
		logger.Warn("[PluginManager] RemoveListener: listener has a non-comparable dynamic type, kept in place")
		return false
	}
}

// --- 生命周期事件通知 ---

// pluginLifecyclePublishTimeout 是框架内部生命周期通知的发布预算。
//
// 通知在业务调用链内同步发布（注册/卸载/重载/失败），而 Publish 在 worker
// 池满时会一直等待槽位——订阅者互相等待即把管理命令整体挂死。给内部通知
// 一个有界预算：超时丢弃并告警，管理操作照常返回。
const pluginLifecyclePublishTimeout = 2 * time.Second

// publishLifecycle 发布框架内部生命周期通知（有界等待，超时丢弃并告警）。
func publishLifecycle(bus EventBus, topic, pluginName string) {
	if bus == nil {
		return
	}
	if err := PublishWithTimeout(bus, topic, pluginName, pluginLifecyclePublishTimeout); err != nil {
		logger.WithFields(logger.Fields{
			"topic":      topic,
			"plugin":     pluginName,
			"timeout":    pluginLifecyclePublishTimeout,
			"subscriber": "framework",
		}).Warn("[PluginManager] Lifecycle notification dropped: event bus saturated")
	}
}

func (lc *lifecycleController) notifyLoaded(name string) {
	lc.pm.mu.RLock()
	listeners := make([]LifecycleListener, len(lc.listeners))
	copy(listeners, lc.listeners)
	bus := lc.pm.eventBus
	lc.pm.mu.RUnlock()

	for _, listener := range listeners {
		safeNotify(name, "OnPluginLoaded", func() { listener.OnPluginLoaded(name) })
	}
	publishLifecycle(bus, "plugin.loaded", name)
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
	publishLifecycle(bus, "plugin.unloaded", name)
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
	publishLifecycle(bus, "plugin.reloaded", name)
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
