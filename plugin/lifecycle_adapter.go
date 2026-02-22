package plugin

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
)

// Component 将插件适配为 lifecycle.Component
// 这样插件可以被集成到统一的生命周期管理系统中
type Component struct {
	plugin      Plugin
	coordinator *engine.Engine
	manager     *Manager // 用于触发生命周期事件
}

// NewPluginComponent 创建插件生命周期组件适配器
func NewPluginComponent(plugin Plugin, coordinator *engine.Engine, manager *Manager) lifecycle.Component {
	return &Component{
		plugin:      plugin,
		coordinator: coordinator,
		manager:     manager,
	}
}

// Name 返回组件名称（实现 lifecycle.Component）
func (pc *Component) Name() string {
	return "plugin:" + pc.plugin.Name()
}

// OnStart 启动时加载插件（实现 lifecycle.Component）
func (pc *Component) OnStart(ctx context.Context) error {
	name := pc.plugin.Name()
	logger.Infof("[Component] Starting plugin: %s", name)

	// 设置加载中状态
	if stateful, ok := pc.plugin.(statefulPluginWriter); ok {
		stateful.SetState(Loading)
	}

	// 加载插件
	if err := pc.plugin.Load(pc.coordinator); err != nil {
		logger.WithError(err).Errorf("[Component] Failed to load plugin: %s", name)

		// 设置错误状态
		if stateful, ok := pc.plugin.(statefulPluginWriter); ok {
			stateful.SetState(Error)
			stateful.SetLastError(err)
		}

		// 触发错误通知
		if pc.manager != nil {
			pc.manager.notifyError(name, "load", err)
		}

		return err
	}

	// 设置加载完成状态
	if stateful, ok := pc.plugin.(statefulPluginWriter); ok {
		stateful.SetState(Loaded)
		stateful.SetLastError(nil)
	}

	// 触发加载完成通知
	if pc.manager != nil {
		pc.manager.notifyLoaded(name)
	}

	logger.Infof("[Component] Plugin loaded: %s", name)
	return nil
}

// OnRun 运行时逻辑（实现 lifecycle.Component）
// 插件通常不需要长期运行的逻辑，所以这里只是等待停止信号
func (pc *Component) OnRun(ctx context.Context) error {
	// 大多数插件不需要持续运行的逻辑
	// 它们通过注册的 Matcher 被动响应事件
	// 这里只是等待停止信号
	<-ctx.Done()
	return nil
}

// OnStop 停止时卸载插件（实现 lifecycle.Component）
func (pc *Component) OnStop(ctx context.Context) error {
	name := pc.plugin.Name()
	logger.Infof("[Component] Stopping plugin: %s", name)

	// 卸载插件
	if err := pc.plugin.Unload(pc.coordinator); err != nil {
		logger.WithError(err).Errorf("[Component] Failed to unload plugin: %s", name)

		// 触发错误通知
		if pc.manager != nil {
			pc.manager.notifyError(name, "unload", err)
		}

		return err
	}

	// 设置卸载状态
	if stateful, ok := pc.plugin.(statefulPluginWriter); ok {
		stateful.SetState(Unloaded)
	}

	// 触发卸载完成通知
	if pc.manager != nil {
		pc.manager.notifyUnloaded(name)
	}

	logger.Infof("[Component] Plugin stopped: %s", name)
	return nil
}

// GetPlugin 获取底层插件对象
func (pc *Component) GetPlugin() Plugin {
	return pc.plugin
}
