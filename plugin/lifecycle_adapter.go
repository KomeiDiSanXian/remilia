package plugin

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/lifecycle"
)

// ManagerComponent 将整个 plugin.Manager 适配为 lifecycle.Component。
//
// 通过将 ManagerComponent 注册到 lifecycle.Manager 中，
// 插件的 Setup/Teardown 生命周期与平台适配器、engine 统一管理：
//
//   - OnStart：调用 pm.StartAll(ctx)，按拓扑顺序 Setup 所有插件
//   - OnRun  ：阻塞等待停止信号（插件通过 engine 的 Matcher 响应事件）
//   - OnStop ：调用 pm.StopAll(ctx)，逆序 Teardown 所有插件
//
// 这是集成插件生命周期与 framework 的唯一推荐方式。
//
// 使用示例（在 lifecycle.Manager 中注册，需在平台适配器之后）：
//
//	lm.Register(plugin.NewManagerComponent(pm))
//
// 注意：使用 ManagerComponent 后，无需再手动调用 pm.StartAll() / StopAll()，
// lifecycle.Manager 会在 Start/Stop 时自动触发。
type ManagerComponent struct {
	pm *Manager
}

// NewManagerComponent 创建 plugin.Manager 的 lifecycle.Component 适配器。
func NewManagerComponent(pm *Manager) lifecycle.Component {
	return &ManagerComponent{pm: pm}
}

// Name 返回组件名称（实现 lifecycle.Component）
func (mc *ManagerComponent) Name() string {
	return "plugin-manager"
}

// OnStart 调用 pm.StartAll()，按拓扑顺序 Setup 所有已注册插件（实现 lifecycle.Component）。
// ctx 用于控制操作的超时，发生超时时返回 ctx.Err()。
// 超时控制会传递到插件 Setup 函数。
func (mc *ManagerComponent) OnStart(ctx context.Context) error {
	return mc.pm.StartAll(ctx)
}

// OnRun 阻塞等待生命周期停止信号（实现 lifecycle.Component）。
//
// 插件本身通过 engine 的 Matcher 响应事件，不需要在此运行额外的后台循环。
func (mc *ManagerComponent) OnRun(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// OnStop 调用 pm.StopAll()，逆序 Teardown 所有插件（实现 lifecycle.Component）。
// ctx 用于控制操作的超时。
// 超时控制会传递到插件 Teardown 函数。
func (mc *ManagerComponent) OnStop(ctx context.Context) error {
	return mc.pm.StopAll(ctx)
}
