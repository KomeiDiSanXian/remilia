package plugin

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// reload.go — 插件热重载逻辑：三种重载策略实现

// reload 重载插件（实现 pluginInternal）
func (pi *Instance) reload(coordinator engine.PluginCoordinator) error {
	pi.mu.Lock()
	oldContext := pi.setupContext
	pi.state = Reloading
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

	// 重新创建 SetupContext（保留 Admin 视图）
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
		},
	}

	pi.mu.Lock()
	pi.setupContext = newContext
	pi.mu.Unlock()

	switch adv.Strategy {
	case ReloadInPlace:
		if adv.Reload == nil {
			// ReloadInPlace 要求提供 Reload 函数；未提供时降级并给出明确警告
			logger.Warnf("[plugin] %s: ReloadInPlace specified but Advanced.Reload is nil, falling back to ReloadUnloadLoad", pi.desc.Name)
			if err := pi.unload(coordinator); err != nil {
				return err
			}
			if err := pi.load(); err != nil {
				return err
			}
			if savedState != nil && adv.RestoreState != nil {
				if err := adv.RestoreState(savedState); err != nil {
					logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
				}
			}
			return nil
		}
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
		pi.mu.Unlock()
		return nil
	case ReloadUnloadLoad:
		// 此分支严格执行 unload → load，不检查 adv.Reload（避免与 ReloadInPlace 语义混淆）
		if err := pi.unload(coordinator); err != nil {
			return err
		}
		if err := pi.load(); err != nil {
			return err
		}
		if savedState != nil && adv.RestoreState != nil {
			if err := adv.RestoreState(savedState); err != nil {
				logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
			}
		}
	case ReloadBlueGreen:
		if err := pi.reloadBlueGreen(coordinator, newContext); err != nil {
			return err
		}
	default:
		if err := pi.unload(coordinator); err != nil {
			return err
		}
		if err := pi.load(); err != nil {
			return err
		}
	}

	return nil
}

// reloadBlueGreen 蓝绿重载策略：并行运行新实例，就绪后原子切换，最后停止旧实例。
//
// 停机窗口从"整个 Setup 时间"缩短为两次 engine 操作的微秒级间隔。
func (pi *Instance) reloadBlueGreen(coordinator engine.PluginCoordinator, newContext *SetupContext) error {
	pluginName := pi.desc.Name
	tempGroup := pluginName + ".__bg"

	newInstance := &Instance{
		desc:         pi.desc,
		state:        Unloaded,
		matchers:     make([]*engine.Matcher, 0),
		setupContext: newContext,
	}
	newContext.instance = newInstance
	newContext.Reg = newLiveRegistryWriter(coordinator, tempGroup, newInstance)

	// Step 1: 并行运行新 Setup（旧实例继续处理消息）
	if err := newInstance.load(); err != nil {
		if coordinator != nil {
			coordinator.RemoveGroup(tempGroup)
		}
		return fmt.Errorf("blue-green reload: new instance setup failed: %w", err)
	}

	// Step 2+3: 极短停机窗口
	if coordinator != nil {
		coordinator.RemoveGroup(pluginName)
		newInstance.mu.RLock()
		for _, m := range newInstance.matchers {
			m.SetGroup(pluginName)
		}
		newInstance.mu.RUnlock()
		coordinator.RemoveGroup(tempGroup)
	}

	// Step 4: 原子切换内部状态
	pi.mu.Lock()
	oldGM := pi.goroutineMgr
	oldAPI := pi.exportedAPI

	newInstance.mu.RLock()
	pi.matchers = newInstance.matchers
	pi.goroutineMgr = newInstance.goroutineMgr
	pi.exportedAPI = newInstance.exportedAPI
	newInstance.mu.RUnlock()

	pi.setupContext = newContext
	pi.state = Loaded
	pi.loadTime = time.Now()
	pi.lastError = nil
	pi.mu.Unlock()

	// Step 5: 异步停止旧 goroutine 并调用旧 Teardown
	go func() {
		if oldGM != nil {
			oldGM.stopAndWait()
		}
		tctx := &TeardownContext{
			API:      oldAPI,
			Config:   newContext.Config,
			EventBus: newContext.EventBus,
			Log:      newPluginLogger(pluginName),
		}
		if teardownErr := pi.desc.callTeardown(tctx); teardownErr != nil {
			logger.WithError(teardownErr).Warnf("[plugin] Blue-green: old instance teardown failed for %s", pluginName)
		}
	}()

	return nil
}
