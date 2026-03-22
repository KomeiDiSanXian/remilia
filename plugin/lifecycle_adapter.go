package plugin

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
)

// Component 将插件实例适配为 lifecycle.Component
// 这样插件可以被集成到统一的生命周期管理系统中
type Component struct {
	instance    *Instance
	coordinator engine.PluginCoordinator
	manager     *Manager // 用于触发生命周期事件
}

// NewPluginComponent 创建插件生命周期组件适配器
func NewPluginComponent(inst *Instance, coordinator engine.PluginCoordinator, manager *Manager) lifecycle.Component {
	return &Component{
		instance:    inst,
		coordinator: coordinator,
		manager:     manager,
	}
}

// Name 返回组件名称（实现 lifecycle.Component）
func (pc *Component) Name() string {
	return "plugin:" + pc.instance.Name()
}

// OnStart 启动时加载插件（实现 lifecycle.Component）
func (pc *Component) OnStart(context.Context) error {
	name := pc.instance.Name()
	logger.Infof("[Component] Starting plugin: %s", name)

	pc.instance.SetState(Loading)

	if err := pc.instance.load(pc.coordinator); err != nil {
		logger.WithError(err).Errorf("[Component] Failed to load plugin: %s", name)
		pc.instance.SetState(Error)
		pc.instance.SetLastError(err)
		if pc.manager != nil {
			pc.manager.notifyError(name, "load", err)
		}
		return err
	}

	pc.instance.SetState(Loaded)
	pc.instance.SetLastError(nil)
	if pc.manager != nil {
		pc.manager.notifyLoaded(name)
	}

	logger.Infof("[Component] Plugin loaded: %s", name)
	return nil
}

// OnRun 运行时逻辑（实现 lifecycle.Component）
// 插件通常不需要长期运行的逻辑，这里只等待停止信号
func (pc *Component) OnRun(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// OnStop 停止时卸载插件（实现 lifecycle.Component）
func (pc *Component) OnStop(context.Context) error {
	name := pc.instance.Name()
	logger.Infof("[Component] Stopping plugin: %s", name)

	if err := pc.instance.unload(pc.coordinator); err != nil {
		logger.WithError(err).Errorf("[Component] Failed to unload plugin: %s", name)
		if pc.manager != nil {
			pc.manager.notifyError(name, "unload", err)
		}
		return err
	}

	if pc.manager != nil {
		pc.manager.notifyUnloaded(name)
	}

	logger.Infof("[Component] Plugin stopped: %s", name)
	return nil
}

// GetInstance 获取底层插件实例
func (pc *Component) GetInstance() *Instance {
	return pc.instance
}
