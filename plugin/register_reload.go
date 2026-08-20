package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// reload.go — 插件热重载逻辑：三种重载策略实现

// reload 重载插件（实现 pluginInternal）
// ctx 用于控制超时：若 context 到期，跳过卸载/加载步骤并返回 ctx.Err()。
func (pi *Instance) reload(ctx context.Context, coordinator engine.PluginCoordinator) error {
	pi.mu.Lock()
	// 与状态置位同锁完成检查，防止并发 Reload/Unregister 交叠执行双重热重载
	switch pi.state {
	case Reloading:
		pi.mu.Unlock()
		return fmt.Errorf("plugin %q is already reloading", pi.desc.Name)
	case Unloading:
		pi.mu.Unlock()
		return fmt.Errorf("plugin %q is being unloaded", pi.desc.Name)
	}
	oldContext := pi.setupContext
	pi.state = Reloading
	oldVersion := pi.loadedVer // 保存旧版本号，供 MigrateState 使用
	pi.mu.Unlock()

	adv := pi.desc.effectiveAdvanced()
	var savedState any
	if adv.SaveState != nil {
		var saveErr error
		savedState, saveErr = adv.SaveState()
		if saveErr != nil {
			logger.WithError(saveErr).Warn("[plugin] Failed to save state before reload")
		}
	}

	// 重新创建 SetupContext（保留 Admin 视图）。
	// goroutineMgr 必须继承旧实例的 GM：InPlace 路径不经过 load()，
	// 而 load() 是唯一为 setupContext.goroutineMgr 赋值的地方——若此处不赋值，
	// adv.Reload 内及此后保留 ctx 的 Spawn/NewTaskGroup/RegisterCron 全部静默
	// no-op（RegisterCron 还假装成功返回 nil），InPlace 插件的后台任务会整体丢失。
	// UnloadLoad/BlueGreen 路径随后走 load()，会覆盖为新的 GM，此处赋值无副作用。
	pi.mu.RLock()
	inheritedGM := pi.goroutineMgr
	pi.mu.RUnlock()
	newContext := &SetupContext{
		Reg:      newLiveRegistryWriter(oldContext.eng, oldContext.pluginName, oldContext.instance),
		Log:      newPluginLogger(oldContext.pluginName),
		Info:     oldContext.Info,
		Admin:    oldContext.Admin,
		Config:   oldContext.Config,
		EventBus: oldContext.EventBus,
		setupContextInternal: setupContextInternal{
			container:        oldContext.container,
			pluginName:       oldContext.pluginName,
			instance:         oldContext.instance,
			autoTrackEnabled: true,
			eng:              oldContext.eng,
			goroutineMgr:     inheritedGM,
		},
	}

	// 注意：此处不能提前替换 pi.setupContext。
	// UnloadLoad 路径依赖 unload() 去 Dispose 旧 rootScope（EventBus 订阅、
	// OnDispose 钩子）；若先替换成 rootScope 为 nil 的新 context，旧 scope
	// 将永远无人清理——事件被旧 handler 重复处理、清理钩子不执行（历史 bug）。
	// 各策略在自己合适的时机完成 context 切换与旧 scope 清理。
	unloadThenLoad := func() error {
		// pi.setupContext 仍是旧 context，unload Step 0 会 Dispose 旧 rootScope
		if err := pi.unload(ctx, coordinator); err != nil {
			return err
		}
		pi.mu.Lock()
		pi.setupContext = newContext
		pi.mu.Unlock()
		return loadWithRegisterBatch(coordinator, func() error { return pi.load(ctx) })
	}

	// 状态迁移：若版本号变化且设置了 MigrateState，在 RestoreState 前迁移旧状态。
	maybeMigrateThenRestore := func() {
		if savedState == nil || adv.RestoreState == nil {
			return
		}
		newVersion := pi.desc.Version
		if newVersion != oldVersion && adv.MigrateState != nil {
			var migrateErr error
			migrated, migrateErr := adv.MigrateState(savedState, oldVersion, newVersion)
			if migrateErr != nil {
				logger.WithError(migrateErr).Warnf("[plugin] %s: state migration from v%s to v%s failed, using raw saved state",
					pi.desc.Name, oldVersion, newVersion)
			} else {
				savedState = migrated
			}
		}
		if err := adv.RestoreState(savedState); err != nil {
			logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
		}
	}

	switch adv.Strategy {
	case ReloadInPlace:
		if adv.Reload == nil {
			// ReloadInPlace 要求提供 Reload 函数；未提供时降级并给出明确警告
			logger.Warnf("[plugin] %s: ReloadInPlace specified but Advanced.Reload is nil, falling back to ReloadUnloadLoad", pi.desc.Name)
			if err := unloadThenLoad(); err != nil {
				return err
			}
			maybeMigrateThenRestore()
			return nil
		}
		// 原地重载不卸载旧实例：新 context 继承旧 rootScope（及已导出 key 记录），
		// 保证订阅/清理钩子仍归本插件生命周期管理，后续 Unregister 时能被 Dispose。
		newContext.rootScope = oldContext.rootScope
		newContext.exportedNames = oldContext.exportedKeysSnapshot()
		pi.mu.Lock()
		pi.setupContext = newContext
		pi.mu.Unlock()
		if err := adv.Reload(newContext); err != nil {
			pi.mu.Lock()
			pi.state = Error
			pi.lastError = err
			pi.mu.Unlock()
			return err
		}
		pi.mu.Lock()
		pi.state = Loaded
		pi.loadTime = time.Now()
		pi.lastError = nil
		pi.loadedVer = pi.desc.Version
		pi.mu.Unlock()
		// InPlace 路径不经过 load()，须在此补 RestoreState/MigrateState
		//（此前 SaveState 的成果被白白丢弃，MigrateState 永不生效）
		maybeMigrateThenRestore()
		return nil
	case ReloadUnloadLoad:
		// 此分支严格执行 unload → load，不检查 adv.Reload（避免与 ReloadInPlace 语义混淆）
		if err := unloadThenLoad(); err != nil {
			return err
		}
		maybeMigrateThenRestore()
	case ReloadBlueGreen:
		if err := pi.reloadBlueGreen(ctx, coordinator, newContext, oldContext); err != nil {
			// 蓝绿失败时旧实例从未被移除、仍在正常服务：
			// 状态恢复为 Loaded（而不是卡在 Reloading 阻塞后续 Reload），错误记入 lastError。
			pi.mu.Lock()
			pi.state = Loaded
			pi.lastError = err
			pi.mu.Unlock()
			return err
		}
		// 切换成功后对新实例（已换入 pi）恢复状态。
		// 此前 SaveState 的成果在蓝绿路径被白白丢弃，MigrateState 永不生效。
		maybeMigrateThenRestore()
	default:
		if err := unloadThenLoad(); err != nil {
			return err
		}
	}

	return nil
}

// reloadBlueGreen 蓝绿重载策略：并行运行新实例，就绪后原子切换，最后停止旧实例。
//
// 停机窗口从"整个 Setup 时间"缩短为两次 engine 操作的微秒级间隔。
//
// 双活防护：新实例的 Matcher（含别名 Matcher）注册后由 postRegister 回调立即
// DisableGroup(tempGroup) 置为禁用态，Setup 期间不参与分发；切换时先移除旧组，
// 迁移分组后统一 EnableGroup 激活。消息被新旧两组重复处理的窗口
// 从整个 Setup 时长压缩到单次注册与禁用之间的微秒级间隔。
func (pi *Instance) reloadBlueGreen(ctx context.Context, coordinator engine.PluginCoordinator, newContext, oldContext *SetupContext) error {
	pluginName := pi.desc.Name
	tempGroup := pluginName + ".__bg"

	newInstance := &Instance{
		desc:         pi.desc,
		state:        Unloaded,
		matchers:     make([]*engine.Matcher, 0),
		setupContext: newContext,
	}
	newContext.instance = newInstance
	if coordinator != nil {
		// postRegister：每个新 Matcher 注册后立即禁用 tempGroup，避免 Setup 期间双活
		newContext.Reg = newLiveRegistryWriterWithHook(coordinator, tempGroup, newInstance, func(_ *engine.Matcher) {
			coordinator.DisableGroup(tempGroup)
		})
	} else {
		newContext.Reg = newLiveRegistryWriter(coordinator, tempGroup, newInstance)
	}

	// Step 1: 并行运行新 Setup（旧实例继续处理消息，新 Matcher 保持禁用）
	if err := loadWithRegisterBatch(coordinator, func() error { return newInstance.load(ctx) }); err != nil {
		if coordinator != nil {
			coordinator.RemoveGroup(tempGroup)
		}
		return fmt.Errorf("blue-green reload: new instance setup failed: %w", err)
	}

	// Step 2+3: 极短停机窗口。
	// 必须使用 coordinator.SetMatcherGroup（同步更新 engine 的 groupIndex），
	// 而不是 m.SetGroup（只改 matcher 字段）：否则索引仍把新 matcher 记在
	// tempGroup 下，随后 RemoveGroup(tempGroup) 因"按当前 group 过滤零命中"
	// 而保留旧索引——后续 Disable/Unregister/再次 Reload 全部静默失效，
	// 且每次重载都会累积一份旧 matcher（历史 bug）。
	if coordinator != nil {
		// 兜底：确保 Setup 期间注册的所有新 Matcher（含别名）处于禁用态
		coordinator.DisableGroup(tempGroup)
		coordinator.RemoveGroup(pluginName)
		newInstance.mu.RLock()
		ms := make([]*engine.Matcher, len(newInstance.matchers))
		copy(ms, newInstance.matchers)
		newInstance.mu.RUnlock()
		for _, m := range ms {
			coordinator.SetMatcherGroup(m, pluginName, "plugin:"+pluginName)
		}
		// 迁移完成后统一激活新组（Matcher 的禁用位不随分组迁移改变）
		coordinator.EnableGroup(pluginName)
		// 清理索引中可能残留的 tempGroup 空键（此时组内已无成员，调用幂等）
		coordinator.RemoveGroup(tempGroup)
	}

	// Step 4: 原子切换内部状态
	pi.mu.Lock()
	oldGM := pi.goroutineMgr
	oldAPI := pi.exportedAPI
	oldContainer := pi.setupContext.container

	newInstance.mu.RLock()
	pi.matchers = newInstance.matchers
	pi.goroutineMgr = newInstance.goroutineMgr
	pi.exportedAPI = newInstance.exportedAPI
	pi.loadedVer = newInstance.loadedVer // 回拷新实例版本，保证下次重载 MigrateState 版本比较正确
	newInstance.mu.RUnlock()

	pi.setupContext = newContext
	pi.state = Loaded
	pi.loadTime = time.Now()
	pi.lastError = nil
	pi.mu.Unlock()

	// 更新换入的 Reg writer：插件保留的 ctx.Reg 引用仍指向 __bg 临时组，
	// 此后运行期注册会落到临时组并被清理。更新 writer 的分组与回调，
	// 使后续注册进入正式插件组并保持活跃。
	if rw, ok := newContext.Reg.(*liveRegistryWriter); ok {
		rw.name = pluginName
		rw.postRegister = nil
	}

	// Step 5: 更新容器中的 API 指针，使依赖方重新解析到新实例
	if oldContainer != nil && newInstance.exportedAPI != nil {
		oldContainer.Register(pluginName, newInstance.exportedAPI)
	}

	// Step 6: 在 Manager 中注册 draining 跟踪（异步清理旧实例）
	pm := pi.managerRef()
	if pm != nil {
		pm.stats.trackDraining(pluginName, nil)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WithField("plugin", pluginName).Errorf("[plugin] Blue-green: old instance teardown panicked: %v", r)
				if pm != nil {
					pm.stats.markDrainingDone(pluginName, fmt.Errorf("panic: %v", r))
				}
			}
		}()
		if oldGM != nil {
			oldGM.stopAndWait()
		}
		// 清理旧实例的 rootScope：EventBus 订阅、OnDispose 钩子、子 Scope。
		// 否则旧 handler 持续留在总线上，事件会被新旧实例各处理一次（历史 bug）。
		if oldContext != nil && oldContext.rootScope != nil {
			if err := oldContext.rootScope.Dispose(context.Background()); err != nil {
				logger.WithField("plugin", pluginName).WithError(err).Warn("[plugin] Blue-green: old scope dispose failed")
			}
		}
		tctx := &TeardownContext{
			API:      oldAPI,
			Config:   oldContext.Config,   // 旧实例清理回调应看到旧配置
			EventBus: oldContext.EventBus, // 旧实例的订阅归属旧总线
			Log:      newPluginLogger(pluginName),
		}
		if teardownErr := pi.desc.callTeardown(tctx); teardownErr != nil {
			logger.WithError(teardownErr).Warnf("[plugin] Blue-green: old instance teardown failed for %s", pluginName)
			if pm != nil {
				pm.stats.markDrainingDone(pluginName, teardownErr)
			}
		} else if pm != nil {
			pm.stats.markDrainingDone(pluginName, nil)
		}
	}()

	return nil
}
